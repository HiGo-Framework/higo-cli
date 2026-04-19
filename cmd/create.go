package cmd

import "github.com/spf13/cobra"

var createCmd = &cobra.Command{
	Use:   "create",
	Short: "Add features to an existing higo project",
	Long: `Add new capabilities to an existing higo-framework project.

Run from the project root. The command validates that the current
directory is a valid higo-framework project before making any changes.

Examples:
  higo create server http
  higo create server grpc
  higo create server consumer`,
}

func init() {
	rootCmd.AddCommand(createCmd)
}
