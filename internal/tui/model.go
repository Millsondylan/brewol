// Package tui provides the terminal user interface using Bubble Tea.
package tui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/ai/brewol/internal/engine"
	"github.com/ai/brewol/internal/ollama"
	"github.com/ai/brewol/internal/tools"
)

// KeyMap defines the keybindings
type KeyMap struct {
	Escape       key.Binding
	CommandMode  key.Binding
	ModelPicker  key.Binding
	ToggleDiff   key.Binding
	OpenLogs     key.Binding
	Help         key.Binding
	ScrollUp     key.Binding
	ScrollDown   key.Binding
	PageUp       key.Binding
	PageDown     key.Binding
}

// DefaultKeyMap returns the default keybindings
func DefaultKeyMap() KeyMap {
	return KeyMap{
		Escape: key.NewBinding(
			key.WithKeys("esc"),
			key.WithHelp("esc", "cancel/exit"),
		),
		CommandMode: key.NewBinding(
			key.WithKeys("ctrl+k"),
			key.WithHelp("ctrl+k", "command palette"),
		),
		ModelPicker: key.NewBinding(
			key.WithKeys("ctrl+m"),
			key.WithHelp("ctrl+m", "model picker"),
		),
		ToggleDiff: key.NewBinding(
			key.WithKeys("ctrl+d"),
			key.WithHelp("ctrl+d", "toggle diff"),
		),
		OpenLogs: key.NewBinding(
			key.WithKeys("ctrl+l"),
			key.WithHelp("ctrl+l", "open logs"),
		),
		Help: key.NewBinding(
			key.WithKeys("?"),
			key.WithHelp("?", "help"),
		),
		ScrollUp: key.NewBinding(
			key.WithKeys("up", "k"),
			key.WithHelp("↑/k", "scroll up"),
		),
		ScrollDown: key.NewBinding(
			key.WithKeys("down", "j"),
			key.WithHelp("↓/j", "scroll down"),
		),
		PageUp: key.NewBinding(
			key.WithKeys("pgup", "ctrl+u"),
			key.WithHelp("pgup", "page up"),
		),
		PageDown: key.NewBinding(
			key.WithKeys("pgdown", "ctrl+d"),
			key.WithHelp("pgdn", "page down"),
		),
	}
}

// ShortHelp returns keybindings to be shown in the mini help view
func (k KeyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Escape, k.CommandMode, k.Help}
}

// FullHelp returns keybindings for the expanded help view
func (k KeyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{k.Escape, k.CommandMode, k.ModelPicker},
		{k.ToggleDiff, k.OpenLogs, k.Help},
		{k.ScrollUp, k.ScrollDown, k.PageUp, k.PageDown},
	}
}

// Model is the main TUI model
type Model struct {
	engine        *engine.Engine
	keyMap        KeyMap
	help          help.Model
	spinner       spinner.Model
	streamView    viewport.Model
	toolLogView   viewport.Model
	backlogView   viewport.Model
	commandInput  textinput.Model
	width         int
	height        int
	streamContent strings.Builder
	toolLogs      []ToolLogEntry
	suggestions   []engine.Suggestion
	showHelp      bool
	showDiff      bool
	showCommand   bool
	showModels    bool
	models        []ollama.ModelInfo
	selectedModel int
	tokensPerSec  float64
	lastExitCode  int
	lastEscTime   time.Time
	quitting      bool
	err           error
}

// ToolLogEntry represents a tool execution log entry
type ToolLogEntry struct {
	Name     string
	Duration float64
	ExitCode int
	Output   string
	Error    error
	Time     time.Time
}

// New creates a new TUI model
func New(eng *engine.Engine) Model {
	s := spinner.New()
	s.Spinner = spinner.Dot
	s.Style = lipgloss.NewStyle().Foreground(lipgloss.Color("205"))

	ti := textinput.New()
	ti.Placeholder = "Enter command..."
	ti.CharLimit = 256

	return Model{
		engine:       eng,
		keyMap:       DefaultKeyMap(),
		help:         help.New(),
		spinner:      s,
		commandInput: ti,
	}
}

// Init implements tea.Model
func (m Model) Init() tea.Cmd {
	return tea.Batch(
		m.spinner.Tick,
		m.listenForUpdates(),
		m.fetchModels(),
	)
}

// engineUpdateMsg carries an update from the engine
type engineUpdateMsg struct {
	update engine.CycleUpdate
}

// modelsLoadedMsg carries loaded models
type modelsLoadedMsg struct {
	models []ollama.ModelInfo
	err    error
}

func (m Model) listenForUpdates() tea.Cmd {
	return func() tea.Msg {
		update, ok := <-m.engine.Updates()
		if !ok {
			return nil
		}
		return engineUpdateMsg{update: update}
	}
}

func (m Model) fetchModels() tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		models, err := m.engine.Client().ListModels(ctx)
		return modelsLoadedMsg{models: models, err: err}
	}
}

// Update implements tea.Model
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.KeyMsg:
		return m.handleKeyMsg(msg)

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.updateViewports()
		return m, nil

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd

	case engineUpdateMsg:
		m = m.handleEngineUpdate(msg.update)
		return m, m.listenForUpdates()

	case modelsLoadedMsg:
		if msg.err == nil {
			m.models = msg.models
		}
		return m, nil
	}

	// Update viewports
	if !m.showCommand {
		var cmd tea.Cmd
		m.streamView, cmd = m.streamView.Update(msg)
		cmds = append(cmds, cmd)
	}

	return m, tea.Batch(cmds...)
}

func (m Model) handleKeyMsg(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Handle command input mode
	if m.showCommand {
		switch msg.String() {
		case "esc":
			m.showCommand = false
			m.commandInput.Reset()
			return m, nil
		case "enter":
			cmd := m.commandInput.Value()
			m.showCommand = false
			m.commandInput.Reset()
			return m.executeCommand(cmd)
		default:
			var cmd tea.Cmd
			m.commandInput, cmd = m.commandInput.Update(msg)
			return m, cmd
		}
	}

	// Handle model picker mode
	if m.showModels {
		switch msg.String() {
		case "esc":
			m.showModels = false
			return m, nil
		case "enter":
			if m.selectedModel < len(m.models) {
				m.engine.Client().SetModel(m.models[m.selectedModel].Name)
			}
			m.showModels = false
			return m, nil
		case "up", "k":
			if m.selectedModel > 0 {
				m.selectedModel--
			}
			return m, nil
		case "down", "j":
			if m.selectedModel < len(m.models)-1 {
				m.selectedModel++
			}
			return m, nil
		}
		return m, nil
	}

	// Normal mode keybindings
	switch {
	case key.Matches(msg, m.keyMap.Escape):
		return m.handleEscape()

	case key.Matches(msg, m.keyMap.CommandMode):
		m.showCommand = true
		m.commandInput.Focus()
		return m, textinput.Blink

	case key.Matches(msg, m.keyMap.ModelPicker):
		m.showModels = true
		return m, m.fetchModels()

	case key.Matches(msg, m.keyMap.ToggleDiff):
		m.showDiff = !m.showDiff
		m.updateViewports()
		return m, nil

	case key.Matches(msg, m.keyMap.OpenLogs):
		// Display log path
		m.streamContent.WriteString(fmt.Sprintf("\n[Logs: %s]\n", m.engine.Session().Path()))
		m.streamView.SetContent(m.streamContent.String())
		m.streamView.GotoBottom()
		return m, nil

	case key.Matches(msg, m.keyMap.Help):
		m.showHelp = !m.showHelp
		return m, nil

	case key.Matches(msg, m.keyMap.ScrollUp):
		m.streamView.LineUp(1)
		return m, nil

	case key.Matches(msg, m.keyMap.ScrollDown):
		m.streamView.LineDown(1)
		return m, nil

	case key.Matches(msg, m.keyMap.PageUp):
		m.streamView.HalfViewUp()
		return m, nil

	case key.Matches(msg, m.keyMap.PageDown):
		m.streamView.HalfViewDown()
		return m, nil
	}

	return m, nil
}

func (m Model) handleEscape() (tea.Model, tea.Cmd) {
	now := time.Now()

	// Check for double ESC (within 600ms)
	if now.Sub(m.lastEscTime) < 600*time.Millisecond {
		m.quitting = true
		m.engine.Stop()
		return m, tea.Quit
	}

	m.lastEscTime = now

	// Single ESC: cancel current operation
	m.engine.CancelCurrent()
	m.streamContent.WriteString("\n[Operation cancelled]\n")
	m.streamView.SetContent(m.streamContent.String())
	m.streamView.GotoBottom()

	return m, nil
}

func (m Model) executeCommand(cmd string) (tea.Model, tea.Cmd) {
	parts := strings.Fields(cmd)
	if len(parts) == 0 {
		return m, nil
	}

	switch parts[0] {
	case "/goal", "/g":
		if len(parts) > 1 {
			goal := strings.Join(parts[1:], " ")
			m.engine.SetGoal(goal)
			m.streamContent.WriteString(fmt.Sprintf("\n[Goal set: %s]\n", goal))
		}

	case "/model", "/m":
		if len(parts) > 1 {
			m.engine.Client().SetModel(parts[1])
			m.streamContent.WriteString(fmt.Sprintf("\n[Model switched to: %s]\n", parts[1]))
		}

	case "/models":
		m.showModels = true
		return m, m.fetchModels()

	case "/status", "/s":
		state := m.engine.GetState()
		objective := m.engine.GetObjective()
		m.streamContent.WriteString(fmt.Sprintf("\n[Status: %s | Objective: %s]\n", state, objective))

	case "/checkpoint", "/cp":
		go m.engine.Checkpoint(context.Background())
		m.streamContent.WriteString("\n[Creating checkpoint...]\n")

	case "/rollback", "/rb":
		go m.engine.Rollback(context.Background())
		m.streamContent.WriteString("\n[Rolling back...]\n")

	case "/speed":
		if len(parts) > 1 {
			var speed int
			fmt.Sscanf(parts[1], "%d", &speed)
			m.engine.SetSpeed(speed)
			m.streamContent.WriteString(fmt.Sprintf("\n[Speed set to: %d]\n", speed))
		}

	case "/help", "/h", "/?":
		m.showHelp = true

	default:
		m.streamContent.WriteString(fmt.Sprintf("\n[Unknown command: %s]\n", parts[0]))
	}

	m.streamView.SetContent(m.streamContent.String())
	m.streamView.GotoBottom()

	return m, nil
}

func (m Model) handleEngineUpdate(update engine.CycleUpdate) Model {
	// Update state display
	if update.Message != "" {
		m.streamContent.WriteString(fmt.Sprintf("[%s] %s\n", update.State, update.Message))
	}

	// Handle streaming tokens
	if update.TokenContent != "" {
		m.streamContent.WriteString(update.TokenContent)
		m.tokensPerSec = update.TokensPerSec
	}

	// Handle tool results
	if update.ToolResult != nil {
		m.lastExitCode = update.ToolResult.ExitCode
		m.toolLogs = append(m.toolLogs, ToolLogEntry{
			Name:     update.ToolResult.Name,
			Duration: update.ToolResult.Duration,
			ExitCode: update.ToolResult.ExitCode,
			Output:   update.ToolResult.Output,
			Error:    update.ToolResult.Error,
			Time:     time.Now(),
		})

		// Keep only last 100 tool logs
		if len(m.toolLogs) > 100 {
			m.toolLogs = m.toolLogs[len(m.toolLogs)-100:]
		}

		m.updateToolLogView()
	}

	// Update suggestions
	if len(update.Suggestions) > 0 {
		m.suggestions = update.Suggestions
	}

	// Handle errors
	if update.Error != nil {
		m.err = update.Error
		m.streamContent.WriteString(fmt.Sprintf("\n[ERROR] %v\n", update.Error))
	}

	m.streamView.SetContent(m.streamContent.String())
	m.streamView.GotoBottom()

	return m
}

func (m *Model) updateViewports() {
	headerHeight := 3
	footerHeight := 2
	contentHeight := m.height - headerHeight - footerHeight

	// Calculate widths
	mainWidth := m.width
	sideWidth := 0
	if m.showDiff && m.width > 120 {
		sideWidth = m.width / 3
		mainWidth = m.width - sideWidth - 1
	}

	// Stream view (main content)
	m.streamView = viewport.New(mainWidth, contentHeight-10)
	m.streamView.SetContent(m.streamContent.String())

	// Tool log view
	m.toolLogView = viewport.New(mainWidth, 8)
	m.updateToolLogView()

	// Backlog view (if showing diff/side panel)
	if sideWidth > 0 {
		m.backlogView = viewport.New(sideWidth, contentHeight)
		m.updateBacklogView()
	}
}

func (m *Model) updateToolLogView() {
	var content strings.Builder
	for i := len(m.toolLogs) - 1; i >= 0 && i >= len(m.toolLogs)-5; i-- {
		log := m.toolLogs[i]
		status := "OK"
		if log.ExitCode != 0 {
			status = fmt.Sprintf("EXIT:%d", log.ExitCode)
		}
		if log.Error != nil {
			status = "ERR"
		}
		content.WriteString(fmt.Sprintf("[%s] %s (%.2fs) %s\n",
			log.Time.Format("15:04:05"),
			log.Name,
			log.Duration,
			status,
		))
	}
	m.toolLogView.SetContent(content.String())
}

func (m *Model) updateBacklogView() {
	backlog := m.engine.GetBacklog()
	var content strings.Builder
	content.WriteString("BACKLOG\n")
	content.WriteString(strings.Repeat("─", 30) + "\n")
	for i, item := range backlog {
		if i >= 10 {
			content.WriteString(fmt.Sprintf("... and %d more\n", len(backlog)-10))
			break
		}
		content.WriteString(fmt.Sprintf("P%d %s\n", item.Priority, truncate(item.Description, 25)))
	}
	m.backlogView.SetContent(content.String())
}

// View implements tea.Model
func (m Model) View() string {
	if m.quitting {
		return "Goodbye!\n"
	}

	if m.width == 0 {
		return "Loading..."
	}

	var b strings.Builder

	// Render header
	b.WriteString(m.renderHeader())
	b.WriteString("\n")

	// Render main content
	if m.showHelp {
		b.WriteString(m.renderHelp())
	} else if m.showModels {
		b.WriteString(m.renderModelPicker())
	} else if m.showCommand {
		b.WriteString(m.renderCommandInput())
	} else {
		b.WriteString(m.renderMainContent())
	}

	// Render footer
	b.WriteString("\n")
	b.WriteString(m.renderFooter())

	return b.String()
}

func (m Model) renderHeader() string {
	state := m.engine.GetState()
	model := m.engine.Client().GetModel()
	project := m.engine.Project()
	branch := tools.GetCurrentBranch(project.Root)

	headerStyle := lipgloss.NewStyle().
		Background(lipgloss.Color("62")).
		Foreground(lipgloss.Color("230")).
		Padding(0, 1)

	modeStyle := lipgloss.NewStyle().
		Background(lipgloss.Color("205")).
		Foreground(lipgloss.Color("230")).
		Bold(true).
		Padding(0, 1)

	stateStyle := lipgloss.NewStyle().
		Background(lipgloss.Color("33")).
		Foreground(lipgloss.Color("230")).
		Padding(0, 1)

	statsStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("241"))

	// Build header parts
	mode := modeStyle.Render("AUTOPILOT")
	stateStr := stateStyle.Render(state.String())
	modelStr := headerStyle.Render(fmt.Sprintf("Model: %s", model))
	branchStr := headerStyle.Render(fmt.Sprintf("Branch: %s", branch))
	projectStr := headerStyle.Render(fmt.Sprintf("Project: %s", project.Type))

	stats := statsStyle.Render(fmt.Sprintf("%.1f tok/s | exit: %d", m.tokensPerSec, m.lastExitCode))

	left := lipgloss.JoinHorizontal(lipgloss.Left, mode, " ", stateStr, " ", m.spinner.View())
	right := lipgloss.JoinHorizontal(lipgloss.Right, modelStr, " ", branchStr, " ", projectStr, " ", stats)

	// Calculate spacing
	leftWidth := lipgloss.Width(left)
	rightWidth := lipgloss.Width(right)
	spacing := m.width - leftWidth - rightWidth
	if spacing < 1 {
		spacing = 1
	}

	return lipgloss.JoinHorizontal(lipgloss.Left, left, strings.Repeat(" ", spacing), right)
}

func (m Model) renderMainContent() string {
	var b strings.Builder

	// Suggestions panel
	if len(m.suggestions) > 0 {
		sugStyle := lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("62")).
			Padding(0, 1).
			Width(m.width - 4)

		var sugContent strings.Builder
		sugContent.WriteString("SUGGESTIONS:\n")
		for _, s := range m.suggestions {
			icon := "○"
			switch s.Status {
			case "EXECUTING":
				icon = "●"
			case "SKIPPED":
				icon = "⊘"
			}
			sugContent.WriteString(fmt.Sprintf(" %s %s — %s", icon, s.Item, s.Status))
			if s.Reason != "" {
				sugContent.WriteString(fmt.Sprintf(" (%s)", s.Reason))
			}
			sugContent.WriteString("\n")
		}

		b.WriteString(sugStyle.Render(sugContent.String()))
		b.WriteString("\n")
	}

	// Stream view
	streamStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("240"))

	b.WriteString(streamStyle.Render(m.streamView.View()))
	b.WriteString("\n")

	// Tool log view
	toolLogStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("240")).
		BorderTop(true)

	b.WriteString(toolLogStyle.Render(m.toolLogView.View()))

	return b.String()
}

func (m Model) renderHelp() string {
	helpStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("62")).
		Padding(1, 2).
		Width(m.width - 4)

	content := `KEYBINDINGS:
  ESC           Cancel current operation
  ESC ESC       Exit (within 600ms)
  Ctrl+K        Command palette
  Ctrl+M        Model picker
  Ctrl+D        Toggle diff panel
  Ctrl+L        Show logs path
  ?             Toggle help

COMMANDS:
  /goal <text>  Set the current goal
  /model <name> Switch model
  /models       Show model picker
  /status       Show current status
  /checkpoint   Create a checkpoint
  /rollback     Rollback to last checkpoint
  /speed <n>    Set throttle (0 = no throttle)

SCROLLING:
  ↑/k           Scroll up
  ↓/j           Scroll down
  PgUp/Ctrl+U   Page up
  PgDn/Ctrl+D   Page down

Press ? to close this help`

	return helpStyle.Render(content)
}

func (m Model) renderModelPicker() string {
	pickerStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("62")).
		Padding(1, 2).
		Width(m.width - 4)

	var content strings.Builder
	content.WriteString("SELECT MODEL:\n\n")

	currentModel := m.engine.Client().GetModel()

	for i, model := range m.models {
		cursor := "  "
		if i == m.selectedModel {
			cursor = "> "
		}

		current := ""
		if model.Name == currentModel {
			current = " (current)"
		}

		content.WriteString(fmt.Sprintf("%s%s%s\n", cursor, model.Name, current))
	}

	content.WriteString("\n↑/↓ to navigate, Enter to select, ESC to cancel")

	return pickerStyle.Render(content.String())
}

func (m Model) renderCommandInput() string {
	inputStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("205")).
		Padding(0, 1).
		Width(m.width - 4)

	return inputStyle.Render(fmt.Sprintf("Command: %s", m.commandInput.View()))
}

func (m Model) renderFooter() string {
	footerStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("241"))

	objective := m.engine.GetObjective()
	if objective == "" {
		objective = "No current objective"
	}

	left := fmt.Sprintf("Objective: %s", truncate(objective, 50))
	right := "ESC ESC to exit | ? for help"

	leftWidth := len(left)
	rightWidth := len(right)
	spacing := m.width - leftWidth - rightWidth
	if spacing < 1 {
		spacing = 1
	}

	return footerStyle.Render(left + strings.Repeat(" ", spacing) + right)
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}
