package ui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/gkampitakis/go-snaps/snaps"
	"github.com/stretchr/testify/require"
	"github.com/wagoodman/go-partybus"
	"github.com/wagoodman/go-progress"

	"github.com/anchore/binny/event"
)

func TestHandler_updateLock(t *testing.T) {

	tests := []struct {
		name       string
		eventFn    func(*testing.T) []partybus.Event
		iterations int
	}{
		{
			name: "update lock in progress",
			eventFn: func(t *testing.T) []partybus.Event {
				names := []string{"foo", "bar", "baz"}
				total := event.ManualStagedProgress{
					Manual:      progress.NewManual(100),
					AtomicStage: progress.NewAtomicStage("current total"),
				}

				start := partybus.Event{
					Type:   event.CLIUpdateCmdStarted,
					Source: names,
					Value:  total,
				}

				foo := event.ManualStagedProgress{
					Manual:      progress.NewManual(100),
					AtomicStage: progress.NewAtomicStage("current foo"),
				}
				foo.Manual.Set(100)
				foo.SetCompleted()

				fooCompleted := partybus.Event{
					Type: event.ToolUpdateVersionStartedEvent,
					Source: mockToolUpdate{
						name:    "foo",
						version: "1.2.3",
						update:  "1.3.0",
					},
					Value: foo,
				}

				bar := event.ManualStagedProgress{
					Manual:      progress.NewManual(100),
					AtomicStage: progress.NewAtomicStage("current bar"),
				}
				bar.Manual.Set(50)

				barStarted := partybus.Event{
					Type: event.ToolUpdateVersionStartedEvent,
					Source: mockToolUpdate{
						name:    "bar",
						version: "4.5.6",
						update:  "",
					},
					Value: bar,
				}

				return []partybus.Event{start, fooCompleted, barStarted}
			},
		},
		{
			name: "update lock completed",
			eventFn: func(t *testing.T) []partybus.Event {
				names := []string{"foo", "bar", "baz"}
				total := event.ManualStagedProgress{
					Manual:      progress.NewManual(100),
					AtomicStage: progress.NewAtomicStage("current total"),
				}

				start := partybus.Event{
					Type:   event.CLIUpdateCmdStarted,
					Source: names,
					Value:  total,
				}

				foo := event.ManualStagedProgress{
					Manual:      progress.NewManual(100),
					AtomicStage: progress.NewAtomicStage("current foo"),
				}
				foo.Manual.Set(100)
				foo.SetCompleted()

				fooCompleted := partybus.Event{
					Type: event.ToolUpdateVersionStartedEvent,
					Source: mockToolUpdate{
						name:    "foo",
						version: "1.2.3",
						update:  "1.3.0",
					},
					Value: foo,
				}

				bar := event.ManualStagedProgress{
					Manual:      progress.NewManual(100),
					AtomicStage: progress.NewAtomicStage("current bar"),
				}
				bar.Manual.Set(100)
				bar.SetCompleted()

				barStarted := partybus.Event{
					Type: event.ToolUpdateVersionStartedEvent,
					Source: mockToolUpdate{
						name:    "bar",
						version: "4.5.6",
						update:  "",
					},
					Value: bar,
				}

				baz := event.ManualStagedProgress{
					Manual:      progress.NewManual(100),
					AtomicStage: progress.NewAtomicStage("current baz"),
				}
				baz.Manual.Set(100)
				baz.SetCompleted()

				bazStarted := partybus.Event{
					Type: event.ToolUpdateVersionStartedEvent,
					Source: mockToolUpdate{
						name:    "baz",
						version: "7.8.9",
						update:  "7.8.9", // filled in but matches
					},
					Value: baz,
				}
				return []partybus.Event{start, fooCompleted, barStarted, bazStarted}
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// the specific color and formatting matters for the snapshot
			t.Setenv("CLICOLOR_FORCE", "1")

			events := tt.eventFn(t)
			handler := New(DefaultHandlerConfig())
			handler.WindowSize = tea.WindowSizeMsg{
				Width:  100,
				Height: 80,
			}

			require.NotEmpty(t, events)

			var cmds []tea.Cmd
			for i := range events {
				cmd := events[i]
				cmds = append(cmds, func() tea.Msg {
					return cmd
				})
			}

			models := handler.Handle(events[0])

			require.Len(t, models, 1)
			model := models[0]

			tsk, ok := model.(updateLockViewModel)
			require.True(t, ok)

			cmds = append([]tea.Cmd{tsk.Init()}, cmds...)

			got := runModel(t, tsk, tt.iterations, tea.Batch(cmds...))
			t.Log(got)
			snaps.MatchSnapshot(t, got)
		})
	}
}

var _ event.ToolUpdate = (*mockToolUpdate)(nil)

type mockToolUpdate struct {
	name    string
	version string
	update  string
}

func (m mockToolUpdate) Updated() string {
	return m.update
}

func (m mockToolUpdate) Name() string {
	return m.name
}

func (m mockToolUpdate) Version() string {
	return m.version
}
