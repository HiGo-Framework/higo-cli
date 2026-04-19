package watcher

import (
	"bufio"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/fsnotify/fsnotify"
)

type Watcher struct {
	cfg    Config
	fsw    *fsnotify.Watcher
	log    *Logger
	procMu sync.Mutex
	proc   *os.Process
}

func New(cfg Config) (*Watcher, error) {
	cfg.applyDefaults()
	fsw, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}
	return &Watcher{
		cfg: cfg,
		fsw: fsw,
		log: newLogger(),
	}, nil
}

func (w *Watcher) Start() error {
	printBanner()

	if err := w.addDirs(w.cfg.RootDir); err != nil {
		return err
	}

	for _, dir := range w.fsw.WatchList() {
		w.log.Watching(dir)
	}
	for _, ex := range w.cfg.Exclude {
		w.log.Exclude(ex)
	}
	fmt.Println()

	w.buildAndRun()

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)

	var (
		debounce *time.Timer
		mu       sync.Mutex
	)

	for {
		select {
		case event, ok := <-w.fsw.Events:
			if !ok {
				return nil
			}

			// watch new directories as they are created
			if event.Has(fsnotify.Create) {
				if info, err := os.Stat(event.Name); err == nil && info.IsDir() && !w.isExcluded(event.Name) {
					if err := w.fsw.Add(event.Name); err == nil {
						w.log.Watching(event.Name)
					}
					continue
				}
			}

			if !isGoFile(event.Name) {
				continue
			}
			if !event.Has(fsnotify.Write | fsnotify.Create | fsnotify.Remove | fsnotify.Rename) {
				continue
			}

			w.log.Changed(filepath.Base(event.Name))

			mu.Lock()
			if debounce != nil {
				debounce.Stop()
			}
			debounce = time.AfterFunc(w.cfg.BuildDelay, func() {
				w.buildAndRun()
			})
			mu.Unlock()

		case err, ok := <-w.fsw.Errors:
			if !ok {
				return nil
			}
			w.log.Error(err.Error())

		case <-sig:
			fmt.Println()
			w.stopProc()
			return nil
		}
	}
}

func (w *Watcher) addDirs(root string) error {
	return filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if !d.IsDir() {
			return nil
		}
		if w.isExcluded(path) {
			return filepath.SkipDir
		}
		return w.fsw.Add(path)
	})
}

func (w *Watcher) isExcluded(path string) bool {
	base := filepath.Base(path)
	for _, ex := range w.cfg.Exclude {
		if base == ex || strings.HasPrefix(filepath.ToSlash(path), ex+"/") {
			return true
		}
	}
	return false
}

func (w *Watcher) buildAndRun() {
	srcPkg, outBin := w.cfg.buildArgs()

	if err := os.MkdirAll("tmp", 0o755); err != nil {
		w.log.Error("mkdir tmp: " + err.Error())
		return
	}

	w.log.Building()
	start := time.Now()

	cmd := exec.Command("go", "build", "-o", outBin, srcPkg)
	out, err := cmd.CombinedOutput()
	if err != nil {
		w.log.BuildFailed(string(out))
		return
	}

	elapsed := lipgloss.NewStyle().Foreground(lipgloss.Color("8")).Render(
		fmt.Sprintf("(%.2fs)", time.Since(start).Seconds()),
	)
	fmt.Printf("%s %s  build ok %s\n",
		w.log.ts(),
		w.log.buildTag.Render(),
		elapsed,
	)

	w.restart(outBin)
}

func (w *Watcher) restart(bin string) {
	w.stopProc()
	w.log.Running()

	cmd := exec.Command("./" + bin)
	cmd.Env = append(os.Environ(), w.cfg.envAppName())

	stdout, _ := cmd.StdoutPipe()
	stderr, _ := cmd.StderrPipe()

	if err := cmd.Start(); err != nil {
		w.log.Error("start failed: " + err.Error())
		return
	}

	w.procMu.Lock()
	w.proc = cmd.Process
	w.procMu.Unlock()

	go w.streamOutput(stdout)
	go w.streamOutput(stderr)
	go func() { _ = cmd.Wait() }()
}

func (w *Watcher) stopProc() {
	w.procMu.Lock()
	defer w.procMu.Unlock()
	if w.proc == nil {
		return
	}
	_ = w.proc.Signal(syscall.SIGTERM)
	done := make(chan struct{})
	go func() {
		_, _ = w.proc.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		_ = w.proc.Kill()
	}
	w.proc = nil
}

func (w *Watcher) streamOutput(r io.Reader) {
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		w.log.App(scanner.Text())
	}
}

func isGoFile(path string) bool {
	return filepath.Ext(path) == ".go"
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
	bannerStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("12")).Bold(true)
	subStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	fmt.Println(bannerStyle.Render(banner))
	fmt.Println(subStyle.Render("  hot reload ready\n"))
}
