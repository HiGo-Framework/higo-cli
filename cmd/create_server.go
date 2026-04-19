package cmd

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"
	"github.com/triasbrata/higo-cli/internal/creator"
)

var createServerCmd = &cobra.Command{
	Use:   "server [http|grpc|consumer]",
	Short: "Add a new server type to the project",
	Long: `Add an HTTP, gRPC, or RabbitMQ consumer server to an existing project.

Creates the cmd entrypoint, bootstrap wiring, and delivery scaffold for the
chosen server type. Shared config files are patched to include the new service.
If 2 or more services are now present, cmd/mix and bootstrap/mix are regenerated.

Examples:
  higo create server http
  higo create server grpc
  higo create server consumer`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		svc := strings.ToLower(args[0])
		switch svc {
		case "http", "grpc", "consumer":
		default:
			return fmt.Errorf("unknown server type %q — must be one of: http, grpc, consumer", svc)
		}

		printCreateBanner(svc)

		data, err := creator.Probe(".")
		if err != nil {
			return err
		}

		return creator.AddServer(".", data, svc)
	},
}

func printCreateBanner(svc string) {
	title := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("12")).
		Render("  higo create server " + svc)
	sub := lipgloss.NewStyle().
		Foreground(lipgloss.Color("8")).
		Render("  adding " + svc + " server to project…")
	fmt.Println()
	fmt.Println(title)
	fmt.Println(sub)
	fmt.Println()
}

func init() {
	createCmd.AddCommand(createServerCmd)
}
