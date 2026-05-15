package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/katistix/ephemeral-linux/internal/config"
	"github.com/katistix/ephemeral-linux/internal/docker"
)

type tickMsg time.Time

type tab int

const (
	tabMain tab = iota
	tabLogs
)

type refreshResultMsg struct {
	status docker.Status
	logs   string
	err    error
	at     time.Time
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
	NextTab     key.Binding
	PrevTab     key.Binding
	Up          key.Binding
	Down        key.Binding
	PageUp      key.Binding
	PageDown    key.Binding
	Top         key.Binding
	Bottom      key.Binding
}

func (k keyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.PrevTab, k.NextTab, k.Up, k.Down, k.StartStop, k.Quit}
}

func (k keyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{k.PrevTab, k.NextTab, k.Refresh, k.ToggleCreds},
		{k.Up, k.Down, k.PageUp, k.PageDown, k.Top, k.Bottom},
		{k.StartStop, k.Restart, k.Quit},
	}
}

type Model struct {
	cfg           config.Config
	configPath    string
	manager       *docker.Manager
	status        docker.Status
	logs          string
	showCreds     bool
	err           error
	keys          keyMap
	help          help.Model
	width         int
	height        int
	activeTab     tab
	logsViewport  viewport.Model
	logsContent   string
	statusNote    string
	lastRefreshed time.Time
}

func NewModel(cfg config.Config, configPath string, manager *docker.Manager) Model {
	h := help.New()
	h.ShowAll = false
	vp := viewport.New(0, 0)
	vp.YPosition = 0
	vp.Style = lipgloss.NewStyle()

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
			NextTab:     key.NewBinding(key.WithKeys("tab", "l", "right"), key.WithHelp("tab/→", "next tab")),
			PrevTab:     key.NewBinding(key.WithKeys("shift+tab", "h", "left"), key.WithHelp("shift+tab/←", "prev tab")),
			Up:          key.NewBinding(key.WithKeys("up", "k"), key.WithHelp("↑/k", "scroll up")),
			Down:        key.NewBinding(key.WithKeys("down", "j"), key.WithHelp("↓/j", "scroll down")),
			PageUp:      key.NewBinding(key.WithKeys("pgup", "b"), key.WithHelp("pgup", "page up")),
			PageDown:    key.NewBinding(key.WithKeys("pgdown", "f", "space"), key.WithHelp("pgdn", "page down")),
			Top:         key.NewBinding(key.WithKeys("g", "home"), key.WithHelp("g", "top")),
			Bottom:      key.NewBinding(key.WithKeys("G", "end"), key.WithHelp("G", "bottom")),
		},
		help:         h,
		width:        100,
		height:       30,
		logsViewport: vp,
		statusNote:   "Starting",
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
		m.syncViewport()
		return m, nil
	case tea.KeyMsg:
		switch {
		case key.Matches(msg, m.keys.Quit):
			return m, tea.Quit
		case key.Matches(msg, m.keys.Refresh):
			m.statusNote = "Refreshing"
			return m, m.refreshCmd()
		case key.Matches(msg, m.keys.ToggleCreds):
			m.showCreds = !m.showCreds
			return m, nil
		case key.Matches(msg, m.keys.StartStop):
			if m.status.Running {
				m.statusNote = "Stopping"
				return m, m.stopCmd()
			}
			m.statusNote = "Starting"
			return m, m.startCmd()
		case key.Matches(msg, m.keys.Restart):
			m.statusNote = "Restarting"
			return m, m.restartCmd()
		case key.Matches(msg, m.keys.NextTab):
			m.activeTab = (m.activeTab + 1) % 2
			return m, nil
		case key.Matches(msg, m.keys.PrevTab):
			m.activeTab = (m.activeTab + 1) % 2
			return m, nil
		}

		showTabs := max(40, m.width-docStyle.GetHorizontalFrameSize()) < 110
		if !showTabs || m.activeTab == tabLogs {
			switch {
			case key.Matches(msg, m.keys.Up):
				m.logsViewport.LineUp(1)
				m.clampViewport()
				return m, nil
			case key.Matches(msg, m.keys.Down):
				m.logsViewport.LineDown(1)
				m.clampViewport()
				return m, nil
			case key.Matches(msg, m.keys.PageUp):
				m.logsViewport.HalfViewUp()
				m.clampViewport()
				return m, nil
			case key.Matches(msg, m.keys.PageDown):
				m.logsViewport.HalfViewDown()
				m.clampViewport()
				return m, nil
			case key.Matches(msg, m.keys.Top):
				m.logsViewport.GotoTop()
				m.clampViewport()
				return m, nil
			case key.Matches(msg, m.keys.Bottom):
				m.logsViewport.GotoBottom()
				m.clampViewport()
				return m, nil
			}
		}
	case refreshResultMsg:
		m.status = msg.status
		m.logs = strings.TrimSpace(msg.logs)
		m.err = msg.err
		m.lastRefreshed = msg.at
		if msg.err != nil {
			m.statusNote = "Degraded"
		} else if m.status.Running {
			m.statusNote = "Ready"
		} else if m.status.ContainerExists {
			m.statusNote = "Stopped"
		} else {
			m.statusNote = "Not created"
		}
		m.syncViewport()
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
	innerHeight := max(12, m.height-docStyle.GetVerticalFrameSize())

	header := m.renderHeader(innerWidth)
	showTabs := innerWidth < 110
	footer := m.renderFooter(innerWidth, showTabs)
	contentHeight := max(8, innerHeight-lipgloss.Height(header)-lipgloss.Height(footer)-2)
	parts := []string{header}
	if showTabs {
		tabs := m.renderTabs(innerWidth)
		contentHeight = max(8, contentHeight-lipgloss.Height(tabs)-1)
		parts = append(parts, tabs)
	}
	var errorPanel string
	if m.err != nil {
		errorPanel = renderPanel(errorPanelStyle, innerWidth, wrapText(m.err.Error(), panelContentWidth(errorPanelStyle, innerWidth)))
		contentHeight = max(8, contentHeight-lipgloss.Height(errorPanel)-1)
	}
	body := m.renderContent(innerWidth, contentHeight, showTabs)

	parts = append(parts, body)
	if errorPanel != "" {
		parts = append(parts, errorPanel)
	}
	parts = append(parts, footer)

	view := docStyle.Render(strings.Join(parts, "\n"))
	return lipgloss.NewStyle().Height(m.height).MaxHeight(m.height).Render(view)
}

func (m Model) renderHeader(width int) string {
	metrics := []string{
		metricPill("state", m.status.StatusText),
		metricPill("port", fmt.Sprintf("%d", m.cfg.HostPort)),
		metricPill("uptime", valueOrDash(m.status.Uptime)),
		metricPill("cpu", valueOrDash(m.status.CPUPerc)),
		metricPill("mem", valueOrDash(m.status.MemUsage)),
		metricPill("mem%", valueOrDash(m.status.MemPerc)),
		metricPill("pids", valueOrDash(m.status.PIDs)),
	}

	titleLine := lipgloss.JoinHorizontal(lipgloss.Center,
		titleStyle.Render("ephemeral-linux"),
		lipgloss.NewStyle().MarginLeft(2).Render(statusBadge(m.status.Running)),
	)
	metaLine := mutedStyle.Render(m.statusNote)
	if !m.lastRefreshed.IsZero() {
		metaLine += mutedStyle.Render(" • " + m.lastRefreshed.Format("15:04:05"))
	}
	content := titleLine + "\n" + strings.Join(metrics, " ") + "\n" + metaLine
	return renderPanel(heroPanelStyle, width, content)
}

func (m Model) renderTabs(width int) string {
	tabs := []string{
		m.renderTab("Main", tabMain),
		m.renderTab("Logs", tabLogs),
	}
	return lipgloss.NewStyle().Width(width).Render(strings.Join(tabs, " "))
}

func (m Model) renderTab(label string, t tab) string {
	if m.activeTab == t {
		return activeTabStyle.Render(label)
	}
	return tabStyle.Render(label)
}

func (m Model) renderContent(width, height int, showTabs bool) string {
	if showTabs {
		if m.activeTab == tabLogs {
			return m.renderLogsTab(width, height)
		}
		return m.renderMainTab(width, height, true)
	}
	return m.renderMainTab(width, height, false)
}

func (m Model) renderMainTab(width, height int, compact bool) string {
	gap := 1
	leftWidth, rightWidth := layoutWidths(width, compact, gap)

	if compact {
		return lipgloss.JoinVertical(lipgloss.Left,
			m.renderPrimaryPanel(leftWidth),
			m.renderAccessPanel(leftWidth),
		)
	}

	leftTopHeight := max(8, (height-gap)/2)
	leftBottomHeight := max(8, height-gap-leftTopHeight)
	left := lipgloss.JoinVertical(lipgloss.Left,
		m.renderPrimaryPanelWithHeight(leftWidth, leftTopHeight),
		spacerVertical(gap),
		m.renderAccessPanelWithHeight(leftWidth, leftBottomHeight),
	)
	right := m.renderLogsTab(rightWidth, leftTopHeight+gap+leftBottomHeight)

	body := lipgloss.JoinHorizontal(lipgloss.Top, left, spacer(gap), right)
	return lipgloss.NewStyle().Width(width).Render(body)
}

func (m Model) renderLogsTab(width, height int) string {
	contentWidth := panelContentWidth(panelStyle, width)
	panelHeight := max(8, height)
	viewportHeight := max(3, panelHeight-panelStyle.GetVerticalFrameSize()-3)
	m.logsViewport.Width = contentWidth
	m.logsViewport.Height = viewportHeight

	header := sectionTitleStyle.Render("Logs")
	body := m.logsViewport.View()
	return renderPanelWithHeight(panelStyle, width, panelHeight, header+"\n\n"+body)
}

func (m Model) renderPrimaryPanel(width int) string {
	return m.renderPrimaryPanelWithHeight(width, 0)
}

func (m Model) renderPrimaryPanelWithHeight(width, height int) string {
	contentWidth := panelContentWidth(panelStyle, width)
	rows := []string{
		infoRow("Container", valueOrDash(m.cfg.ContainerName), contentWidth),
		infoRow("ID", valueOrDash(m.status.ContainerID), contentWidth),
		infoRow("Image", valueOrDash(m.status.Image), contentWidth),
		infoRow("Started", formatTimestamp(m.status.StartedAt), contentWidth),
		infoRow("IP", valueOrDash(m.status.IPAddress), contentWidth),
	}
	content := sectionTitleStyle.Render("Instance") + "\n\n" + strings.Join(rows, "\n")
	if height > 0 {
		return renderPanelWithHeight(panelStyle, width, height, content)
	}
	return renderPanel(panelStyle, width, content)
}

func (m Model) renderAccessPanel(width int) string {
	return m.renderAccessPanelWithHeight(width, 0)
}

func (m Model) renderAccessPanelWithHeight(width, height int) string {
	contentWidth := panelContentWidth(panelStyle, width)
	rows := []string{
		infoRow("SSH", valueOrDash(m.status.SSHCommand), contentWidth),
		infoRow("User", m.cfg.Username, contentWidth),
		infoRow("Pass", credentialValue(m.showCreds, m.cfg.Password), contentWidth),
		infoRow("Config", valueOrDash(m.configPath), contentWidth),
		infoRow("Workspace", valueOrDash(m.cfg.WorkspaceDir), contentWidth),
	}
	content := sectionTitleStyle.Render("Access") + "\n\n" + strings.Join(rows, "\n")
	if height > 0 {
		return renderPanelWithHeight(panelStyle, width, height, content)
	}
	return renderPanel(panelStyle, width, content)
}

func (m Model) refreshCmd() tea.Cmd {
	return func() tea.Msg {
		status, err := m.manager.Status()
		if err != nil {
			return refreshResultMsg{err: err, at: time.Now()}
		}
		logs, logErr := m.manager.Logs(200)
		if logErr != nil && status.ContainerExists {
			err = logErr
		}
		return refreshResultMsg{status: status, logs: logs, err: err, at: time.Now()}
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

func (m *Model) syncViewport() {
	width := max(20, m.width-docStyle.GetHorizontalFrameSize())
	showTabs := width < 110
	contentWidth := max(10, panelContentWidth(panelStyle, width)-1)
	if !showTabs {
		_, rightWidth := layoutWidths(width, false, 1)
		contentWidth = max(10, panelContentWidth(panelStyle, rightWidth)-1)
	}
	content := wrapLines(valueOrDefault(m.logs, "No logs yet."), contentWidth)
	content = fadedStyle.Render("[top]") + "\n" + content + "\n" + fadedStyle.Render("[eof]")
	atBottom := m.logsViewport.AtBottom()
	m.logsContent = content
	m.logsViewport.SetContent(content)
	if atBottom {
		m.logsViewport.GotoBottom()
	}
	m.clampViewport()
}

func (m *Model) clampViewport() {
	maxOffset := max(0, len(strings.Split(m.logsContent, "\n"))-m.logsViewport.Height)
	if m.logsViewport.YOffset > maxOffset {
		m.logsViewport.SetYOffset(maxOffset)
	}
	if m.logsViewport.YOffset < 0 {
		m.logsViewport.SetYOffset(0)
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

func metricPill(label, value string) string {
	return pillStyle.Render(strings.ToUpper(label) + ": " + value)
}

func statusBadge(running bool) string {
	if running {
		return runningStyle.Render(" RUNNING ")
	}
	return stoppedStyle.Render(" STOPPED ")
}

func formatTimestamp(v string) string {
	if strings.TrimSpace(v) == "" || v == "0001-01-01T00:00:00Z" {
		return "-"
	}
	if t, err := time.Parse(time.RFC3339Nano, v); err == nil {
		return t.Local().Format("2006-01-02 15:04:05")
	}
	return v
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

func renderPanelWithHeight(style lipgloss.Style, width, height int, content string) string {
	contentWidth := panelContentWidth(style, width)
	contentHeight := max(1, height-style.GetVerticalFrameSize())
	return style.Width(contentWidth).Height(contentHeight).Render(content)
}

func panelContentWidth(style lipgloss.Style, width int) int {
	return max(1, width-style.GetHorizontalFrameSize())
}

func layoutWidths(totalWidth int, compact bool, gap int) (int, int) {
	if compact {
		return totalWidth, totalWidth
	}
	left := int(float64(totalWidth-gap) * 0.48)
	left = max(42, left)
	right := totalWidth - gap - left
	right = max(42, right)
	left = totalWidth - gap - right
	return left, right
}

func spacer(width int) string {
	return strings.Repeat(" ", width)
}

func spacerVertical(height int) string {
	return strings.Repeat("\n", max(0, height-1))
}

func valueOrDash(v string) string {
	if strings.TrimSpace(v) == "" {
		return "-"
	}
	return v
}

func valueOrDefault(v, fallback string) string {
	if strings.TrimSpace(v) == "" {
		return fallback
	}
	return v
}

func credentialValue(show bool, value string) string {
	if !show {
		return "••••••••"
	}
	return value
}

func (m Model) renderFooter(width int, showTabs bool) string {
	if showTabs {
		return m.help.View(m.keys)
	}
	bindings := []key.Binding{m.keys.Up, m.keys.Down, m.keys.PageUp, m.keys.PageDown, m.keys.ToggleCreds, m.keys.StartStop, m.keys.Quit}
	return m.help.View(helpKeyMap{bindings: bindings})
}

type helpKeyMap struct {
	bindings []key.Binding
}

func (h helpKeyMap) ShortHelp() []key.Binding {
	return h.bindings
}

func (h helpKeyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{h.bindings}
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

var (
	docStyle          = lipgloss.NewStyle().Padding(0, 2)
	titleStyle        = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("212"))
	sectionTitleStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("230"))
	panelStyle        = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("63")).Padding(1)
	heroPanelStyle    = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("99")).Padding(1)
	errorPanelStyle   = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("203")).Foreground(lipgloss.Color("203")).Padding(1)
	mutedStyle        = lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
	fadedStyle        = lipgloss.NewStyle().Foreground(lipgloss.Color("238"))
	pillStyle         = lipgloss.NewStyle().Foreground(lipgloss.Color("230")).Background(lipgloss.Color("238")).Padding(0, 1)
	tabStyle          = lipgloss.NewStyle().Foreground(lipgloss.Color("247")).Background(lipgloss.Color("236")).Padding(0, 2)
	activeTabStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("230")).Background(lipgloss.Color("63")).Bold(true).Padding(0, 2)
	codeStyle         = lipgloss.NewStyle().Foreground(lipgloss.Color("193"))
	runningStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("42")).Background(lipgloss.Color("22")).Bold(true)
	stoppedStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("203")).Background(lipgloss.Color("52")).Bold(true)
)
