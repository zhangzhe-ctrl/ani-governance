package main

import (
	"github.com/spf13/cobra"
	"go-wind-admin/tools/localdeps/gow/internal/ent"
)

var entCmd = &cobra.Command{
	Use:   "ent [service] | ent generate <service> | ent add <service> <schemas>",
	Short: "manage ent schemas",
	Long: `Manage ent schemas for services.

  gow ent admin                 generate ent code for service admin
  gow ent generate admin        same as above (explicit subcommand)
  gow ent admin1 admin2         generate for multiple services
  gow ent add admin User,Group  add schema(s) to a service, then regenerate`,
	RunE:         ent.RunGenerate,
	SilenceUsage: true,
}

var entGenerateCmd = &cobra.Command{
	Use:   "generate <service>",
	Short: "generate ent code for a service",
	//Args:  cobra.MinimumNArgs(1),
	RunE:         ent.RunGenerate,
	SilenceUsage: true,
}

var entAddCmd = &cobra.Command{
	Use:          "add <service> <schemas>",
	Short:        "add schema(s) to a service (comma separated, e.g. User,Group)",
	Args:         cobra.MinimumNArgs(2),
	RunE:         ent.RunAdd,
	SilenceUsage: true,
}

func init() {
	entCmd.AddCommand(entGenerateCmd, entAddCmd)
	rootCmd.AddCommand(entCmd)
}
