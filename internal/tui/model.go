package tui

import (
	"strings"
	"time"

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
	manager   *docker.Manager
	state     state
	userInput textinput.Model
	passInput textinput.Model
	status    docker.Status
	err       error
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

	return Model{manager: manager, state: stateForm, userInput: user, passInput: pass}
}

func (m Model) Init() tea.Cmd { return textinput.Blink }

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
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
				return m, m.startCmd()
			}
		case stateRunning:
			switch msg.String() {
			case "ctrl+c", "q":
				return m, tea.Quit
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
		m.state = stateRunning
		return m, tea.Batch(m.statusCmd(), tickCmd())
	case statusMsg:
		if msg.err != nil {
			m.err = msg.err
			m.state = stateError
			return m, nil
		}
		m.status = msg.status
		return m, nil
	case tickMsg:
		if m.state == stateRunning {
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
		return docStyle.Render("Starting temporary Ubuntu SSH container...\n\nPlease wait.")
	case stateRunning:
		pass := strings.Repeat("•", max(4, len(m.status.Password)))
		if m.passInput.EchoMode == textinput.EchoNormal {
			pass = m.status.Password
		}
		return docStyle.Render(strings.Join([]string{
			titleStyle.Render("ephemeral-linux"),
			"",
			row("Status", "running"),
			row("Container", m.status.ContainerName),
			row("ID", m.status.ContainerID),
			row("Uptime", valueOrDash(m.status.Uptime)),
			row("SSH Port", valueOrDash(m.status.HostPort)),
			row("Connect", m.status.SSHCommand),
			row("User", m.status.Username),
			row("Pass", pass),
			"",
			mutedStyle.Render("c toggle password • q quit and remove container"),
		}, "\n"))
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
func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

var (
	docStyle   = lipgloss.NewStyle().Padding(1, 2)
	titleStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("212"))
	mutedStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
	errorStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("203"))
)
