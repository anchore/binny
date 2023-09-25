package ui

import (
	"reflect"
	"sync"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func runModel(t testing.TB, m tea.Model, iterations int, cmd tea.Cmd, h ...*sync.WaitGroup) string {
	t.Helper()
	if iterations == 0 {
		iterations = 1
	}
	m.Init()

	for _, each := range h {
		if each != nil {
			each.Wait()
		}
	}

	for i := 0; cmd != nil && i < iterations; i++ {
		msgs := flatten(cmd())
		var nextCmds []tea.Cmd
		var next tea.Cmd
		for _, msg := range msgs {
			t.Logf("Message: %+v %+v\n", reflect.TypeOf(msg), msg)
			m, next = m.Update(msg)
			nextCmds = append(nextCmds, next)
		}
		cmd = tea.Batch(nextCmds...)
	}

	return m.View()
}

func flatten(p tea.Msg) (msgs []tea.Msg) {
	switch v := p.(type) {
	case tea.BatchMsg:
		for _, cmd := range v {
			msgs = append(msgs, flatten(cmd())...)
		}
	default:
		msgs = []tea.Msg{p}
	}

	return msgs
}
