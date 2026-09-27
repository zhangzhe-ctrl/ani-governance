package build

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"go-wind-admin/tools/localdeps/gow/internal/pkg"
)

var (
	outputDir  string
	targetOS   []string
	targetArch []string
	buildVer   string
	ldflagsArg string
	trimPath   bool
	stripFlag  bool
)

// CmdBuild build 命令:编译单个、多个或全部服务,支持 GOOS/GOARCH 交叉编译。
var CmdBuild = &cobra.Command{
	Use:   "build [service...]",
	Short: "Build one, several, or all project services into binaries (cross-compilation supported)",
	Long: `Build services of the project into standalone binaries.

With no arguments every service under app/ is built; naming one or more services
builds just those. Each service's cmd/server main package is compiled.

The --os and --arch flags accept comma separated lists; every GOOS×GOARCH
combination is built (cross compilation), e.g. --os linux,windows --arch amd64.
Cross-compiled binaries carry a _<goos>_<goarch> filename suffix so the
combinations never overwrite each other.

By default binaries are written into each service's bin/ directory; --out
redirects all output into a single directory instead.

Version stamping: generated service main packages declare "var version" —
--version injects it via -ldflags "-X main.version=...". --ldflags passes
extra flags through verbatim; --strip adds "-s -w" (smaller binaries);
--trimpath removes local filesystem paths from the binary.`,
	RunE:         Run,
	SilenceUsage: true,
}

func init() {
	CmdBuild.Flags().StringVarP(&outputDir, "out", "o", "", "output directory for all built binaries (default: each service's bin/)")
	CmdBuild.Flags().StringArrayVar(&targetOS, "os", nil, "comma separated target GOOS list for cross compilation")
	CmdBuild.Flags().StringArrayVar(&targetArch, "arch", nil, "comma separated target GOARCH list for cross compilation")
	CmdBuild.Flags().StringVar(&buildVer, "version", "", "inject as main.version into each built service (via -ldflags -X)")
	CmdBuild.Flags().StringVar(&ldflagsArg, "ldflags", "", "extra -ldflags passed verbatim to go build")
	CmdBuild.Flags().BoolVar(&trimPath, "trimpath", false, "add -trimpath (remove local filesystem paths from binaries)")
	CmdBuild.Flags().BoolVar(&stripFlag, "strip", false, "add \"-s -w\" to ldflags (strip symbol table and DWARF for smaller binaries)")
}

// buildTarget 一次构建目标。
type buildTarget struct {
	goos     string
	goarch   string
	explicit bool
}

// Run 为 cobra 的 RunE 回调:编译目标服务/目标平台组合。
func Run(cmd *cobra.Command, args []string) error {
	inspector, err := pkg.NewModuleInspectorFromGo(cmd.Context(), "")
	if err != nil {
		return err
	}

	// 工具不再自动执行 `go mod tidy`（T13 授权的工作流变更）：
	// 依赖维护改为显式人工 `go mod tidy` 或 T14 阶段处理。

	names, err := resolveServiceNames(inspector.Root, args)
	if err != nil {
		return err
	}

	targets, err := resolveTargets()
	if err != nil {
		return err
	}

	opts := OptionsFromFlags()

	failed := false
	for _, name := range names {
		serviceDir := filepath.Join(inspector.Root, "app", name, "service")
		for _, target := range targets {
			outPath, err := ResolveOutputPath(name, serviceDir, target.goos, target.goarch, target.explicit)
			if err != nil {
				_, _ = fmt.Fprintf(os.Stderr, "\033[31mERROR: %s\033[m\n", err.Error())
				failed = true
				continue
			}
			if err = BuildBinary(cmd.Context(), serviceDir, outPath, target.goos, target.goarch, opts); err != nil {
				_, _ = fmt.Fprintf(os.Stderr, "\033[31mERROR: build for service '%s' (%s/%s) failed: %s\033[m\n", name, target.goos, target.goarch, err.Error())
				failed = true
				continue
			}
			_, _ = fmt.Fprintf(os.Stdout, "\033[32mSUCCESS: built '%s' (%s/%s) -> %s\033[m\n", name, target.goos, target.goarch, outPath)
		}
	}
	if failed {
		return fmt.Errorf("one or more build targets failed")
	}
	return nil
}

// resolveServiceNames 解析目标服务列表:无参数时枚举全部有效服务,否则逐个校验。
func resolveServiceNames(root string, args []string) ([]string, error) {
	if len(args) == 0 {
		names, err := pkg.ListServiceNames(root)
		if err != nil {
			return nil, err
		}
		if len(names) == 0 {
			return nil, fmt.Errorf("no valid services found under %s", filepath.Join(root, "app"))
		}
		return names, nil
	}

	names := make([]string, 0, len(args))
	for _, arg := range args {
		name := strings.TrimSpace(arg)
		if name == "" {
			return nil, fmt.Errorf("service name is required")
		}
		valid, err := pkg.IsValidServiceName(root, name)
		if err != nil {
			return nil, err
		}
		if !valid {
			return nil, fmt.Errorf("service '%s' does not exist or is not valid (missing cmd/server or configs)", name)
		}
		names = append(names, name)
	}
	return names, nil
}

// resolveTargets 解析构建目标组合。未指定 os/arch 时为单一原生目标;
// 指定任一即进入显式模式,做 GOOS×GOARCH 全组合。
func resolveTargets() ([]buildTarget, error) {
	oss := pkg.SplitFlagList(targetOS)
	archs := pkg.SplitFlagList(targetArch)

	if len(oss) == 0 && len(archs) == 0 {
		return []buildTarget{{goos: runtime.GOOS, goarch: runtime.GOARCH, explicit: false}}, nil
	}
	if len(oss) == 0 {
		oss = []string{runtime.GOOS}
	}
	if len(archs) == 0 {
		archs = []string{runtime.GOARCH}
	}

	targets := make([]buildTarget, 0, len(oss)*len(archs))
	for _, goos := range oss {
		for _, goarch := range archs {
			targets = append(targets, buildTarget{goos: goos, goarch: goarch, explicit: true})
		}
	}
	return targets, nil
}

// ResolveOutputPath 决定产物路径:默认各服务 bin/,--out 时集中到单目录;
// 交叉目标带 _<goos>_<goarch> 后缀,GOOS=windows 追加 .exe。
// 供 build 命令与 run 命令的多服务模式共用同一命名规则。
func ResolveOutputPath(name string, serviceDir string, goos string, goarch string, explicit bool) (string, error) {
	outDir := filepath.Join(serviceDir, "bin")
	if outputDir != "" {
		absDir, err := filepath.Abs(outputDir)
		if err != nil {
			return "", err
		}
		outDir = absDir
	} else {
		absDir, err := filepath.Abs(outDir)
		if err != nil {
			return "", err
		}
		outDir = absDir
	}
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return "", err
	}

	binary := name
	if explicit {
		binary = fmt.Sprintf("%s_%s_%s", name, goos, goarch)
	}
	if goos == "windows" {
		binary += ".exe"
	}
	return filepath.Join(outDir, binary), nil
}

// BuildOptions 描述一次 go build 的附加选项。
type BuildOptions struct {
	Version  string   // 注入为服务 main 包 version 变量的版本号;空则不注入
	Ldflags  []string // 额外 -ldflags 内容,逐段拼接
	TrimPath bool     // 加 -trimpath
	Strip    bool     // ldflags 追加 -s -w,剥离符号表与 DWARF
}

// OptionsFromFlags 把 build 命令旗标转换为 BuildOptions。
// 供 build 命令使用;watch 等内部场景传零值即可。
func OptionsFromFlags() BuildOptions {
	var opts BuildOptions
	opts.Version = buildVer
	opts.TrimPath = trimPath
	opts.Strip = stripFlag
	if s := strings.TrimSpace(ldflagsArg); s != "" {
		opts.Ldflags = []string{s}
	}
	return opts
}

// buildArgs 装配 go build 参数。纯函数,便于单测。
func (o BuildOptions) buildArgs(outPath string) []string {
	args := []string{"build", "-o", outPath}
	if o.TrimPath {
		args = append(args, "-trimpath")
	}

	var ld []string
	if o.Version != "" {
		ld = append(ld, "-X", "main.version="+o.Version)
	}
	if o.Strip {
		ld = append(ld, "-s", "-w")
	}
	ld = append(ld, o.Ldflags...)
	if len(ld) > 0 {
		args = append(args, "-ldflags", strings.Join(ld, " "))
	}

	return append(args, "./cmd/server")
}

// buildTimeout 单次服务构建的超时上限,防止 go build 挂死整个 CLI。
const buildTimeout = 10 * time.Minute

// BuildBinary 编译服务主程序为独立二进制。goos/goarch 为目标平台,
// 与宿主一致即原生构建。输出路径须为绝对路径。
func BuildBinary(ctx context.Context, serviceDir string, outPath string, goos string, goarch string, opts BuildOptions) error {
	g := pkg.NewGoCmdWithTimeout(serviceDir, buildTimeout)
	g.Env = []string{
		"GOOS=" + goos,
		"GOARCH=" + goarch,
	}
	return g.Run(ctx, opts.buildArgs(outPath)...)
}
