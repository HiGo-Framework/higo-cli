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

	"github.com/fsnotify/fsnotify"
	"github.com/triasbrata/higo-cli/internal/logviewer"
)

type Watcher struct {
	cfg     Config
	fsw     *fsnotify.Watcher
	logFile *os.File
	procMu  sync.Mutex
	proc    *os.Process
	stopCh  chan struct{}
}

func New(cfg Config) (*Watcher, error) {
	cfg.applyDefaults()
	fsw, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}
	w := &Watcher{
		cfg:    cfg,
		fsw:    fsw,
		stopCh: make(chan struct{}),
	}
	if cfg.LogFile != "" {
		f, err := os.Create(cfg.LogFile)
		if err == nil {
			w.logFile = f
		}
	}
	return w, nil
}

// Stop signals the watcher to shut down. Safe to call from any goroutine.
func (w *Watcher) Stop() {
	select {
	case <-w.stopCh:
	default:
		close(w.stopCh)
	}
	w.stopProc()
	w.fsw.Close()
}

func (w *Watcher) Start() error {
	defer func() {
		if w.logFile != nil {
			_ = w.logFile.Close()
		}
	}()

	if err := w.addDirs(w.cfg.RootDir); err != nil {
		return err
	}

	for _, dir := range w.fsw.WatchList() {
		w.emit(logviewer.SysEntry("watch", "watching "+dir))
	}
	for _, ex := range w.cfg.Exclude {
		w.emit(logviewer.SysEntry("excl", "!exclude "+ex))
	}

	w.buildAndRun()

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)

	var (
		debounce *time.Timer
		mu       sync.Mutex
	)

	for {
		select {
		case <-w.stopCh:
			w.stopProc()
			return nil

		case event, ok := <-w.fsw.Events:
			if !ok {
				return nil
			}
			if event.Has(fsnotify.Create) {
				if info, err := os.Stat(event.Name); err == nil && info.IsDir() && !w.isExcluded(event.Name) {
					if err := w.fsw.Add(event.Name); err == nil {
						w.emit(logviewer.SysEntry("watch", "watching "+event.Name))
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
			w.emit(logviewer.SysEntry("change", filepath.Base(event.Name)+" has changed"))

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
			w.emit(logviewer.SysEntry("error", err.Error()))

		case <-sig:
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
		w.emit(logviewer.SysEntry("error", "mkdir tmp: "+err.Error()))
		return
	}

	w.emit(logviewer.SysEntry("build", "building…"))
	start := time.Now()

	cmd := exec.Command("go", "build", "-o", outBin, srcPkg)
	out, err := cmd.CombinedOutput()
	if err != nil {
		w.emit(logviewer.SysEntry("error", "build failed:\n"+string(out)))
		return
	}

	elapsed := fmt.Sprintf("(%.2fs)", time.Since(start).Seconds())
	w.emit(logviewer.SysEntry("build", "build ok "+elapsed))
	w.restart(outBin)
}

func (w *Watcher) restart(bin string) {
	w.stopProc()
	w.emit(logviewer.SysEntry("run", "running…"))

	cmd := exec.Command("./" + bin)
	cmd.Env = append(os.Environ(), w.cfg.envAppName())

	stdout, _ := cmd.StdoutPipe()
	stderr, _ := cmd.StderrPipe()

	if err := cmd.Start(); err != nil {
		w.emit(logviewer.SysEntry("error", "start failed: "+err.Error()))
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
		line := scanner.Text()
		e := logviewer.ParseApp(line)
		if fxNoise[e.Msg] {
			continue
		}
		w.emit(e)
		// always write raw line to log file for later inspection
		if w.logFile != nil {
			_, _ = fmt.Fprintln(w.logFile, line)
		}
	}
}

// emit sends an entry to the log channel (non-blocking drop on full buffer)
// and writes system entries to the log file.
func (w *Watcher) emit(e logviewer.Entry) {
	if w.cfg.LogCh != nil {
		select {
		case w.cfg.LogCh <- e:
		default:
		}
	}
	if w.logFile != nil && e.Kind == logviewer.KindSystem {
		_, _ = fmt.Fprintln(w.logFile, e.Raw)
	}
}

func isGoFile(path string) bool {
	return filepath.Ext(path) == ".go"
}
