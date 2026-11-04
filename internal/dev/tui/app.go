package tui

import (
	"fmt"
	"strings"
	"time"

	"ctrldown/cli/internal/dev/graph"
	"ctrldown/cli/internal/dev/supervisor"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// ActiveTab defines which major screen is being rendered.
type ActiveTab int

const (
	TabFleet ActiveTab = iota
	TabGraph
	TabLogs
)

type tickMsg time.Time
type logStreamMsg supervisor.LogLine

// Model represents the interactive Bubble Tea state for ctrldown dev.
type Model struct {
	supervisor    *supervisor.Supervisor
	graph         *graph.WorkspaceGraph
	projectTitle  string
	activeTab     ActiveTab
	selectedCol   int // 0: Infra, 1: Backend, 2: Frontend
	selectedRow   int
	width         int
	height        int
	logViewport   viewport.Model
	fullscreenLog viewport.Model
	recentLogs    []supervisor.LogLine
	quitting      bool
	logChan       <-chan supervisor.LogLine
}

// NewModel instantiates the dev TUI model.
func NewModel(sup *supervisor.Supervisor, g *graph.WorkspaceGraph, projectTitle string) Model {
	vp := viewport.New(100, 8)
	fvp := viewport.New(100, 24)

	return Model{
		supervisor:    sup,
		graph:         g,
		projectTitle:  projectTitle,
		activeTab:     TabFleet,
		selectedCol:   1, // Default focus on Backend
		selectedRow:   0,
		logViewport:   vp,
		fullscreenLog: fvp,
		recentLogs:    make([]supervisor.LogLine, 0, 100),
		logChan:       sup.UnifiedLogChannel(),
	}
}

// Init sets up recurring tickers and log streaming subscriptions.
func (m Model) Init() tea.Cmd {
	return tea.Batch(
		tea.EnterAltScreen,
		tickCmd(),
		waitForLogMsg(m.logChan),
	)
}

func tickCmd() tea.Cmd {
	return tea.Tick(500*time.Millisecond, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

func waitForLogMsg(ch <-chan supervisor.LogLine) tea.Cmd {
	return func() tea.Msg {
		if ch == nil {
			return nil
		}
		line, ok := <-ch
		if !ok {
			return nil
		}
		return logStreamMsg(line)
	}
}

// Update handles keyboard navigation, log updates, and resizing.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.logViewport.Width = msg.Width - 4
		m.logViewport.Height = 7
		m.fullscreenLog.Width = msg.Width - 4
		m.fullscreenLog.Height = msg.Height - 8

	case tickMsg:
		cmds = append(cmds, tickCmd())

	case logStreamMsg:
		m.recentLogs = append(m.recentLogs, supervisor.LogLine(msg))
		if len(m.recentLogs) > 100 {
			m.recentLogs = m.recentLogs[1:]
		}

		// Update viewports
		var b strings.Builder
		for _, l := range m.recentLogs {
			tag := lipgloss.NewStyle().Foreground(lipgloss.Color("#A3E635")).Render("[" + l.Source + "]")
			b.WriteString(fmt.Sprintf("%s %s\n", tag, l.Text))
		}
		m.logViewport.SetContent(b.String())
		m.logViewport.GotoBottom()
		cmds = append(cmds, waitForLogMsg(m.logChan))

	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c":
			m.quitting = true
			m.supervisor.StopAll()
			return m, tea.Quit

		case "tab":
			m.activeTab = (m.activeTab + 1) % 3

		case "shift+tab":
			if m.activeTab == 0 {
				m.activeTab = TabLogs
			} else {
				m.activeTab--
			}

		case "1":
			m.activeTab = TabFleet
			m.selectedCol = 0
		case "2":
			m.activeTab = TabFleet
			m.selectedCol = 1
		case "3":
			m.activeTab = TabFleet
			m.selectedCol = 2
		case "g", "p":
			m.activeTab = TabGraph
		case "f":
			m.activeTab = TabLogs

		case "left", "h":
			if m.activeTab == TabFleet && m.selectedCol > 0 {
				m.selectedCol--
				m.selectedRow = 0
			}

		case "right", "l":
			if m.activeTab == TabFleet && m.selectedCol < 2 {
				m.selectedCol++
				m.selectedRow = 0
			}

		case "up", "k":
			if m.activeTab == TabFleet && m.selectedRow > 0 {
				m.selectedRow--
			}

		case "down", "j":
			if m.activeTab == TabFleet {
				colSvcs := m.getColServices(m.selectedCol)
				if m.selectedRow < len(colSvcs)-1 {
					m.selectedRow++
				}
			}

		case "r":
			// Restart selected service
			if m.activeTab == TabFleet {
				colSvcs := m.getColServices(m.selectedCol)
				if len(colSvcs) > 0 && m.selectedRow < len(colSvcs) {
					target := colSvcs[m.selectedRow]
					go func(name string) {
						_ = m.supervisor.RestartService(name)
					}(target.Config.Name)
				}
			}
		}
	}

	return m, tea.Batch(cmds...)
}

func (m Model) getColServices(col int) []*supervisor.Service {
	switch col {
	case 0:
		return m.supervisor.ByCategory(supervisor.CategoryInfra)
	case 1:
		return m.supervisor.ByCategory(supervisor.CategoryBackend)
	case 2:
		return m.supervisor.ByCategory(supervisor.CategoryFrontend)
	default:
		return nil
	}
}

// View delegates rendering based on active tab.
func (m Model) View() string {
	if m.quitting {
		return lipgloss.NewStyle().Foreground(lipgloss.Color("#94A3B8")).Render("\n  Shutting down services gracefully...\n")
	}

	header := m.renderHeader()
	var body string

	switch m.activeTab {
	case TabFleet:
		body = m.renderFleetTab()
	case TabGraph:
		body = m.renderGraphTab()
	case TabLogs:
		body = m.renderLogsTab()
	}

	footer := m.renderFooter()

	return lipgloss.JoinVertical(lipgloss.Left, header, body, footer)
}

// renderHeader displays the top branding, tabs, and status badges.
func (m Model) renderHeader() string {
	titleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("#0F172A")).
		Background(lipgloss.Color("#D2F862")).
		Padding(0, 1).
		MarginRight(1)

	projStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("#F8FAFC")).
		Background(lipgloss.Color("#1E293B")).
		Padding(0, 1)

	tabActiveStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("#D2F862")).
		Border(lipgloss.NormalBorder(), false, false, true, false).
		BorderForeground(lipgloss.Color("#D2F862")).
		Padding(0, 1)

	tabInactiveStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#64748B")).
		Padding(0, 1)

	tab1 := tabInactiveStyle.Render("1: Fleet")
	if m.activeTab == TabFleet {
		tab1 = tabActiveStyle.Render("1: Fleet")
	}

	tab2 := tabInactiveStyle.Render("2: Packages")
	if m.activeTab == TabGraph {
		tab2 = tabActiveStyle.Render("2: Packages")
	}

	tab3 := tabInactiveStyle.Render("3: Logs")
	if m.activeTab == TabLogs {
		tab3 = tabActiveStyle.Render("3: Logs")
	}

	tabs := lipgloss.JoinHorizontal(lipgloss.Center, tab1, tab2, tab3)

	topBar := lipgloss.JoinHorizontal(
		lipgloss.Center,
		titleStyle.Render("ctrldown dev"),
		projStyle.Render(m.projectTitle),
		"   ",
		tabs,
	)

	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("#334155")).
		Padding(0, 1).
		Width(m.width - 2).
		Render(topBar)
}

// renderFleetTab builds the 3-column bento-grid layout with live activity card.
func (m Model) renderFleetTab() string {
	colWidth := (m.width - 10) / 3
	if colWidth < 28 {
		colWidth = 28
	}

	// 1. Column 1: Infrastructure
	c1 := m.renderServiceColumn(
		"INFRASTRUCTURE",
		m.supervisor.ByCategory(supervisor.CategoryInfra),
		colWidth,
		m.selectedCol == 0,
	)

	// 2. Column 2: Backend & Runtimes
	c2 := m.renderServiceColumn(
		"BACKEND",
		m.supervisor.ByCategory(supervisor.CategoryBackend),
		colWidth,
		m.selectedCol == 1,
	)

	// 3. Column 3: Frontend Applications
	c3 := m.renderServiceColumn(
		"FRONTEND",
		m.supervisor.ByCategory(supervisor.CategoryFrontend),
		colWidth,
		m.selectedCol == 2,
	)

	// Join the 3 columns horizontally with gap-4 spacing
	columnsView := lipgloss.JoinHorizontal(lipgloss.Top, c1, "  ", c2, "  ", c3)

	// Bottom Activity Pane
	logHeader := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("#94A3B8")).
		Render("  ACTIVITY & LOGS ───────────────────────────────────────────────────────────── [f: Fullscreen]")

	logBox := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("#334155")).
		Width(m.width - 4).
		Height(8).
		Render(m.logViewport.View())

	return lipgloss.JoinVertical(lipgloss.Left, columnsView, logHeader, logBox)
}

// renderServiceColumn renders an individual column card with rounded borders.
func (m Model) renderServiceColumn(title string, svcs []*supervisor.Service, width int, isColSelected bool) string {
	borderCol := lipgloss.Color("#334155")
	titleCol := lipgloss.Color("#94A3B8")
	if isColSelected {
		borderCol = lipgloss.Color("#D2F862")
		titleCol = lipgloss.Color("#D2F862")
	}

	var b strings.Builder
	b.WriteString(lipgloss.NewStyle().Bold(true).Foreground(titleCol).Render(title))
	b.WriteString("\n\n")

	if len(svcs) == 0 {
		b.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("#475569")).Italic(true).Render("No active services in fleet"))
		b.WriteString("\n")
	}

	for idx, s := range svcs {
		isSelected := isColSelected && m.selectedRow == idx
		b.WriteString(m.renderServiceItem(s, isSelected, width-4))
		b.WriteString("\n")
	}

	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(borderCol).
		Padding(1, 1).
		Width(width).
		Height(14).
		Render(b.String())
}

// renderServiceItem formats a single service status tile.
func (m Model) renderServiceItem(s *supervisor.Service, isSelected bool, maxW int) string {
	status, pid, uptime, _ := s.Snapshot()

	var dot string
	switch status {
	case supervisor.StatusRunning:
		dot = lipgloss.NewStyle().Foreground(lipgloss.Color("#10B981")).Render("●")
	case supervisor.StatusProbing, supervisor.StatusStarting:
		dot = lipgloss.NewStyle().Foreground(lipgloss.Color("#FCD34D")).Render("◐")
	case supervisor.StatusCrashed:
		dot = lipgloss.NewStyle().Foreground(lipgloss.Color("#F43F5E")).Render("✖")
	default:
		dot = lipgloss.NewStyle().Foreground(lipgloss.Color("#64748B")).Render("○")
	}

	nameStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#F8FAFC"))
	if isSelected {
		nameStyle = nameStyle.Foreground(lipgloss.Color("#D2F862")).Underline(true)
	}

	header := fmt.Sprintf("%s %s", dot, nameStyle.Render(s.Config.Name))

	details := ""
	if s.Config.Port > 0 {
		details = fmt.Sprintf("Port: %d", s.Config.Port)
	}
	if pid > 0 {
		details += fmt.Sprintf(" | PID: %d", pid)
	}
	if uptime > 0 {
		details += fmt.Sprintf(" (%s)", uptime.Round(time.Second))
	}

	subStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#64748B")).MarginLeft(2)

	return header + "\n" + subStyle.Render(details)
}

// renderGraphTab builds the interactive Monorepo Packages & AST linkage matrix.
func (m Model) renderGraphTab() string {
	var b strings.Builder

	headStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("#0F172A")).
		Background(lipgloss.Color("#38BDF8")).
		Padding(0, 1).
		MarginBottom(1)

	b.WriteString(headStyle.Render("MONOREPO PACKAGES & AST LINKAGES MAP"))
	b.WriteString("\n\n")

	if m.graph == nil || len(m.graph.Nodes) == 0 {
		b.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("#64748B")).Render("No monorepo packages or microfeatures found in workspace."))
		return b.String()
	}

	headerRow := fmt.Sprintf("  %-32s %-32s %-24s\n", "PACKAGE / SOURCE", "BOUND DOMAIN (CONSUMER)", "LINKAGE MECHANISM")
	b.WriteString(lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#94A3B8")).Render(headerRow))
	b.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("#334155")).Render("  " + strings.Repeat("─", m.width-8) + "\n"))

	for _, n := range m.graph.Nodes {
		pkgStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#D2F862"))
		if len(n.Consumers) == 0 {
			row := fmt.Sprintf("  %-32s %-32s %-24s\n", pkgStyle.Render("📦 "+n.Name), lipgloss.NewStyle().Foreground(lipgloss.Color("#64748B")).Render("(Unlinked Library)"), n.Kind)
			b.WriteString(row)
		} else {
			for i, c := range n.Consumers {
				nameCol := ""
				if i == 0 {
					nameCol = pkgStyle.Render("📦 " + n.Name)
				}
				branch := "├── "
				if i == len(n.Consumers)-1 {
					branch = "└── "
				}
				consumerStr := branch + c.DomainName
				row := fmt.Sprintf("  %-32s %-32s %-24s\n", nameCol, lipgloss.NewStyle().Foreground(lipgloss.Color("#F8FAFC")).Render(consumerStr), lipgloss.NewStyle().Foreground(lipgloss.Color("#38BDF8")).Render(c.LinkType))
				b.WriteString(row)
			}
		}
		b.WriteString("\n")
	}

	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("#334155")).
		Padding(1, 2).
		Width(m.width - 4).
		Height(m.height - 8).
		Render(b.String())
}

// renderLogsTab renders the full-screen terminal log stream.
func (m Model) renderLogsTab() string {
	headStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("#0F172A")).
		Background(lipgloss.Color("#D2F862")).
		Padding(0, 1).
		MarginBottom(1)

	title := headStyle.Render("FULLSCREEN UNIFIED LOG STREAM")

	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("#D2F862")).
		Padding(1, 1).
		Width(m.width - 4).
		Height(m.height - 8).
		Render(title + "\n\n" + m.logViewport.View())
}

// renderFooter renders the hotkey legend at the bottom of the screen.
func (m Model) renderFooter() string {
	keyStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#D2F862"))
	descStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#64748B"))

	keys := []string{
		keyStyle.Render("[Tab]") + " " + descStyle.Render("Switch View"),
		keyStyle.Render("[←/→]") + " " + descStyle.Render("Select Column"),
		keyStyle.Render("[↑/↓]") + " " + descStyle.Render("Select Service"),
		keyStyle.Render("[r]") + " " + descStyle.Render("Restart Service"),
		keyStyle.Render("[f]") + " " + descStyle.Render("Fullscreen Logs"),
		keyStyle.Render("[g]") + " " + descStyle.Render("Packages Graph"),
		keyStyle.Render("[q]") + " " + descStyle.Render("Quit / Shutdown"),
	}

	return lipgloss.NewStyle().
		MarginTop(0).
		Padding(0, 1).
		Width(m.width - 2).
		Render(strings.Join(keys, "   "))
}

// StartApp starts the Bubble Tea TUI event loop.
func StartApp(sup *supervisor.Supervisor, g *graph.WorkspaceGraph, projectTitle string) error {
	p := tea.NewProgram(NewModel(sup, g, projectTitle), tea.WithAltScreen())
	_, err := p.Run()
	return err
}
