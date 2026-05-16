package tui

import (
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/katistix/ephemeral-linux/internal/docker"
)

type state int

const (
	stateForm state = iota
	stateStarting
	stateRunning
	stateError
)

type tickMsg time.Time

type startedMsg struct{ err error }

type statusMsg struct {
	status docker.Status
	err    error
}

type Model struct {
	manager       *docker.Manager
	state         state
	userInput     textinput.Model
	passInput     textinput.Model
	status        docker.Status
	err           error
	spinner       spinner.Model
	setupLines    []string
	setupStarted  time.Time
	stepStartedAt time.Time
	stepDurations map[string]time.Duration
	confirmQuit   bool
}

func NewModel(manager *docker.Manager) Model {
	userDefault, passDefault := manager.Defaults()

	user := textinput.New()
	user.Placeholder = "username"
	user.SetValue(userDefault)
	user.Focus()
	user.CharLimit = 32
	user.Width = 24

	pass := textinput.New()
	pass.Placeholder = "password"
	pass.SetValue(passDefault)
	pass.CharLimit = 64
	pass.Width = 24
	pass.EchoMode = textinput.EchoPassword
	pass.EchoCharacter = '•'

	spin := spinner.New()
	spin.Spinner = spinner.Dot
	return Model{manager: manager, state: stateForm, userInput: user, passInput: pass, spinner: spin, stepDurations: map[string]time.Duration{}}
}

func (m Model) Init() tea.Cmd { return tea.Batch(textinput.Blink, m.spinner.Tick) }

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd
	case tea.KeyMsg:
		switch m.state {
		case stateForm:
			switch msg.String() {
			case "ctrl+c", "q":
				return m, tea.Quit
			case "tab", "shift+tab", "up", "down":
				if m.userInput.Focused() {
					m.userInput.Blur()
					m.passInput.Focus()
				} else {
					m.passInput.Blur()
					m.userInput.Focus()
				}
				return m, nil
			case "enter":
				m.state = stateStarting
				m.err = nil
				m.confirmQuit = false
				m.setupLines = []string{"creating container"}
				now := time.Now()
				m.setupStarted = now
				m.stepStartedAt = now
				m.stepDurations = map[string]time.Duration{}
				return m, m.startCmd()
			}
		case stateRunning:
			if m.confirmQuit {
				switch msg.String() {
				case "y":
					return m, tea.Quit
				case "n", "esc", "q", "enter":
					m.confirmQuit = false
					return m, nil
				}
				return m, nil
			}
			switch msg.String() {
			case "ctrl+c", "q":
				m.confirmQuit = true
				return m, nil
			case "c":
				if m.passInput.EchoMode == textinput.EchoPassword {
					m.passInput.EchoMode = textinput.EchoNormal
				} else {
					m.passInput.EchoMode = textinput.EchoPassword
				}
				return m, nil
			}
		case stateError:
			switch msg.String() {
			case "ctrl+c", "q":
				return m, tea.Quit
			case "r":
				m.state = stateForm
				m.err = nil
				return m, nil
			}
		case stateStarting:
			if msg.String() == "ctrl+c" || msg.String() == "q" {
				return m, tea.Quit
			}
		}

	case startedMsg:
		if msg.err != nil {
			m.err = msg.err
			m.state = stateError
			return m, nil
		}
		m.stepDurations["creating container"] = time.Since(m.stepStartedAt)
		m.setupLines = appendSetupLine(m.setupLines, "starting ssh service")
		m.stepStartedAt = time.Now()
		return m, tea.Batch(m.statusCmd(), tickCmd())
	case statusMsg:
		if msg.err != nil {
			m.err = msg.err
			m.state = stateError
			return m, nil
		}
		m.status = msg.status
		if m.state == stateStarting && m.status.SSHReady {
			m.stepDurations["starting ssh service"] = time.Since(m.stepStartedAt)
			m.state = stateRunning
		}
		return m, nil
	case tickMsg:
		if m.state == stateRunning || m.state == stateStarting {
			return m, tea.Batch(m.statusCmd(), tickCmd())
		}
	}

	if m.state == stateForm {
		var cmd tea.Cmd
		if m.userInput.Focused() {
			m.userInput, cmd = m.userInput.Update(msg)
		} else {
			m.passInput, cmd = m.passInput.Update(msg)
		}
		return m, cmd
	}

	return m, nil
}

func (m Model) View() string {
	switch m.state {
	case stateStarting:
		lines := []string{
			titleStyle.Render("ephemeral-linux"),
			"",
			"setup",
			"",
		}
		for i, line := range m.setupLines {
			prefix := "✓ "
			durationText := ""
			if d, ok := m.stepDurations[line]; ok {
				durationText = mutedStyle.Render(" (" + humanDuration(d) + ")")
			} else if i == len(m.setupLines)-1 {
				prefix = m.spinner.View() + " "
				durationText = mutedStyle.Render(" (" + humanDuration(time.Since(m.stepStartedAt)) + ")")
			}
			lines = append(lines, prefix+line+durationText)
		}
		if m.status.ContainerName != "" {
			lines = append(lines, "", row("Container", valueOrDash(m.status.ContainerName)), row("SSH Port", valueOrDash(m.status.HostPort)))
		}
		return docStyle.Render(strings.Join(lines, "\n"))
	case stateRunning:
		pass := strings.Repeat("•", max(4, len(m.status.Password)))
		if m.passInput.EchoMode == textinput.EchoNormal {
			pass = m.status.Password
		}
		statusText := "running"
		if m.status.SSHReady {
			statusText = "ready"
		}
		lines := []string{
			titleStyle.Render("ephemeral-linux"),
			"",
			row("Status", statusText),
			row("Phase", valueOrDash(m.status.Phase)),
			row("Container", m.status.ContainerName),
			row("ID", m.status.ContainerID),
			row("Uptime", valueOrDash(m.status.Uptime)),
			row("SSH Port", valueOrDash(m.status.HostPort)),
			row("User", m.status.Username),
			row("Pass", pass),
			"",
			mutedStyle.Render("run this script to connect:"),
			commandStyle.Render(m.status.SSHCommand),
			"",
		}
		if m.confirmQuit {
			lines = append(lines, errorStyle.Render("quit and remove the container? [y/N] enter = no"))
		} else {
			lines = append(lines, mutedStyle.Render("c toggle password • q quit"))
		}
		return docStyle.Render(strings.Join(lines, "\n"))
	case stateError:
		return docStyle.Render(titleStyle.Render("ephemeral-linux") + "\n\n" + errorStyle.Render(m.err.Error()) + "\n\n" + mutedStyle.Render("r back • q quit"))
	default:
		return docStyle.Render(strings.Join([]string{
			titleStyle.Render("ephemeral-linux"),
			"",
			"SSH username",
			m.userInput.View(),
			"",
			"SSH password",
			m.passInput.View(),
			"",
			mutedStyle.Render("enter launch • tab switch • q quit"),
		}, "\n"))
	}
}

func (m Model) Close() error { return m.manager.Stop() }

func (m Model) startCmd() tea.Cmd {
	user := m.userInput.Value()
	pass := m.passInput.Value()
	return func() tea.Msg { return startedMsg{err: m.manager.Start(user, pass)} }
}

func (m Model) statusCmd() tea.Cmd {
	return func() tea.Msg {
		status, err := m.manager.Status()
		return statusMsg{status: status, err: err}
	}
}

func tickCmd() tea.Cmd {
	return tea.Tick(1*time.Second, func(t time.Time) tea.Msg { return tickMsg(t) })
}

func row(label, value string) string { return mutedStyle.Render(label+":") + " " + value }
func valueOrDash(v string) string {
	if strings.TrimSpace(v) == "" {
		return "-"
	}
	return v
}
func humanDuration(d time.Duration) string {
	return d.Round(time.Second).String()
}

func appendSetupLine(lines []string, line string) []string {
	if len(lines) > 0 && lines[len(lines)-1] == line {
		return lines
	}
	for _, existing := range lines {
		if existing == line {
			return lines
		}
	}
	return append(lines, line)
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

var (
	docStyle     = lipgloss.NewStyle().Padding(1, 2)
	titleStyle   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("212"))
	mutedStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
	errorStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("203"))
	commandStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("230")).Background(lipgloss.Color("236")).Padding(0, 1)
)
