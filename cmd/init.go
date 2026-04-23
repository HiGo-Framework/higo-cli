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

type initOpts struct {
	module    string
	appName   string
	services  string
	pyroscope bool
	yes       bool
	skipEnv   bool
	skipTools bool
}

var initFlags initOpts

var initCmd = &cobra.Command{
	Use:   "init [project-name]",
	Short: "Initialize a new higo project (interactive or flag-driven)",
	Long: `Initialize a new higo project.

By default, runs an interactive wizard. Pass --yes to skip prompts and drive
the flow entirely from flags — useful for CI, scripts, and automation.

Headless example:
  higo init my-app \
    --module github.com/you/my-app \
    --services grpc \
    --yes

Flags:
  --module       Go module path (default: github.com/you/<project-name>)
  --app-name     App name for telemetry (default: <project-name>)
  --services     Comma-separated list: http,grpc,consumer (default: http)
  --pyroscope    Enable Pyroscope profiling (default: false)
  --yes, -y      Skip all prompts (requires project-name arg)
  --skip-env     Don't create .env (headless implies this unless combined)
  --skip-tools   Don't run the tool-install check`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if initFlags.yes {
			return runHeadless(args, &initFlags)
		}
		return runInteractive(args)
	},
}

func init() {
	initCmd.Flags().StringVarP(&initFlags.module, "module", "m", "", "Go module path (e.g. github.com/you/my-app)")
	initCmd.Flags().StringVar(&initFlags.appName, "app-name", "", "App name for telemetry (defaults to project name)")
	initCmd.Flags().StringVarP(&initFlags.services, "services", "s", "", "Comma-separated services: http,grpc,consumer")
	initCmd.Flags().BoolVar(&initFlags.pyroscope, "pyroscope", false, "Enable Pyroscope profiling")
	initCmd.Flags().BoolVarP(&initFlags.yes, "yes", "y", false, "Skip all prompts (requires project-name)")
	initCmd.Flags().BoolVar(&initFlags.skipEnv, "skip-env", false, "Don't create .env file")
	initCmd.Flags().BoolVar(&initFlags.skipTools, "skip-tools", false, "Skip the required-tools check")
}

// runHeadless skips all prompts. Requires project-name arg.
func runHeadless(args []string, f *initOpts) error {
	if len(args) == 0 {
		return fmt.Errorf("--yes requires a project-name argument")
	}
	projectName := args[0]

	data := &wizard.ProjectData{
		ProjectName:  projectName,
		ModuleName:   firstNonEmpty(f.module, "github.com/you/"+projectName),
		AppName:      firstNonEmpty(f.appName, projectName),
		HasPyroscope: f.pyroscope,
	}
	applyServicesFlag(data, f.services)

	if !data.HasHTTP && !data.HasGRPC && !data.HasConsumer {
		return fmt.Errorf("--services must include at least one of: http, grpc, consumer")
	}
	count := 0
	for _, v := range []bool{data.HasHTTP, data.HasGRPC, data.HasConsumer} {
		if v {
			count++
		}
	}
	data.HasMix = count > 1

	outputDir := data.ProjectName
	if _, err := os.Stat(outputDir); !os.IsNotExist(err) {
		return fmt.Errorf("directory %q already exists", outputDir)
	}

	dimStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	successStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("10"))
	errStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("9"))

	fmt.Println(dimStyle.Render("→ generating project " + projectName + "…"))
	if err := generator.Generate(outputDir, data); err != nil {
		return err
	}

	fmt.Println(dimStyle.Render("→ go mod tidy…"))
	if err := runner.RunTidyPlain(outputDir); err != nil {
		os.RemoveAll(outputDir)
		fmt.Println(errStyle.Render("✗ go mod tidy failed, project rolled back."))
		return err
	}

	fmt.Println(successStyle.Render("✓ Project ready: " + outputDir))
	return nil
}

// runInteractive is the existing wizard-driven flow.
func runInteractive(args []string) error {
	labelStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	valueStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("15"))
	pathStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("14"))
	infoStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("12"))
	successStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("10"))
	errStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("9"))
	dimStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("8"))

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
}

func firstNonEmpty(a, b string) string {
	if strings.TrimSpace(a) != "" {
		return a
	}
	return b
}

// applyServicesFlag parses a CSV services list and sets the HasXXX flags.
// Empty string defaults to http-only for backwards compatibility.
func applyServicesFlag(data *wizard.ProjectData, csv string) {
	csv = strings.TrimSpace(csv)
	if csv == "" {
		data.HasHTTP = true
		return
	}
	for _, s := range strings.Split(csv, ",") {
		switch strings.ToLower(strings.TrimSpace(s)) {
		case "http":
			data.HasHTTP = true
		case "grpc":
			data.HasGRPC = true
		case "consumer":
			data.HasConsumer = true
		}
	}
}
