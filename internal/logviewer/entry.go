package logviewer

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/bytedance/sonic"
	"github.com/charmbracelet/lipgloss"
)

type EntryKind uint8

const (
	KindApp EntryKind = iota
	KindSystem
)

type Level string

const (
	LevelAll   Level = "ALL"
	LevelDebug Level = "DEBUG"
	LevelInfo  Level = "INFO"
	LevelWarn  Level = "WARN"
	LevelError Level = "ERROR"
)

var Levels = []Level{LevelAll, LevelDebug, LevelInfo, LevelWarn, LevelError}

type Entry struct {
	Kind   EntryKind
	At     time.Time
	Level  Level
	Msg    string
	Fields map[string]string
	SysTag string
	Raw    string // original line, used for regex matching
}

// Rendered returns a lipgloss-styled string for display in the TUI viewport.
func (e Entry) Rendered(width int) string {
	ts := timeStyle.Render(e.At.Format("15:04:05"))

	var tagStr string
	if e.Kind == KindSystem {
		tagStr = sysTagStyle(e.SysTag).Render(padCenter(e.SysTag, 8))
	} else {
		tagStr = levelStyle(e.Level).Render(padCenter(string(e.Level), 8))
	}

	msg := msgStyle.Render(e.Msg)

	var extra []string
	for k, v := range e.Fields {
		vs := valStyle
		if k == "error" {
			vs = errValStyle
		}
		extra = append(extra, keyStyle.Render(k+"=")+vs.Render(v))
	}

	line := fmt.Sprintf("%s %s  %s", ts, tagStr, msg)
	if len(extra) > 0 {
		line += "  " + strings.Join(extra, "  ")
	}
	return line
}

// Matches reports whether the entry passes the regex and level filters.
// System entries always pass the level filter (only app entries are level-filtered).
func (e Entry) Matches(re *regexp.Regexp, level Level) bool {
	if e.Kind == KindApp && level != LevelAll && e.Level != level {
		return false
	}
	if re != nil && !re.MatchString(e.Raw) {
		return false
	}
	return true
}

// ParseApp parses a raw log line from the running service.
func ParseApp(line string) Entry {
	e := Entry{
		Kind: KindApp,
		At:   time.Now(),
		Raw:  line,
	}

	var raw map[string]json.RawMessage
	if err := sonic.Unmarshal([]byte(line), &raw); err != nil || raw["level"] == nil {
		e.Level = LevelInfo
		e.Msg = line
		return e
	}

	e.Level = Level(strings.ToUpper(jsonStr(raw["level"])))
	e.Msg = jsonStr(raw["msg"])

	if ts := jsonStr(raw["time"]); ts != "" {
		if t, err := time.Parse(time.RFC3339Nano, ts); err == nil {
			e.At = t
		}
	}

	e.Fields = make(map[string]string)
	for k, v := range raw {
		switch k {
		case "time", "level", "msg":
			continue
		}
		e.Fields[k] = jsonStr(v)
	}
	return e
}

// SysEntry creates a system-level entry (build, run, watch events).
func SysEntry(tag, msg string) Entry {
	return Entry{
		Kind:   KindSystem,
		At:     time.Now(),
		SysTag: tag,
		Msg:    msg,
		Raw:    fmt.Sprintf("[%s] %s", tag, msg),
	}
}

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

func padCenter(s string, width int) string {
	if len(s) >= width {
		return s[:width]
	}
	total := width - len(s)
	left := total / 2
	right := total - left
	return strings.Repeat(" ", left) + s + strings.Repeat(" ", right)
}

// lipgloss styles shared across entry rendering.
var (
	timeStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	msgStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("15"))
	keyStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	valStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("15"))
	errValStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("9"))
)

var sysTagColors = map[string][2]string{
	"watch":  {"6", "0"},
	"build":  {"3", "0"},
	"run":    {"2", "0"},
	"change": {"5", "15"},
	"error":  {"1", "15"},
	"excl":   {"4", "15"},
}

func sysTagStyle(tag string) lipgloss.Style {
	colors, ok := sysTagColors[tag]
	if !ok {
		colors = [2]string{"8", "15"}
	}
	return lipgloss.NewStyle().
		Background(lipgloss.Color(colors[0])).
		Foreground(lipgloss.Color(colors[1])).
		Bold(true).
		Width(8).
		Align(lipgloss.Center)
}

func levelStyle(level Level) lipgloss.Style {
	var bg, fg string
	switch level {
	case LevelError:
		bg, fg = "1", "15"
	case LevelWarn:
		bg, fg = "3", "0"
	case LevelInfo:
		bg, fg = "2", "0"
	default:
		bg, fg = "8", "15"
	}
	return lipgloss.NewStyle().
		Background(lipgloss.Color(bg)).
		Foreground(lipgloss.Color(fg)).
		Bold(true).
		Width(8).
		Align(lipgloss.Center)
}
