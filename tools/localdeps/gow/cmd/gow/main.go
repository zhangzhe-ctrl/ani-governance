package main

import (
	"log"

	"github.com/spf13/cobra"

	"go-wind-admin/tools/localdeps/gow/internal/run"
)

var rootCmd = &cobra.Command{
	Use:   "gow",
	Short: "gow CLI",
	Long:  "gow is the CLI for GoWind framework.",
}

func init() {
	rootCmd.AddCommand(run.CmdRun)
	rootCmd.AddCommand(versionCmd)
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		log.Fatal(err)
	}
}
