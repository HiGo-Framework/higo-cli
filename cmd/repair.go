package cmd

import (
	"fmt"

	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"
	"github.com/triasbrata/higo-cli/internal/creator"
)

var repairCmd = &cobra.Command{
	Use:   "repair",
	Short: "Regenerate shared config + delivery wiring from templates",
	Long: `Regenerate shared files that may have been corrupted by legacy
text-patching or manual edits. Reads the current project state (which
servers are installed) and re-renders config.go, env.go, fx.go, and
delivery/fx.go from their templates.

Safe to run repeatedly. User-edited files like handler.go, router.go,
and impl/*.go are NOT touched.

Run from the project root.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		files, err := creator.Repair(".")
		if err != nil {
			return err
		}
		title := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("10")).
			Render("  ✓ project repaired")
		dim := lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
		fmt.Println()
		fmt.Println(title)
		for _, f := range files {
			fmt.Println(dim.Render("    " + f))
		}
		fmt.Println()
		return nil
	},
}

func init() {
	rootCmd.AddCommand(repairCmd)
}
