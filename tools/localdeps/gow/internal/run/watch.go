package run

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/fsnotify/fsnotify"

	"go-wind-admin/tools/localdeps/gow/internal/build"
)

const (
	// watchDebounce 静默期:该窗口内的连续文件事件合并为一次重建重启。
	watchDebounce = 500 * time.Millisecond
	// stopGrace 优雅停止宽限期:超时后强杀。
	stopGrace = 5 * time.Second
)

// watchTarget 一个被监听的服务。
type watchTarget struct {
	name string
	dir  string // 服务目录 app/<name>/service
}

// watchedExts 触发重建重启的文件扩展名。
// .proto 变更需要先 `gow api` 重新生成才能反映到代码,但重启无害,故一并触发。
var watchedExts = map[string]bool{
	".go":         true,
	".yaml":       true,
	".yml":        true,
	".json":       true,
	".toml":       true,
	".properties": true,
	".proto":      true,
}

// skippedDirs 不纳入监听的目录名(一切以 . 开头的目录同样跳过)。
var skippedDirs = map[string]bool{
	".git":         true,
	".idea":        true,
	".vscode":      true,
	".zcode":       true,
	"node_modules": true,
	"vendor":       true,
	"bin":          true,
	"testdata":     true,
}

// shouldTriggerWatch 过滤无关变更:隐藏文件、_test.go、未监听的扩展名、纯 Chmod。
func shouldTriggerWatch(path string, op fsnotify.Op) bool {
	if op&fsnotify.Chmod != 0 {
		return false
	}
	base := filepath.Base(path)
	if strings.HasPrefix(base, ".") {
		return false
	}
	if strings.HasSuffix(base, "_test.go") {
		return false
	}
	return watchedExts[strings.ToLower(filepath.Ext(path))]
}

// affectedServicesByChange 计算一次文件变更影响的服务集合。
// 变更落在某服务目录(app/<name>/service)内 → 只影响该服务;
// 落在模块级共享代码或无法判定 → 保守起见影响全部服务。
func affectedServicesByChange(changedPath string, root string, targets []watchTarget) []string {
	names := make([]string, 0, len(targets))
	for _, t := range targets {
		names = append(names, t.name)
	}

	rel, err := filepath.Rel(root, changedPath)
	if err != nil {
		return names
	}
	parts := strings.Split(filepath.ToSlash(rel), "/")
	if len(parts) >= 3 && parts[0] == "app" {
		for _, t := range targets {
			if t.name == parts[1] {
				return []string{t.name}
			}
		}
	}
	return names
}

// deriveWatchServiceName 从服务目录推断服务名:布局为 app/<name>/service,
// 目录名是 service 时取上级目录名,否则取目录名本身。
func deriveWatchServiceName(dir string) string {
	base := filepath.Base(dir)
	if strings.EqualFold(base, "service") {
		return filepath.Base(filepath.Dir(dir))
	}
	return base
}

// watchServices 以 watch 模式运行目标服务:先编译启动,再递归监听模块根目录,
// 文件变更经静默期合并后只重建重启受影响的服务。Ctrl+C 停止全部并退出。
func watchServices(root string, targets []watchTarget) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	m := newWatchManager(ctx, root, targets)

	// 初始构建与启动:单个服务失败不阻断其余服务;失败的服务保持未运行,
	// 下次文件变更时 refresh 会重试。
	started := 0
	for _, t := range targets {
		binPath, err := build.ResolveOutputPath(t.name, t.dir, runtime.GOOS, runtime.GOARCH, false)
		if err != nil {
			_, _ = fmt.Fprintf(os.Stderr, "\033[31mERROR: resolve output for service '%s' failed: %s\033[m\n", t.name, err.Error())
			continue
		}
		m.binPath[t.name] = binPath
		if err = build.BuildBinary(m.ctx, t.dir, binPath, runtime.GOOS, runtime.GOARCH, build.BuildOptions{}); err != nil {
			_, _ = fmt.Fprintf(os.Stderr, "\033[31mERROR: build for service '%s' failed: %s\033[m\n", t.name, err.Error())
			continue
		}
		if err = m.start(t); err != nil {
			continue
		}
		started++
	}
	if started == 0 {
		return errors.New("no service could be started")
	}
	_, _ = fmt.Fprintf(os.Stdout, "\033[36mWatching %d service(s); save a file to rebuild & restart, Ctrl+C stops all.\033[m\n", started)

	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		m.stopAll()
		return err
	}
	defer watcher.Close()
	if err = addWatchRecursively(watcher, root); err != nil {
		m.stopAll()
		return err
	}

	// 重建请求由单个工作 goroutine 串行执行:编译耗时远超防抖窗口,若在
	// AfterFunc 的 goroutine 里直接执行 refresh,防抖窗内的新事件会调度出
	// 并发的 refresh,同一服务的两个进程句柄互相覆盖、先启动者永远无人停
	// 止,产生孤儿进程。
	refreshCh := make(chan []string)
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case names := <-refreshCh:
				for _, name := range names {
					if ctx.Err() != nil {
						return
					}
					m.refresh(name)
				}
			}
		}
	}()

	// 事件合并:静默期内的新事件重置计时器;到期后一次性处理积压的受影响服务。
	var mu sync.Mutex
	pending := map[string]bool{}
	var debounce *time.Timer
	flush := func() {
		mu.Lock()
		names := make([]string, 0, len(pending))
		for name := range pending {
			names = append(names, name)
		}
		pending = map[string]bool{}
		debounce = nil
		mu.Unlock()

		sort.Strings(names)
		if ctx.Err() != nil {
			return
		}
		select {
		case refreshCh <- names:
		case <-ctx.Done():
		}
	}
	resetDebounce := func() {
		mu.Lock()
		if debounce != nil {
			debounce.Stop()
		}
		debounce = time.AfterFunc(watchDebounce, flush)
		mu.Unlock()
	}

	for {
		select {
		case <-ctx.Done():
			m.stopAll()
			return nil
		case ev, ok := <-watcher.Events:
			if !ok {
				m.stopAll()
				return errors.New("file watcher closed unexpectedly")
			}
			if ev.Op&fsnotify.Create != 0 {
				// 新建目录纳入监听;目录里的既有文件会补发事件,无需单独处理。
				if fi, statErr := os.Stat(ev.Name); statErr == nil && fi.IsDir() && !skippedDirPath(ev.Name) {
					_ = addWatchRecursively(watcher, ev.Name)
				}
			}
			if !shouldTriggerWatch(ev.Name, ev.Op) {
				continue
			}
			mu.Lock()
			for _, name := range affectedServicesByChange(ev.Name, root, targets) {
				pending[name] = true
			}
			mu.Unlock()
			resetDebounce()
		case err, ok := <-watcher.Errors:
			m.stopAll()
			if !ok {
				return errors.New("file watcher closed unexpectedly")
			}
			return fmt.Errorf("file watcher error: %w", err)
		}
	}
}

// addWatchRecursively 递归监听目录树:跳过隐藏目录与 skippedDirs。
// 单个子目录监听失败(如无权限)不阻断整体。
func addWatchRecursively(watcher *fsnotify.Watcher, dir string) error {
	return filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if !d.IsDir() {
			return nil
		}
		if p != dir && skippedDirPath(p) {
			return filepath.SkipDir
		}
		_ = watcher.Add(p)
		return nil
	})
}

// skippedDirPath 目录名在跳过名单中,或是以 . 开头的隐藏目录。
func skippedDirPath(p string) bool {
	name := filepath.Base(p)
	return skippedDirs[name] || strings.HasPrefix(name, ".")
}

// procHandle 一个运行中的服务进程;done 由唯一的 Wait 持有者关闭。
type procHandle struct {
	cmd  *exec.Cmd
	done chan struct{}
}

// watchManager 管理被监听服务的进程生命周期:构建 → 启动 → 变更时重建重启。
type watchManager struct {
	ctx      context.Context
	targets  []watchTarget
	byName   map[string]watchTarget
	binPath  map[string]string
	prefixed bool // 多服务时输出加服务名前缀

	outMu sync.Mutex
	errMu sync.Mutex

	mu      sync.Mutex
	procs   map[string]*procHandle
	stopped map[string]bool
}

func newWatchManager(ctx context.Context, root string, targets []watchTarget) *watchManager {
	byName := make(map[string]watchTarget, len(targets))
	for _, t := range targets {
		byName[t.name] = t
	}
	return &watchManager{
		ctx:      ctx,
		targets:  targets,
		byName:   byName,
		binPath:  map[string]string{},
		prefixed: len(targets) > 1,
		procs:    map[string]*procHandle{},
		stopped:  map[string]bool{},
	}
}

// start 拉起服务进程并登记;进程退出由唯一的 goroutine Wait 并按需告警。
// 进程绑定到管理器的 ctx:信号上下文取消时由 exec 撤销,不会在 Ctrl+C
// 与重建竞态时留下无人管理的进程。
func (m *watchManager) start(t watchTarget) error {
	configPath := filepath.Join(t.dir, "configs")
	proc := exec.CommandContext(m.ctx, m.binPath[t.name], "-c", configPath)
	proc.Dir = t.dir
	if m.prefixed {
		proc.Stdout = newPrefixedWriter(os.Stdout, t.name, &m.outMu)
		proc.Stderr = newPrefixedWriter(os.Stderr, t.name, &m.errMu)
	} else {
		proc.Stdout = os.Stdout
		proc.Stderr = os.Stderr
	}
	if err := proc.Start(); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "\033[31mERROR: failed to start service '%s': %s\033[m\n", t.name, err.Error())
		return err
	}

	h := &procHandle{cmd: proc, done: make(chan struct{})}
	m.mu.Lock()
	m.procs[t.name] = h
	m.stopped[t.name] = false
	m.mu.Unlock()

	name := t.name
	go func() {
		err := proc.Wait()
		close(h.done)

		m.mu.Lock()
		if m.procs[name] == h {
			delete(m.procs, name)
		}
		intentional := m.stopped[name]
		m.mu.Unlock()

		if !intentional && m.ctx.Err() == nil {
			_, _ = fmt.Fprintf(os.Stderr, "\033[33mWARNING: service '%s' exited: %v\033[m\n", name, err)
		}
	}()
	return nil
}

// stop 同步停止单个服务:先 SIGTERM(平台不支持时直接 Kill),宽限期后强杀。
func (m *watchManager) stop(name string) {
	m.mu.Lock()
	h := m.procs[name]
	if h == nil || h.cmd.Process == nil {
		m.mu.Unlock()
		return
	}
	m.stopped[name] = true
	m.mu.Unlock()

	if err := h.cmd.Process.Signal(syscall.SIGTERM); err != nil {
		_ = h.cmd.Process.Kill()
	}
	select {
	case <-h.done:
	case <-time.After(stopGrace):
		_ = h.cmd.Process.Kill()
		<-h.done
	}
}

func (m *watchManager) stopAll() {
	m.mu.Lock()
	names := make([]string, 0, len(m.procs))
	for name := range m.procs {
		names = append(names, name)
	}
	m.mu.Unlock()
	for _, name := range names {
		m.stop(name)
	}
}

// refresh 重建并重启单个服务。先编译到临时路径(Windows 不允许覆写运行中的
// exe),编译成功才停旧、换新、再启动;编译失败保持旧进程不动。
// 仅由 watchServices 的单工作 goroutine 串行调用。
func (m *watchManager) refresh(name string) {
	if m.ctx.Err() != nil {
		return
	}
	t, ok := m.byName[name]
	if !ok {
		return
	}
	binPath, ok := m.binPath[name]
	if !ok {
		return
	}

	tmpPath := binPath + ".next"
	if err := build.BuildBinary(m.ctx, t.dir, tmpPath, runtime.GOOS, runtime.GOARCH, build.BuildOptions{}); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "\033[31mERROR: rebuild for service '%s' failed, keeping old process: %s\033[m\n", name, err.Error())
		return
	}

	m.stop(name)

	// 停止可能恰逢 Ctrl+C:主循环的 stopAll 已跑完,此处不得再拉起新进程。
	if m.ctx.Err() != nil {
		_ = os.Remove(tmpPath)
		return
	}

	if err := os.Remove(binPath); err != nil && !os.IsNotExist(err) {
		_, _ = fmt.Fprintf(os.Stderr, "\033[33mWARNING: replace binary for service '%s': %s\033[m\n", name, err.Error())
	}
	if err := os.Rename(tmpPath, binPath); err != nil {
		// 清理残留的临时产物,避免 bin/ 下遗留 .next 文件。
		_ = os.Remove(tmpPath)
		_, _ = fmt.Fprintf(os.Stderr, "\033[31mERROR: swap binary for service '%s' failed: %s\033[m\n", name, err.Error())
		return
	}

	if err := m.start(t); err != nil {
		return
	}
	_, _ = fmt.Fprintf(os.Stdout, "\033[36m[%s] rebuilt and restarted.\033[m\n", name)
}
