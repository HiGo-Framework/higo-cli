package logviewer

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

const (
	headerHeight = 1
	footerHeight = 3 // border + filter bar + help bar
)

// Model is the bubbletea model for the live log viewer TUI.
type Model struct {
	entries  []Entry
	filtered []Entry

	viewport   viewport.Model
	regexInput textinput.Model

	logCh   <-chan Entry
	logFile string

	activeLevel Level
	compiledRe  *regexp.Regexp
	reErr       string
	regexFocus   bool // true = typing in regex, false = level selector active
	mouseEnabled bool // true = bubbletea captures mouse (wheel scroll); false = native text selection

	lastClickAt  time.Time // for double-click detection
	mouseTempOff bool      // true when double-click temporarily disabled mouse

	width  int
	height int
	ready  bool
	follow bool
}

type newEntryMsg Entry
type channelClosedMsg struct{}
type clearSelectionMsg struct{}

// clearSelectionCmd briefly claims then releases mouse to force the terminal to drop any text selection.
func clearSelectionCmd() tea.Cmd {
	return func() tea.Msg { return clearSelectionMsg{} }
}

// New creates the initial TUI model.
func New(logCh <-chan Entry, logFile string) Model {
	ti := textinput.New()
	ti.Placeholder = "regex filter…"
	ti.Focus()
	ti.CharLimit = 200
	ti.Width = 35

	return Model{
		logCh:       logCh,
		logFile:     logFile,
		activeLevel: LevelAll,
		regexFocus:  true,
		follow:      true,
		regexInput:  ti,
	}
}

// waitForEntry is the bubbletea Cmd that blocks until the next log entry arrives.
func waitForEntry(ch <-chan Entry) tea.Cmd {
	return func() tea.Msg {
		e, ok := <-ch
		if !ok {
			return channelClosedMsg{}
		}
		return newEntryMsg(e)
	}
}

func (m Model) Init() tea.Cmd {
	return tea.Batch(textinput.Blink, waitForEntry(m.logCh))
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		vpHeight := msg.Height - headerHeight - footerHeight
		if vpHeight < 1 {
			vpHeight = 1
		}
		m.regexInput.Width = msg.Width/2 - 6
		if !m.ready {
			m.viewport = viewport.New(msg.Width, vpHeight)
			m.viewport.SetContent(m.renderLogs())
			m.ready = true
		} else {
			m.viewport.Width = msg.Width
			m.viewport.Height = vpHeight
			m.viewport.SetContent(m.renderLogs())
		}

	case newEntryMsg:
		e := Entry(msg)
		m.entries = append(m.entries, e)
		if e.Matches(m.compiledRe, m.activeLevel) {
			m.filtered = append(m.filtered, e)
			if m.ready {
				m.viewport.SetContent(m.renderLogs())
				if m.follow {
					m.viewport.GotoBottom()
				}
			}
		}
		cmds = append(cmds, waitForEntry(m.logCh))

	case channelClosedMsg:
		return m, tea.Quit

	case clearSelectionMsg:
		if !m.mouseEnabled {
			return m, tea.DisableMouse
		}

	case tea.MouseMsg:
		if m.mouseEnabled && m.ready {
			switch msg.Action {
			case tea.MouseActionPress:
				switch msg.Button {
				case tea.MouseButtonLeft:
					if time.Since(m.lastClickAt) < 400*time.Millisecond {
						// double-click: temp disable mouse so user can select text; re-enables on next scroll key
						m.mouseTempOff = true
						m.lastClickAt = time.Time{}
						return m, tea.Batch(append(cmds, tea.DisableMouse)...)
					}
					m.lastClickAt = time.Now()
				case tea.MouseButtonWheelUp:
					m.viewport.ScrollUp(3)
					m.follow = false
				case tea.MouseButtonWheelDown:
					m.viewport.ScrollDown(3)
				}
			}
		}

	case tea.KeyMsg:
		key := msg.String()

		if key == "ctrl+c" {
			return m, tea.Quit
		}

		if key == "tab" {
			m.regexFocus = !m.regexFocus
			if m.regexFocus {
				m.regexInput.Focus()
			} else {
				m.regexInput.Blur()
			}
			return m, tea.Batch(cmds...)
		}

		// scroll keys always work regardless of focus
		switch key {
		case "pgup", "pgdown", "home", "end", "up", "down":
			if m.mouseTempOff {
				// first press clears the text selection; next press will scroll
				m.mouseTempOff = false
				return m, tea.Batch(append(cmds, tea.EnableMouseCellMotion)...)
			}
			if m.ready {
				var vpCmd tea.Cmd
				m.viewport, vpCmd = m.viewport.Update(msg)
				cmds = append(cmds, vpCmd)
				if key == "pgup" || key == "up" {
					m.follow = false
				}
			}
			if !m.mouseEnabled {
				// briefly claim mouse ownership to force terminal to drop any text selection
				cmds = append(cmds, tea.EnableMouseCellMotion, clearSelectionCmd())
			}
			return m, tea.Batch(cmds...)
		}

		if m.regexFocus {
			switch key {
			case "esc":
				m.regexInput.SetValue("")
				m.applyFilter()
			case "enter":
				m.applyFilter()
			default:
				prev := m.regexInput.Value()
				var tiCmd tea.Cmd
				m.regexInput, tiCmd = m.regexInput.Update(msg)
				cmds = append(cmds, tiCmd)
				if m.regexInput.Value() != prev {
					m.applyFilter()
				}
			}
		} else {
			switch key {
			case "q":
				return m, tea.Quit
			case "left", "h":
				m.prevLevel()
				m.applyFilter()
			case "right", "l":
				m.nextLevel()
				m.applyFilter()
			case "f":
				m.follow = !m.follow
				if m.follow && m.ready {
					m.viewport.GotoBottom()
				}
			}
		}

	default:
		if m.ready {
			var vpCmd tea.Cmd
			m.viewport, vpCmd = m.viewport.Update(msg)
			cmds = append(cmds, vpCmd)
		}
	}

	return m, tea.Batch(cmds...)
}

func (m *Model) nextLevel() {
	for i, l := range Levels {
		if l == m.activeLevel {
			m.activeLevel = Levels[(i+1)%len(Levels)]
			return
		}
	}
}

func (m *Model) prevLevel() {
	for i, l := range Levels {
		if l == m.activeLevel {
			m.activeLevel = Levels[(i-1+len(Levels))%len(Levels)]
			return
		}
	}
}

func (m *Model) applyFilter() {
	m.reErr = ""
	raw := strings.TrimSpace(m.regexInput.Value())
	if raw != "" {
		re, err := regexp.Compile(raw)
		if err != nil {
			m.reErr = err.Error()
			return
		}
		m.compiledRe = re
	} else {
		m.compiledRe = nil
	}

	m.filtered = m.filtered[:0]
	for _, e := range m.entries {
		if e.Matches(m.compiledRe, m.activeLevel) {
			m.filtered = append(m.filtered, e)
		}
	}

	if m.ready {
		m.viewport.SetContent(m.renderLogs())
		if m.follow {
			m.viewport.GotoBottom()
		}
	}
}

func (m Model) renderLogs() string {
	var sb strings.Builder
	for _, e := range m.filtered {
		sb.WriteString(e.Rendered(m.width))
		sb.WriteByte('\n')
	}
	return sb.String()
}

func (m Model) View() string {
	if !m.ready {
		return "\n  Initializing…"
	}
	return m.headerView() + "\n" + m.viewport.View() + "\n" + m.footerView()
}

// ── styles ────────────────────────────────────────────────────────────────────

var (
	headerBg = lipgloss.NewStyle().
			Bold(true).
			Background(lipgloss.Color("12")).
			Foreground(lipgloss.Color("0")).
			Padding(0, 1)

	headerMeta = lipgloss.NewStyle().
			Foreground(lipgloss.Color("8")).
			Background(lipgloss.Color("0")).
			Padding(0, 1)

	footerBorder = lipgloss.NewStyle().
			BorderStyle(lipgloss.NormalBorder()).
			BorderTop(true).
			BorderForeground(lipgloss.Color("8"))

	levelActive = lipgloss.NewStyle().
			Background(lipgloss.Color("4")).
			Foreground(lipgloss.Color("15")).
			Bold(true).
			Padding(0, 1)

	levelInactive = lipgloss.NewStyle().
			Background(lipgloss.Color("0")).
			Foreground(lipgloss.Color("8")).
			Padding(0, 1)

	followOn  = lipgloss.NewStyle().Foreground(lipgloss.Color("10")).Bold(true)
	followOff = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	helpStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	reErrStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("9"))
)

func (m Model) headerView() string {
	title := headerBg.Render("higo live logs")
	meta := headerMeta.Render(fmt.Sprintf("%d/%d  log→ %s", len(m.filtered), len(m.entries), m.logFile))
	gap := m.width - lipgloss.Width(title) - lipgloss.Width(meta)
	if gap < 0 {
		gap = 0
	}
	return title + strings.Repeat(" ", gap) + meta
}

func (m Model) footerView() string {
	// regex section
	prompt := lipgloss.NewStyle().Foreground(lipgloss.Color("12")).Bold(true).Render("/")
	reSection := prompt + " " + m.regexInput.View()
	if m.reErr != "" {
		reSection += "  " + reErrStyle.Render("⚠ "+m.reErr)
	}

	// level pills
	var pills []string
	for _, l := range Levels {
		if l == m.activeLevel {
			pills = append(pills, levelActive.Render(string(l)))
		} else {
			pills = append(pills, levelInactive.Render(string(l)))
		}
	}
	levelBar := strings.Join(pills, " ")

	// follow indicator
	var followStr string
	if m.follow {
		followStr = followOn.Render("↓follow")
	} else {
		followStr = followOff.Render("↓follow")
	}

	// mouse mode indicator
	var mouseStr string
	switch {
	case m.mouseTempOff:
		mouseStr = lipgloss.NewStyle().Foreground(lipgloss.Color("11")).Bold(true).Render("[M]ouse:selecting…")
	case m.mouseEnabled:
		mouseStr = followOn.Render("[M]ouse:on")
	default:
		mouseStr = followOff.Render("[M]ouse:off")
	}

	filterLine := reSection + "   " + levelBar + "   " + followStr

	var focusHint string
	if m.regexFocus {
		focusHint = "[regex]  tab→level"
	} else {
		focusHint = "[level ←→]  tab→regex  f=follow  q=quit"
	}
	helpLine := helpStyle.Render("  ") + mouseStr + helpStyle.Render("  ") + focusHint + helpStyle.Render("  pgup/pgdn=scroll  ctrl+c=quit")

	content := filterLine + "\n" + helpLine
	return footerBorder.Width(m.width).Render(content)
}
