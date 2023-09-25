package ui

import (
	"fmt"
	"strings"
	"sync"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/wagoodman/go-partybus"
	"github.com/wagoodman/go-progress"

	"github.com/anchore/binny/event"
	"github.com/anchore/binny/internal/log"
	"github.com/anchore/bubbly/bubbles/taskprogress"
)

var _ tea.Model = (*updateLockViewModel)(nil)

func (m *Handler) handleCLIUpdateLockCmdStarted(e partybus.Event) []tea.Model {
	toolNames, prog, err := event.ParseUpdateLockCmdStarted(e)
	if err != nil {
		log.WithFields("error", err).Warn("unable to parse event")
		return nil
	}

	return []tea.Model{newUpdateLockViewModel(m.Running, toolNames, prog)}
}

type updateLockViewModel struct {
	ToolNames []string
	Total     progress.StagedProgressable
	Progress  map[string]progress.Monitorable
	Info      map[string]event.ToolUpdate

	WindowSize tea.WindowSizeMsg

	Task taskprogress.Model

	DefaultStyle  lipgloss.Style
	ToolNameStyle lipgloss.Style
	WaitingStyle  lipgloss.Style
	UpdatingStyle lipgloss.Style
	DoneStyle     lipgloss.Style
	ErrorStyle    lipgloss.Style
}

func newUpdateLockViewModel(wg *sync.WaitGroup, toolNames []string, total progress.StagedProgressable) updateLockViewModel {
	prog := taskprogress.New(
		wg,
		taskprogress.WithStagedProgressable(total),
	)

	prog.TitleOptions = taskprogress.Title{
		Default: "Update version locks",
		Running: "Updating version locks",
		Success: "Updated version locks",
	}

	prog.HideProgressOnSuccess = true
	prog.HideStageOnSuccess = false
	prog.TitleWidth = len(prog.TitleOptions.Running)

	padding := 0
	for _, name := range toolNames {
		if len(name) > padding {
			padding = len(name)
		}
	}

	return updateLockViewModel{
		ToolNames: toolNames,
		Total:     total,
		Progress:  make(map[string]progress.Monitorable),
		Info:      make(map[string]event.ToolUpdate),

		Task: prog,

		DefaultStyle:  lipgloss.NewStyle(),
		ToolNameStyle: lipgloss.NewStyle().Width(padding),
		WaitingStyle:  lipgloss.NewStyle().Foreground(lipgloss.Color("#777777")), // grey
		UpdatingStyle: lipgloss.NewStyle().Foreground(lipgloss.Color("214")),     // 214 = orange1 (ANSI 16 bit color code)
		DoneStyle:     lipgloss.NewStyle().Foreground(lipgloss.Color("10")),      // 10 = high intensity green (ANSI 16 bit color code)
		ErrorStyle:    lipgloss.NewStyle().Foreground(lipgloss.Color("9")),       // 9 = high intensity red (ANSI 16 bit color code)
	}
}

func (m updateLockViewModel) Init() tea.Cmd {
	return m.Task.Init()
}

func (m updateLockViewModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.WindowSize = msg
		// don't return, the taskprogress model may also need to update the window size

	case partybus.Event:
		log.WithFields("component", "ui").Tracef("event: %q", msg.Type)

		if msg.Type == event.ToolUpdateVersionStartedEvent {
			toolUpdate, prog, err := event.ParseToolUpdateVersionStarted(msg)
			if err != nil {
				log.WithFields("error", err).Trace("unable to parse event")
				return m, nil
			}

			m.Progress[toolUpdate.Name()] = prog
			m.Info[toolUpdate.Name()] = toolUpdate

			return m, nil
		}
	}

	tm, tc := m.Task.Update(msg)
	m.Task = tm.(taskprogress.Model)
	return m, tc
}

func (m updateLockViewModel) View() string {
	s := strings.Builder{}
	s.WriteString(m.Task.View())
	s.WriteString("\n")

	for i, tool := range m.ToolNames {
		if i < len(m.ToolNames)-1 {
			s.WriteString(m.WaitingStyle.Render("   ├── "))
		} else {
			s.WriteString(m.WaitingStyle.Render("   └── "))
		}
		p, ok := m.Progress[tool]

		if !ok {
			s.WriteString(m.WaitingStyle.Render(tool) + "\n")
			continue
		}

		info := m.Info[tool]

		ogVersion := info.Version()
		newVersion := info.Updated()
		isUpdated := ogVersion != newVersion && newVersion != ""

		version := ogVersion
		if isUpdated {
			version = fmt.Sprintf("%s → %s", ogVersion, newVersion)
		}

		formattedLine := fmt.Sprintf("%s %s",
			m.ToolNameStyle.Render(tool),
			m.toolStatusStyle(p, isUpdated).Render(version),
		)
		s.WriteString(formattedLine)

		if i < len(m.ToolNames)-1 {
			s.WriteString("\n")
		}
	}

	return s.String()
}

func (m updateLockViewModel) toolStatusStyle(prog progress.Monitorable, isUpdated bool) lipgloss.Style {
	switch {
	case prog == nil:
		return m.WaitingStyle
	case progress.IsErrCompleted(prog.Error()):
		if isUpdated {
			return m.DoneStyle
		}
		return m.DefaultStyle
	case prog.Current() > 0:
		return m.UpdatingStyle
	case prog.Error() != nil:
		return m.ErrorStyle
	}

	return m.WaitingStyle
}
