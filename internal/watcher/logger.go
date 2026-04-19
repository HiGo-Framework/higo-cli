package watcher

import (
	"encoding/json"

	"github.com/bytedance/sonic"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
)

type tag struct {
	style lipgloss.Style
	label string
}

func (t tag) Render() string {
	return t.style.Render(t.label)
}

type Logger struct {
	watchTag   tag
	buildTag   tag
	runTag     tag
	changeTag  tag
	errTag     tag
	appTag     tag
	excludeTag tag
	timeStyle  lipgloss.Style
	msgStyle   lipgloss.Style
	errMsg     lipgloss.Style
	changeMsg  lipgloss.Style
}

func newLogger() *Logger {
	mkTag := func(bg, fg, label string) tag {
		return tag{
			label: label,
			style: lipgloss.NewStyle().
				Background(lipgloss.Color(bg)).
				Foreground(lipgloss.Color(fg)).
				Bold(true).
				Width(8).
				Align(lipgloss.Center),
		}
	}
	return &Logger{
		watchTag:   mkTag("6", "0", "watch"),
		buildTag:   mkTag("3", "0", "build"),
		runTag:     mkTag("2", "0", "run"),
		changeTag:  mkTag("5", "15", "change"),
		errTag:     mkTag("1", "15", "error"),
		appTag:     mkTag("8", "15", "app"),
		excludeTag: mkTag("4", "15", "excl"),
		timeStyle:  lipgloss.NewStyle().Foreground(lipgloss.Color("8")),
		msgStyle:   lipgloss.NewStyle().Foreground(lipgloss.Color("15")),
		errMsg:     lipgloss.NewStyle().Foreground(lipgloss.Color("9")),
		changeMsg:  lipgloss.NewStyle().Foreground(lipgloss.Color("13")).Bold(true),
	}
}

func (l *Logger) ts() string {
	return l.timeStyle.Render(time.Now().Format("15:04:05"))
}

func (l *Logger) line(t tag, msg lipgloss.Style, text string) {
	fmt.Printf("%s %s  %s\n", l.ts(), t.Render(), msg.Render(text))
}

func (l *Logger) Watching(dir string)  { l.line(l.watchTag, l.msgStyle, "watching "+dir) }
func (l *Logger) Exclude(dir string)   { l.line(l.excludeTag, l.msgStyle, "!exclude "+dir) }
func (l *Logger) Building()            { l.line(l.buildTag, l.msgStyle, "building...") }
func (l *Logger) Running()             { l.line(l.runTag, l.msgStyle, "running...") }
func (l *Logger) Changed(file string)  { l.line(l.changeTag, l.changeMsg, file+" has changed") }
func (l *Logger) Error(msg string)     { l.line(l.errTag, l.errMsg, msg) }
func (l *Logger) BuildFailed(out string) {
	fmt.Printf("%s %s\n%s\n", l.ts(), l.errTag.Render(), l.errMsg.Render(out))
}

// coreFields are extracted separately; everything else is printed inline.
var coreFields = map[string]bool{
	"time": true, "level": true,
}

// fxNoise are fx DI container lifecycle messages that add no value in the watcher output.
var fxNoise = map[string]bool{
	"provided": true, "decorated": true, "supplied": true,
	"running": true, "started": true, "stopping": true, "stopped": true,
	"initialized custom fxevent.Logger": true,
}

func (l *Logger) App(line string) {
	var raw map[string]json.RawMessage
	if err := sonic.Unmarshal([]byte(line), &raw); err != nil || raw["level"] == nil {
		l.line(l.appTag, l.msgStyle, line)
		return
	}

	level := jsonStr(raw["level"])
	msg := jsonStr(raw["msg"])
	if fxNoise[msg] {
		return
	}

	ts := l.buildTimestamp(jsonStr(raw["time"]))
	lvlTag := l.levelTag(level)

	keyStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	valStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("15"))
	errValStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("9"))

	var parts []string
	parts = append(parts, l.msgStyle.Render(msg))
	for k, v := range raw {
		if coreFields[k] || k == "msg" {
			continue
		}
		val := jsonStr(v)
		vs := valStyle
		if k == "error" {
			vs = errValStyle
		}
		parts = append(parts, keyStyle.Render(k+"=")+vs.Render(val))
	}

	fmt.Printf("%s %s  %s\n", ts, lvlTag, strings.Join(parts, "  "))
}

// jsonStr extracts a plain string from a JSON raw value,
// stripping surrounding quotes. Non-string values are returned as-is.
func jsonStr(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var s string
	if err := sonic.Unmarshal(raw, &s); err == nil {
		return s
	}
	return string(raw)
}

func (l *Logger) buildTimestamp(raw string) string {
	t, err := time.Parse(time.RFC3339Nano, raw)
	if err != nil {
		return l.timeStyle.Render(time.Now().Format("15:04:05"))
	}
	return l.timeStyle.Render(t.Format("15:04:05"))
}


func (l *Logger) levelTag(level string) string {
	var bg, fg string
	switch strings.ToUpper(level) {
	case "ERROR":
		bg, fg = "1", "15"
	case "WARN", "WARNING":
		bg, fg = "3", "0"
	case "INFO":
		bg, fg = "2", "0"
	default:
		bg, fg = "8", "15"
	}
	return lipgloss.NewStyle().
		Background(lipgloss.Color(bg)).
		Foreground(lipgloss.Color(fg)).
		Bold(true).
		Width(8).
		Align(lipgloss.Center).
		Render(strings.ToUpper(level))
}
