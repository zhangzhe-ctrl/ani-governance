package main

import (
	"fmt"
	"runtime/debug"

	"github.com/spf13/cobra"
)

// version/commit/date 由发布流程注入:
// go build -ldflags "-X main.version=v1.2.3 -X main.commit=abc1234 -X main.date=2026-09-12T..."
// 本地 go build / go install 构建时为 dev,version 命令回落到构建信息中的模块版本。
var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print the version number of gow",
	Long:  `All software has versions. This is gow's`,
	Run: func(cmd *cobra.Command, args []string) {
		v := version
		if v == "dev" || v == "" {
			// go install module@vx.y.z 构建的二进制在构建信息里带模块版本。
			if bi, ok := debug.ReadBuildInfo(); ok && bi.Main.Version != "" && bi.Main.Version != "(devel)" {
				v = bi.Main.Version
			}
		}
		fmt.Printf("gow version %s (commit: %s, built: %s)\n", v, commit, date)
	},
}
