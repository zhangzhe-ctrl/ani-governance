package build

import (
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestResolveOutputPathNaming(t *testing.T) {
	svcDir := t.TempDir()

	// 原生目标:无后缀;Windows 追加 .exe。
	native, err := ResolveOutputPath("alpha", svcDir, runtime.GOOS, runtime.GOARCH, false)
	if err != nil {
		t.Fatalf("native: %v", err)
	}
	nativeName := filepath.Base(native)
	if runtime.GOOS == "windows" {
		if nativeName != "alpha.exe" {
			t.Fatalf("expected alpha.exe, got %q", nativeName)
		}
	} else if nativeName != "alpha" {
		t.Fatalf("expected alpha, got %q", nativeName)
	}
	if filepath.Dir(native) != svcDir+string(filepath.Separator)+"bin" &&
		filepath.Clean(filepath.Dir(native)) != filepath.Clean(filepath.Join(svcDir, "bin")) {
		t.Fatalf("expected per-service bin/, got %q", filepath.Dir(native))
	}

	// 交叉目标:带 _<goos>_<goarch> 后缀;windows 目标追加 .exe。
	cross, err := ResolveOutputPath("alpha", svcDir, "linux", "arm64", true)
	if err != nil {
		t.Fatalf("cross: %v", err)
	}
	if !strings.HasSuffix(filepath.Base(cross), "_linux_arm64") {
		t.Fatalf("expected _linux_arm64 suffix, got %q", filepath.Base(cross))
	}
	crossWin, err := ResolveOutputPath("alpha", svcDir, "windows", "amd64", true)
	if err != nil {
		t.Fatalf("crossWin: %v", err)
	}
	if filepath.Base(crossWin) != "alpha_windows_amd64.exe" {
		t.Fatalf("expected alpha_windows_amd64.exe, got %q", filepath.Base(crossWin))
	}

	// 原生但显式指定:仍按交叉目标命名(后缀照加,避免与隐式原生产物互相覆盖)。
	explicitNative, err := ResolveOutputPath("alpha", svcDir, runtime.GOOS, runtime.GOARCH, true)
	if err != nil {
		t.Fatalf("explicitNative: %v", err)
	}
	suffix := "_" + runtime.GOOS + "_" + runtime.GOARCH
	if runtime.GOOS == "windows" {
		suffix += ".exe"
	}
	if filepath.Base(explicitNative) != "alpha"+suffix {
		t.Fatalf("expected alpha%s, got %q", suffix, filepath.Base(explicitNative))
	}
}
