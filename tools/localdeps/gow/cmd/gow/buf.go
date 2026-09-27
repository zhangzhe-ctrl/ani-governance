package main

import (
	"github.com/spf13/cobra"

	"go-wind-admin/tools/localdeps/gow/internal/buf"
)

var bufCmd = &cobra.Command{
	Use:          "api",
	Short:        "manage proto and buf files",
	Long:         "Manage proto and buf files for services.",
	Args:         cobra.NoArgs,
	RunE:         buf.RunGenerate,
	SilenceUsage: true,
}

func init() {
	rootCmd.AddCommand(bufCmd)
}
