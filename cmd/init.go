package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"
	"github.com/triasbrata/higo-cli/internal/envform"
	"github.com/triasbrata/higo-cli/internal/generator"
	"github.com/triasbrata/higo-cli/internal/runner"
	"github.com/triasbrata/higo-cli/internal/tooling"
	"github.com/triasbrata/higo-cli/internal/wizard"
)

var initCmd = &cobra.Command{
	Use:   "init [project-name]",
	Short: "Initialize a new higo project interactively",
	Args:  cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		labelStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
		valueStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("15"))
		pathStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("14"))
		infoStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("12"))
		successStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("10"))
		errStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("9"))
		dimStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("8"))

		// seed wizard with project name arg if provided
		var prev *wizard.ProjectData
		if len(args) > 0 {
			prev = &wizard.ProjectData{
				ProjectName: args[0],
				ModuleName:  "github.com/you/" + args[0],
				AppName:     args[0],
				HasHTTP:     true,
			}
		}

		for {
			data, err := wizard.Run(prev)
			if err != nil {
				return err
			}

			outputDir := data.ProjectName
			if _, err := os.Stat(outputDir); !os.IsNotExist(err) {
				return fmt.Errorf("directory %q already exists", outputDir)
			}

			absPath, err := filepath.Abs(outputDir)
			if err != nil {
				return err
			}

			services := []string{}
			if data.HasHTTP {
				services = append(services, "HTTP")
			}
			if data.HasGRPC {
				services = append(services, "gRPC")
			}
			if data.HasConsumer {
				services = append(services, "Consumer")
			}
			pyroscope := "No"
			if data.HasPyroscope {
				pyroscope = "Yes"
			}

			fmt.Println()
			fmt.Println(labelStyle.Render("  Project name   ") + valueStyle.Render(data.ProjectName))
			fmt.Println(labelStyle.Render("  Module path    ") + valueStyle.Render(data.ModuleName))
			fmt.Println(labelStyle.Render("  App name       ") + valueStyle.Render(data.AppName))
			fmt.Println(labelStyle.Render("  Services       ") + valueStyle.Render(strings.Join(services, ", ")))
			fmt.Println(labelStyle.Render("  Pyroscope      ") + valueStyle.Render(pyroscope))
			fmt.Println(labelStyle.Render("  Output path    ") + pathStyle.Render(absPath))
			fmt.Println()

			confirmed := true
			if err := huh.NewConfirm().
				Title("Proceed with project creation?").
				Value(&confirmed).
				Run(); err != nil {
				return err
			}
			if !confirmed {
				// go back to wizard with previous values pre-filled
				prev = data
				fmt.Println()
				continue
			}

			if err := generator.Generate(outputDir, data); err != nil {
				return err
			}

			fmt.Println()
			fmt.Println(infoStyle.Render("→ Installing dependencies..."))
			fmt.Println()

			if err := runner.RunTidy(outputDir); err != nil {
				os.RemoveAll(outputDir)
				fmt.Println(errStyle.Render("✗ go mod tidy failed, project rolled back."))
				return err
			}

			fmt.Println()
			fmt.Println(successStyle.Render("✓ Project ready: " + outputDir))
			fmt.Println()

			if err := envform.Run(data, outputDir); err != nil {
				return err
			}

			fmt.Println(infoStyle.Render("→ Checking required tools..."))
			fmt.Println()
			tooling.CheckAndInstall(data.HasGRPC)

			fmt.Println()
			fmt.Println(dimStyle.Render("  cd " + outputDir))
			fmt.Println(dimStyle.Render("  task run-http"))
			fmt.Println()

			return nil
		}
	},
}
