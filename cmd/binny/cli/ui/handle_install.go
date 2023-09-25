package ui

import (
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/wagoodman/go-partybus"
	"github.com/wagoodman/go-progress"

	"github.com/anchore/binny/event"
	"github.com/anchore/binny/internal/log"
)

var _ tea.Model = (*installViewModel)(nil)

func (m *Handler) handleCLIInstallCmdStarted(e partybus.Event) []tea.Model {
	toolNames, prog, err := event.ParseInstallCmdStarted(e)
	if err != nil {
		log.WithFields("error", err).Warn("unable to parse event")
		return nil
	}

	return []tea.Model{newInstallViewModel(toolNames, prog, m.WindowSize)}
}

type installViewModel struct {
	ToolNames []string
	Total     progress.StagedProgressable
	Progress  map[string]progress.StagedProgressable
	Info      map[string]event.Tool

	WindowSize tea.WindowSizeMsg
	Spinner    spinner.Model

	ToolNameStyle   lipgloss.Style
	TitleStyle      lipgloss.Style
	WaitingStyle    lipgloss.Style
	InstallingStyle lipgloss.Style
	DoneStyle       lipgloss.Style
	ErrorStyle      lipgloss.Style
}

func newInstallViewModel(toolNames []string, total progress.StagedProgressable, windowSize tea.WindowSizeMsg) installViewModel {
	padding := 0
	for _, name := range toolNames {
		if len(name) > padding {
			padding = len(name)
		}
	}

	return installViewModel{
		ToolNames: toolNames,
		Total:     total,
		Progress:  make(map[string]progress.StagedProgressable),
		Info:      make(map[string]event.Tool),

		Spinner: spinner.New(
			spinner.WithSpinner(
				// matches the same spinner as syft/grype
				spinner.Spinner{
					Frames: strings.Split("⠋⠙⠹⠸⠼⠴⠦⠧⠇⠏", ""),
					FPS:    150 * time.Millisecond,
				},
			),
			spinner.WithStyle(
				lipgloss.NewStyle().Foreground(lipgloss.Color("13")), // 13 = high intentity magenta (ANSI 16 bit color code)
			),
		),

		WindowSize: windowSize,

		ToolNameStyle:   lipgloss.NewStyle().Width(padding),
		TitleStyle:      lipgloss.NewStyle().Bold(true),
		WaitingStyle:    lipgloss.NewStyle().Foreground(lipgloss.Color("#777777")), // grey
		InstallingStyle: lipgloss.NewStyle().Foreground(lipgloss.Color("214")),     // 214 = orange1 (ANSI 16 bit color code)
		DoneStyle:       lipgloss.NewStyle().Foreground(lipgloss.Color("10")),      // 10 = high intensity green (ANSI 16 bit color code)
		ErrorStyle:      lipgloss.NewStyle().Foreground(lipgloss.Color("9")),       // 9 = high intensity red (ANSI 16 bit color code)
	}
}

func (m installViewModel) Init() tea.Cmd {
	return m.Spinner.Tick
}

func (m installViewModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.WindowSize = msg
		return m, nil

	case spinner.TickMsg:
		spinModel, spinCmd := m.Spinner.Update(msg)
		m.Spinner = spinModel
		return m, spinCmd

	case partybus.Event:
		log.WithFields("component", "ui").Tracef("event: %q", msg.Type)

		if msg.Type == event.ToolInstallationStartedEvent {
			tool, prog, err := event.ParseToolInstallationStarted(msg)
			if err != nil {
				log.WithFields("error", err).Trace("unable to parse event")
				return m, nil
			}

			m.Progress[tool.Name()] = prog
			m.Info[tool.Name()] = tool

			return m, nil
		}
	}
	return m, nil
}

func (m installViewModel) View() string {
	isCompleted := progress.IsCompleted(m.Total)

	s := m.wideView(isCompleted)
	if lipgloss.Width(s) > m.WindowSize.Width {
		return m.longView(isCompleted)
	}
	return s
}

func (m installViewModel) longView(isCompleted bool) string {
	s := strings.Builder{}
	s.WriteString(m.titleViewComponent(m.Total.Error()) + "\n")

	for i, toolName := range m.ToolNames {
		if i < len(m.ToolNames)-1 {
			s.WriteString(m.WaitingStyle.Render("   ├── "))
		} else {
			s.WriteString(m.WaitingStyle.Render("   └── "))
		}

		formattedName := m.toolStatusStyle(m.Progress[toolName], isCompleted).Render(toolName)
		s.WriteString(m.ToolNameStyle.Render(formattedName))

		info := m.Info[toolName]
		if info != nil {
			s.WriteString(" " + info.Version())
		}

		s.WriteString("\n")
	}
	return s.String()
}

func (m installViewModel) wideView(isCompleted bool) string {
	s := strings.Builder{}
	s.WriteString(m.titleViewComponent(m.Total.Error()))

	for i, tool := range m.ToolNames {
		formattedName := m.toolStatusStyle(m.Progress[tool], isCompleted).Render(tool)
		s.WriteString(formattedName)

		if i < len(m.ToolNames)-1 {
			s.WriteString(m.WaitingStyle.Render(", "))
		}
	}
	return s.String()
}

func (m installViewModel) titleViewComponent(totalErr error) string {
	s := strings.Builder{}
	var (
		status string
		title  = m.Total.Stage()
	)
	switch {
	case progress.IsErrCompleted(totalErr):
		status = m.DoneStyle.Bold(true).Render("✔")
	case totalErr != nil:
		status = m.ErrorStyle.Bold(true).Render("✘")
	default:
		status = m.Spinner.View()
	}

	s.WriteString(" " + status + " ")
	s.WriteString(m.TitleStyle.Render(title) + "   ")

	return s.String()
}

func (m installViewModel) toolStatusStyle(prog progress.Progressable, isCompleted bool) lipgloss.Style {
	var style *lipgloss.Style
	switch {
	case prog == nil:
		return m.WaitingStyle
	case progress.IsCompleted(prog):
		style = &m.DoneStyle
	case prog.Current() > 0:
		style = &m.InstallingStyle
	case prog.Error() != nil:
		return m.ErrorStyle
	}

	if style != nil {
		if isCompleted {
			return m.WaitingStyle
		}
		return *style
	}

	return m.WaitingStyle
}
