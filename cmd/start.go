package cmd

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"
	"github.com/triasbrata/higo-cli/internal/logviewer"
	"github.com/triasbrata/higo-cli/internal/watcher"
)

const frameworkServerPkg = "github.com/triasbrata/higo-framework/server"

var startCmd = &cobra.Command{
	Use:   "start [http|grpc|consumer|mix]",
	Short: "Start a higo service with hot reload",
	Long: `Start a higo service and watch for file changes.
Rebuilds and restarts automatically after 1s of inactivity.

When no service is specified, higo detects it automatically:
  - runs cmd/mix if it exists (multiple services)
  - runs the single available cmd/<service> otherwise

Examples:
  higo start
  higo start http
  higo start mix --exclude gen,proto
  higo start grpc --delay 500ms`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		exclude, _ := cmd.Flags().GetStringSlice("exclude")
		delay, _ := cmd.Flags().GetDuration("delay")

		var svc string
		if len(args) > 0 {
			svc = strings.ToLower(args[0])
		} else {
			detected, err := detectService(".")
			if err != nil {
				return err
			}
			svc = detected
		}

		switch svc {
		case "http", "grpc", "consumer", "mix":
		default:
			return fmt.Errorf("unknown service %q — must be: http, grpc, consumer, mix", svc)
		}

		if err := validateFramework("."); err != nil {
			return err
		}

		printBanner()

		logFile := filepath.Join(os.TempDir(), fmt.Sprintf("higo-%s-%d.log", svc, os.Getpid()))
		logCh := make(chan logviewer.Entry, 500)

		w, err := watcher.New(watcher.Config{
			Service:    watcher.ServiceType(svc),
			Exclude:    exclude,
			BuildDelay: delay,
			LogCh:      logCh,
			LogFile:    logFile,
		})
		if err != nil {
			return err
		}

		watcherErr := make(chan error, 1)
		go func() {
			watcherErr <- w.Start()
			close(logCh)
		}()

		m := logviewer.New(logCh, logFile)
		p := tea.NewProgram(m, tea.WithAltScreen())
		if _, err := p.Run(); err != nil {
			w.Stop()
			return err
		}

		// TUI exited (user pressed q or channel closed). Ensure watcher is stopped.
		w.Stop()

		select {
		case err := <-watcherErr:
			return err
		case <-time.After(3 * time.Second):
			return nil
		}
	},
}

// detectService inspects cmd/ to determine which service to run.
func detectService(root string) (string, error) {
	cmdDir := filepath.Join(root, "cmd")

	if _, err := os.Stat(filepath.Join(cmdDir, "mix")); err == nil {
		return "mix", nil
	}

	known := []string{"http", "grpc", "consumer"}
	var found []string
	for _, svc := range known {
		if _, err := os.Stat(filepath.Join(cmdDir, svc)); err == nil {
			found = append(found, svc)
		}
	}

	switch len(found) {
	case 0:
		return "", fmt.Errorf("no service found in %s — expected cmd/http, cmd/grpc, cmd/consumer, or cmd/mix", cmdDir)
	case 1:
		return found[0], nil
	default:
		return "", fmt.Errorf(
			"multiple services found (%s) but no cmd/mix — specify one: higo start [http|grpc|consumer]",
			strings.Join(found, ", "),
		)
	}
}

// validateFramework ensures the project imports higo-framework server packages.
func validateFramework(root string) error {
	internalsDir := filepath.Join(root, "internals")
	if _, err := os.Stat(internalsDir); err != nil {
		return fmt.Errorf("internals/ directory not found — run higo start from the project root")
	}

	found := false
	_ = filepath.WalkDir(internalsDir, func(path string, d fs.DirEntry, err error) error {
		if found || err != nil || d.IsDir() || filepath.Ext(path) != ".go" {
			return nil
		}
		content, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		if strings.Contains(string(content), frameworkServerPkg) {
			found = true
		}
		return nil
	})

	if !found {
		return fmt.Errorf(
			"no higo-framework server implementation found in internals/\n"+
				"  expected import: %q", frameworkServerPkg,
		)
	}
	return nil
}

func printBanner() {
	banner := `
  _     _
 | |__ (_) __ _  ___
 | '_ \| |/ _` + "`" + ` |/ _ \
 | | | | | (_| | (_) |
 |_| |_|_|\__, |\___/
           |___/
`
	fmt.Println(lipgloss.NewStyle().Foreground(lipgloss.Color("12")).Bold(true).Render(banner))
	fmt.Println(lipgloss.NewStyle().Foreground(lipgloss.Color("8")).Render("  hot reload ready\n"))
}

func init() {
	startCmd.Flags().StringSliceP("exclude", "e", []string{}, "Additional directories to exclude (comma-separated)")
	startCmd.Flags().Duration("delay", time.Second, "Debounce delay before rebuilding after a file change")
	rootCmd.AddCommand(startCmd)
}
