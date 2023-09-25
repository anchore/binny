package ui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/gkampitakis/go-snaps/snaps"
	"github.com/stretchr/testify/require"
	"github.com/wagoodman/go-partybus"
	"github.com/wagoodman/go-progress"

	"github.com/anchore/binny/event"
	"github.com/anchore/bubbly/bubbles/taskprogress"
)

func TestHandler_taskStarted(t *testing.T) {

	tests := []struct {
		name       string
		eventFn    func(*testing.T) partybus.Event
		iterations int
	}{
		{
			name: "task in progress",
			eventFn: func(t *testing.T) partybus.Event {
				prog := &event.ManualStagedProgress{
					AtomicStage: progress.NewAtomicStage("current"),
					Manual:      progress.NewManual(100),
				}
				prog.Manual.Set(50)

				return partybus.Event{
					Type: event.TaskStartedEvent,
					Source: event.Task{
						Title: event.Title{
							Default:      "do something",
							WhileRunning: "doing something",
							OnSuccess:    "done something",
						},
						Context: "ctx",
					},
					Value: prog,
				}
			},
		},
		{
			name: "task complete",
			eventFn: func(t *testing.T) partybus.Event {
				prog := &event.ManualStagedProgress{
					AtomicStage: progress.NewAtomicStage("current"),
					Manual:      progress.NewManual(100),
				}
				prog.Manual.Set(100)
				prog.SetCompleted()

				return partybus.Event{
					Type: event.TaskStartedEvent,
					Source: event.Task{
						Title: event.Title{
							Default:      "do something",
							WhileRunning: "doing something",
							OnSuccess:    "done something",
						},
						Context: "ctx",
					},
					Value: prog,
				}
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// the specific color and formatting matters for the snapshot
			t.Setenv("CLICOLOR_FORCE", "1")

			e := tt.eventFn(t)
			handler := New(DefaultHandlerConfig())
			handler.WindowSize = tea.WindowSizeMsg{
				Width:  100,
				Height: 80,
			}

			models := handler.Handle(e)
			require.Len(t, models, 1)
			model := models[0]

			tsk, ok := model.(taskprogress.Model)
			require.True(t, ok)

			got := runModel(t, tsk, tt.iterations, model.Init())
			t.Log(got)
			snaps.MatchSnapshot(t, got)
		})
	}
}
