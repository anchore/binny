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

func TestHandler_install(t *testing.T) {

	tests := []struct {
		name       string
		eventFn    func(*testing.T) []partybus.Event
		iterations int
	}{
		{
			name: "install in progress",
			eventFn: func(t *testing.T) []partybus.Event {
				names := []string{"foo", "bar", "baz"}
				total := event.ManualStagedProgress{
					Manual:      progress.NewManual(100),
					AtomicStage: progress.NewAtomicStage("current total"),
				}

				start := partybus.Event{
					Type:   event.CLIInstallCmdStarted,
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
					Type: event.ToolInstallationStartedEvent,
					Source: mockTool{
						name:    "foo",
						version: "1.2.3",
					},
					Value: foo,
				}

				bar := event.ManualStagedProgress{
					Manual:      progress.NewManual(100),
					AtomicStage: progress.NewAtomicStage("current bar"),
				}
				bar.Manual.Set(50)

				barStarted := partybus.Event{
					Type: event.ToolInstallationStartedEvent,
					Source: mockTool{
						name:    "bar",
						version: "4.5.6",
					},
					Value: bar,
				}

				return []partybus.Event{start, fooCompleted, barStarted}
			},
		},
		{
			name: "install completed",
			eventFn: func(t *testing.T) []partybus.Event {
				names := []string{"foo", "bar", "baz"}
				total := event.ManualStagedProgress{
					Manual:      progress.NewManual(100),
					AtomicStage: progress.NewAtomicStage("current total"),
				}
				total.Manual.Set(100)
				total.SetCompleted()

				start := partybus.Event{
					Type:   event.CLIInstallCmdStarted,
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
					Type: event.ToolInstallationStartedEvent,
					Source: mockTool{
						name:    "foo",
						version: "1.2.3",
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
					Type: event.ToolInstallationStartedEvent,
					Source: mockTool{
						name:    "bar",
						version: "4.5.6",
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
					Type: event.ToolInstallationStartedEvent,
					Source: mockTool{
						name:    "baz",
						version: "7.8.9",
					},
					Value: bar,
				}

				return []partybus.Event{start, fooCompleted, barStarted, bazStarted}
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			configs := []struct {
				Name  string
				Width int
			}{
				{
					Name:  "wide",
					Width: 1000,
				},
				{
					Name:  "narrow",
					Width: 20,
				},
			}
			for _, cfg := range configs {
				// the specific color and formatting matters for the snapshot
				t.Setenv("CLICOLOR_FORCE", "1")

				events := tt.eventFn(t)
				handler := New(DefaultHandlerConfig())
				handler.WindowSize = tea.WindowSizeMsg{
					Width:  cfg.Width,
					Height: 80,
				}

				require.NotEmpty(t, events)

				start := events[0]
				var remainingCmds []tea.Cmd
				if len(events) > 1 {
					remaining := events[1:]
					for i := range remaining {
						cmd := remaining[i]
						remainingCmds = append(remainingCmds, func() tea.Msg {
							return cmd
						})
					}
				}

				models := handler.Handle(start)

				require.Len(t, models, 1)
				model := models[0]

				tsk, ok := model.(installViewModel)
				require.True(t, ok)

				got := runModel(t, tsk, tt.iterations, tea.Batch(remainingCmds...))
				t.Log(got)
				snaps.MatchSnapshot(t, got)
			}
		})
	}
}

var _ event.Tool = (*mockTool)(nil)

type mockTool struct {
	name    string
	version string
}

func (m mockTool) Name() string {
	return m.name
}

func (m mockTool) Version() string {
	return m.version
}
