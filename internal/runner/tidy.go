package runner

import (
	"bufio"
	"fmt"
	"os/exec"
	"strings"
	"sync"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

const maxLines = 5
const boxWidth = 62

type lineMsg string
type doneMsg struct{ err error }

type tidyModel struct {
	spinner spinner.Model
	lines   []string
	done    bool
	err     error
	lineCh  <-chan string
	doneCh  <-chan error
}

func (m tidyModel) Init() tea.Cmd {
	return tea.Batch(m.spinner.Tick, m.waitNext())
}

func (m tidyModel) waitNext() tea.Cmd {
	return func() tea.Msg {
		select {
		case line, ok := <-m.lineCh:
			if !ok {
				return doneMsg{err: <-m.doneCh}
			}
			return lineMsg(line)
		case err := <-m.doneCh:
			return doneMsg{err: err}
		}
	}
}

func (m tidyModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd
	case lineMsg:
		if line := strings.TrimSpace(string(msg)); line != "" {
			m.lines = append(m.lines, line)
			if len(m.lines) > maxLines {
				m.lines = m.lines[len(m.lines)-maxLines:]
			}
		}
		return m, m.waitNext()
	case doneMsg:
		m.done = true
		m.err = msg.err
		return m, tea.Quit
	case tea.KeyMsg:
		if msg.Type == tea.KeyCtrlC {
			return m, tea.Quit
		}
	}
	return m, nil
}

func (m tidyModel) View() string {
	if m.done {
		return ""
	}

	lineStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("8")).
		Width(boxWidth - 4)

	var sb strings.Builder
	for i := range maxLines {
		if i < len(m.lines) {
			sb.WriteString(lineStyle.Render(m.lines[i]))
		} else {
			sb.WriteString(lineStyle.Render(" "))
		}
		if i < maxLines-1 {
			sb.WriteString("\n")
		}
	}

	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("8")).
		Width(boxWidth).
		Padding(0, 1).
		Render(sb.String())

	statusStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("12"))
	return fmt.Sprintf("%s\n%s\n", box, statusStyle.Render(m.spinner.View()+" running go mod tidy..."))
}

func RunTidy(dir string) error {
	get := exec.Command("go", "get", "github.com/triasbrata/higo-framework@latest")
	get.Dir = dir
	if out, err := get.CombinedOutput(); err != nil {
		return fmt.Errorf("go get higo-framework@latest: %w\n%s", err, out)
	}

	cmd := exec.Command("go", "mod", "tidy")
	cmd.Dir = dir

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return err
	}

	if err := cmd.Start(); err != nil {
		return err
	}

	lineCh := make(chan string, 100)
	doneCh := make(chan error, 1)

	go func() {
		var wg sync.WaitGroup
		wg.Add(2)
		scan := func(r interface{ Read([]byte) (int, error) }) {
			defer wg.Done()
			sc := bufio.NewScanner(r)
			for sc.Scan() {
				lineCh <- sc.Text()
			}
		}
		go scan(stdout)
		go scan(stderr)
		wg.Wait()
		close(lineCh)
		doneCh <- cmd.Wait()
	}()

	s := spinner.New()
	s.Spinner = spinner.Dot
	s.Style = lipgloss.NewStyle().Foreground(lipgloss.Color("12"))

	m := tidyModel{spinner: s, lineCh: lineCh, doneCh: doneCh}
	p := tea.NewProgram(m)
	final, err := p.Run()
	if err != nil {
		return err
	}
	return final.(tidyModel).err
}
