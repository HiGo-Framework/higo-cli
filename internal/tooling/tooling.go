package tooling

import (
	"fmt"
	"os/exec"

	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/lipgloss"
)

type Tool struct {
	Name        string
	Description string
	InstallCmd  []string
}

var requiredTools = []Tool{
	{
		Name:        "task",
		Description: "Taskfile runner — used to run build/run commands (task run-http, etc.)",
		InstallCmd:  []string{"go", "install", "github.com/go-task/task/v3/cmd/task@latest"},
	},
}

var optionalGrpcTools = []Tool{
	{
		Name:        "buf",
		Description: "Protobuf toolkit — used to generate Go code from .proto files (task proto-gen)",
		InstallCmd:  []string{"go", "install", "github.com/bufbuild/buf/cmd/buf@latest"},
	},
}

func CheckAndInstall(hasGRPC bool) {
	tools := requiredTools
	if hasGRPC {
		tools = append(tools, optionalGrpcTools...)
	}

	warnStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("11"))
	okStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("10"))
	dimStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	errStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("9"))

	var missing []Tool
	for _, t := range tools {
		if _, err := exec.LookPath(t.Name); err != nil {
			missing = append(missing, t)
		} else {
			fmt.Println(okStyle.Render("✓ " + t.Name + " is already installed"))
		}
	}

	if len(missing) == 0 {
		return
	}

	fmt.Println()
	fmt.Println(warnStyle.Render("⚠ The following tools are required but not installed:"))
	for _, t := range missing {
		fmt.Println(dimStyle.Render("  • " + t.Name + " — " + t.Description))
	}
	fmt.Println()

	var skipped []Tool
	for _, t := range missing {
		var install bool
		if err := huh.NewConfirm().
			Title("Install " + t.Name + "?").
			Description(t.Description).
			Value(&install).
			Run(); err != nil || !install {
			skipped = append(skipped, t)
			continue
		}

		fmt.Println(dimStyle.Render("  → running: " + fmt.Sprint(t.InstallCmd)))
		c := exec.Command(t.InstallCmd[0], t.InstallCmd[1:]...)
		if out, err := c.CombinedOutput(); err != nil {
			fmt.Println(errStyle.Render("  ✗ failed to install "+t.Name+": "+err.Error()))
			fmt.Println(dimStyle.Render(string(out)))
			skipped = append(skipped, t)
		} else {
			fmt.Println(okStyle.Render("  ✓ " + t.Name + " installed"))
		}
	}

	if len(skipped) > 0 {
		fmt.Println()
		fmt.Println(warnStyle.Render("⚠ Warning: the following tools were not installed:"))
		for _, t := range skipped {
			fmt.Println(errStyle.Render("  • " + t.Name))
			fmt.Println(dimStyle.Render("    Install manually: " + fmt.Sprint(t.InstallCmd)))
		}
		fmt.Println(warnStyle.Render("  You must install them before running task commands."))
	}
}
