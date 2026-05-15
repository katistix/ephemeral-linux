package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/katistix/ephemeral-linux/internal/config"
	"github.com/katistix/ephemeral-linux/internal/docker"
)

type tickMsg time.Time

type refreshResultMsg struct {
	status docker.Status
	logs   string
	err    error
}

type actionResultMsg struct {
	err error
}

type keyMap struct {
	Quit        key.Binding
	Refresh     key.Binding
	ToggleCreds key.Binding
	StartStop   key.Binding
	Restart     key.Binding
}

func (k keyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Refresh, k.ToggleCreds, k.StartStop, k.Restart, k.Quit}
}

func (k keyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{{k.Refresh, k.ToggleCreds, k.StartStop, k.Restart, k.Quit}}
}

type Model struct {
	cfg        config.Config
	configPath string
	manager    *docker.Manager
	status     docker.Status
	logs       string
	showCreds  bool
	err        error
	keys       keyMap
	help       help.Model
	width      int
	height     int
}

func NewModel(cfg config.Config, configPath string, manager *docker.Manager) Model {
	h := help.New()
	h.ShowAll = false
	return Model{
		cfg:        cfg,
		configPath: configPath,
		manager:    manager,
		keys: keyMap{
			Quit:        key.NewBinding(key.WithKeys("q", "ctrl+c"), key.WithHelp("q", "quit")),
			Refresh:     key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "refresh")),
			ToggleCreds: key.NewBinding(key.WithKeys("c"), key.WithHelp("c", "toggle creds")),
			StartStop:   key.NewBinding(key.WithKeys("s"), key.WithHelp("s", "start/stop")),
			Restart:     key.NewBinding(key.WithKeys("R"), key.WithHelp("R", "restart")),
		},
		help:   h,
		width:  100,
		height: 30,
	}
}

func (m Model) Init() tea.Cmd {
	return tea.Batch(m.refreshCmd(), tickCmd())
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.help.Width = max(20, msg.Width-4)
		return m, nil
	case tea.KeyMsg:
		switch {
		case key.Matches(msg, m.keys.Quit):
			return m, tea.Quit
		case key.Matches(msg, m.keys.Refresh):
			return m, m.refreshCmd()
		case key.Matches(msg, m.keys.ToggleCreds):
			m.showCreds = !m.showCreds
			return m, nil
		case key.Matches(msg, m.keys.StartStop):
			if m.status.Running {
				return m, m.stopCmd()
			}
			return m, m.startCmd()
		case key.Matches(msg, m.keys.Restart):
			return m, m.restartCmd()
		}
	case refreshResultMsg:
		m.status = msg.status
		m.logs = strings.TrimSpace(msg.logs)
		m.err = msg.err
		return m, nil
	case actionResultMsg:
		m.err = msg.err
		return m, m.refreshCmd()
	case tickMsg:
		return m, tea.Batch(m.refreshCmd(), tickCmd())
	}
	return m, nil
}

func (m Model) View() string {
	innerWidth := max(40, m.width-docStyle.GetHorizontalFrameSize())
	compact := innerWidth < 120
	gap := 1

	title := titleStyle.Render("ephemeral-linux")
	header := lipgloss.JoinHorizontal(lipgloss.Center,
		title,
		lipgloss.NewStyle().MarginLeft(2).Foreground(lipgloss.Color("241")).Render(statusBadge(m.status.Running)),
	)

	leftWidth, rightWidth := layoutWidths(innerWidth, compact, gap)

	summary := m.renderSummary(leftWidth)
	creds := m.renderCredentials(leftWidth)
	leftColumn := lipgloss.JoinVertical(lipgloss.Left, summary, creds)

	logs := m.renderLogs(rightWidth)

	var body string
	if compact {
		body = lipgloss.JoinVertical(lipgloss.Left, leftColumn, logs)
	} else {
		body = lipgloss.JoinHorizontal(lipgloss.Top, leftColumn, spacer(gap), logs)
	}

	footer := m.help.View(m.keys)
	parts := []string{header, body}
	if m.err != nil {
		parts = append(parts, renderPanel(errorPanelStyle, innerWidth, wrapText(m.err.Error(), innerWidth-errorPanelStyle.GetHorizontalFrameSize())))
	}
	parts = append(parts, footer)

	return docStyle.Render(strings.Join(parts, "\n\n"))
}

func (m Model) renderSummary(width int) string {
	contentWidth := panelContentWidth(panelStyle, width)
	rows := []string{
		infoRow("Container", valueOrDash(m.cfg.ContainerName), contentWidth),
		infoRow("Container ID", valueOrDash(m.status.ContainerID), contentWidth),
		infoRow("Image", valueOrDash(m.cfg.ImageName), contentWidth),
		infoRow("SSH", valueOrDash(m.status.SSHCommand), contentWidth),
		infoRow("Host Port", fmt.Sprintf("%d", m.cfg.HostPort), contentWidth),
		infoRow("Container IP", valueOrDash(m.status.IPAddress), contentWidth),
		infoRow("Uptime", valueOrDash(m.status.Uptime), contentWidth),
		infoRow("Workspace", valueOrDash(m.cfg.WorkspaceDir), contentWidth),
		infoRow("Config", valueOrDash(m.configPath), contentWidth),
	}

	content := strings.Join([]string{
		sectionTitleStyle.Render("Instance"),
		"",
		infoRow("Status", statusBadge(m.status.Running), contentWidth),
		strings.Join(rows, "\n"),
	}, "\n")

	return renderPanel(panelStyle, width, content)
}

func (m Model) renderCredentials(width int) string {
	contentWidth := panelContentWidth(panelStyle, width)
	creds := "hidden"
	if m.showCreds {
		creds = fmt.Sprintf("user=%s  pass=%s", m.cfg.Username, m.cfg.Password)
	}

	content := strings.Join([]string{
		sectionTitleStyle.Render("Credentials"),
		"",
		wrapText(creds, contentWidth),
		mutedStyle.Render("Press c to show or hide SSH credentials."),
	}, "\n")

	return renderPanel(panelStyle, width, content)
}

func (m Model) renderLogs(width int) string {
	contentWidth := panelContentWidth(panelStyle, width)
	logs := m.logs
	if logs == "" {
		logs = "No logs yet."
	}
	wrapped := wrapLines(logs, contentWidth)
	return renderPanel(panelStyle, width, sectionTitleStyle.Render("Recent logs")+"\n\n"+wrapped)
}

func (m Model) refreshCmd() tea.Cmd {
	return func() tea.Msg {
		status, err := m.manager.Status()
		if err != nil {
			return refreshResultMsg{err: err}
		}
		logs, logErr := m.manager.Logs(12)
		if logErr != nil && status.ContainerExists {
			err = logErr
		}
		return refreshResultMsg{status: status, logs: logs, err: err}
	}
}

func (m Model) startCmd() tea.Cmd {
	return func() tea.Msg {
		return actionResultMsg{err: m.manager.EnsureContainerRunning()}
	}
}

func (m Model) stopCmd() tea.Cmd {
	return func() tea.Msg {
		return actionResultMsg{err: m.manager.StopContainer()}
	}
}

func (m Model) restartCmd() tea.Cmd {
	return func() tea.Msg {
		return actionResultMsg{err: m.manager.RestartContainer()}
	}
}

func tickCmd() tea.Cmd {
	return tea.Tick(2*time.Second, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

func infoRow(label, value string, width int) string {
	labelWidth := 12
	available := max(10, width-labelWidth-2)
	wrapped := wrapText(value, available)
	lines := strings.Split(wrapped, "\n")
	for i := range lines {
		if i == 0 {
			lines[i] = mutedStyle.Width(labelWidth).Render(label) + "  " + lines[i]
			continue
		}
		lines[i] = strings.Repeat(" ", labelWidth+2) + lines[i]
	}
	return strings.Join(lines, "\n")
}

func statusBadge(running bool) string {
	if running {
		return runningStyle.Render(" RUNNING ")
	}
	return stoppedStyle.Render(" STOPPED ")
}

func wrapLines(text string, width int) string {
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		lines[i] = wrapText(line, width)
	}
	return strings.Join(lines, "\n")
}

func wrapText(text string, width int) string {
	if strings.TrimSpace(text) == "" {
		return "-"
	}
	if width <= 1 {
		return text
	}

	var out []string
	for _, rawLine := range strings.Split(text, "\n") {
		if strings.TrimSpace(rawLine) == "" {
			out = append(out, "")
			continue
		}

		words := strings.Fields(rawLine)
		if len(words) == 0 {
			out = append(out, rawLine)
			continue
		}

		line := ""
		for _, word := range words {
			for visualWidth(word) > width {
				cut := sliceByWidth(word, width)
				if line != "" {
					out = append(out, line)
					line = ""
				}
				out = append(out, cut)
				word = strings.TrimPrefix(word, cut)
			}

			candidate := word
			if line != "" {
				candidate = line + " " + word
			}
			if visualWidth(candidate) <= width {
				line = candidate
			} else {
				out = append(out, line)
				line = word
			}
		}
		if line != "" {
			out = append(out, line)
		}
	}

	return strings.Join(out, "\n")
}

func visualWidth(s string) int {
	return lipgloss.Width(s)
}

func sliceByWidth(s string, width int) string {
	var b strings.Builder
	for _, r := range s {
		if lipgloss.Width(b.String()+string(r)) > width {
			break
		}
		b.WriteRune(r)
	}
	if b.Len() == 0 {
		return s
	}
	return b.String()
}

func renderPanel(style lipgloss.Style, width int, content string) string {
	return style.Width(panelContentWidth(style, width)).Render(content)
}

func panelContentWidth(style lipgloss.Style, width int) int {
	return max(1, width-style.GetHorizontalFrameSize())
}

func layoutWidths(totalWidth int, compact bool, gap int) (int, int) {
	if compact {
		return totalWidth, totalWidth
	}
	left := int(float64(totalWidth-gap) * 0.44)
	left = max(42, left)
	right := totalWidth - gap - left
	right = max(42, right)
	left = totalWidth - gap - right
	return left, right
}

func spacer(width int) string {
	return strings.Repeat(" ", width)
}

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
	docStyle          = lipgloss.NewStyle().Padding(1, 2)
	titleStyle        = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("212"))
	sectionTitleStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("230"))
	panelStyle        = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("63")).Padding(1)
	errorPanelStyle   = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("203")).Foreground(lipgloss.Color("203")).Padding(1)
	mutedStyle        = lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
	runningStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("42")).Background(lipgloss.Color("22")).Bold(true)
	stoppedStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("203")).Background(lipgloss.Color("52")).Bold(true)
)
