package event

import (
	"fmt"

	"github.com/wagoodman/go-partybus"
	"github.com/wagoodman/go-progress"
)

type Tool interface {
	Name() string
	Version() string
}

type ToolUpdate interface {
	Tool
	Updated() string
}

type ErrBadPayload struct {
	Type  partybus.EventType
	Field string
	Value interface{}
}

func (e *ErrBadPayload) Error() string {
	return fmt.Sprintf("event='%s' has bad event payload field='%v': '%+v'", string(e.Type), e.Field, e.Value)
}

func newPayloadErr(t partybus.EventType, field string, value interface{}) error {
	return &ErrBadPayload{
		Type:  t,
		Field: field,
		Value: value,
	}
}

func checkEventType(actual, expected partybus.EventType) error {
	if actual != expected {
		return newPayloadErr(expected, "Type", actual)
	}
	return nil
}

func ParseTaskStarted(e partybus.Event) (*Task, progress.StagedProgressable, error) {
	if err := checkEventType(e.Type, TaskStartedEvent); err != nil {
		return nil, nil, err
	}

	cmd, ok := e.Source.(Task)
	if !ok {
		return nil, nil, newPayloadErr(e.Type, "Source", e.Source)
	}

	p, ok := e.Value.(progress.StagedProgressable)
	if !ok {
		return nil, nil, newPayloadErr(e.Type, "Value", e.Value)
	}

	return &cmd, p, nil
}

func ParseInstallCmdStarted(e partybus.Event) ([]string, progress.StagedProgressable, error) {
	if err := checkEventType(e.Type, CLIInstallCmdStarted); err != nil {
		return nil, nil, err
	}

	return parseNamesAndStagedProgressable(e)
}

func ParseToolInstallationStarted(e partybus.Event) (Tool, progress.StagedProgressable, error) {
	if err := checkEventType(e.Type, ToolInstallationStartedEvent); err != nil {
		return nil, nil, err
	}

	t, ok := e.Source.(Tool)
	if !ok {
		return nil, nil, newPayloadErr(e.Type, "Source", e.Source)
	}

	prog, ok := e.Value.(progress.StagedProgressable)
	if !ok {
		return nil, nil, newPayloadErr(e.Type, "Value", e.Value)
	}

	return t, prog, nil
}

func ParseUpdateLockCmdStarted(e partybus.Event) ([]string, progress.StagedProgressable, error) {
	if err := checkEventType(e.Type, CLIUpdateCmdStarted); err != nil {
		return nil, nil, err
	}

	return parseNamesAndStagedProgressable(e)
}

func ParseToolUpdateVersionStarted(e partybus.Event) (ToolUpdate, progress.Monitorable, error) {
	if err := checkEventType(e.Type, ToolUpdateVersionStartedEvent); err != nil {
		return nil, nil, err
	}

	name, ok := e.Source.(ToolUpdate)
	if !ok {
		return nil, nil, newPayloadErr(e.Type, "Source", e.Source)
	}

	prog, ok := e.Value.(progress.Monitorable)
	if !ok {
		return nil, nil, newPayloadErr(e.Type, "Value", e.Value)
	}

	return name, prog, nil
}

func parseNamesAndStagedProgressable(e partybus.Event) ([]string, progress.StagedProgressable, error) {
	names, ok := e.Source.([]string)
	if !ok {
		return nil, nil, newPayloadErr(e.Type, "Source", e.Source)
	}

	prog, ok := e.Value.(progress.StagedProgressable)
	if !ok {
		return nil, nil, newPayloadErr(e.Type, "Value", e.Value)
	}

	return names, prog, nil
}

func ParseCLIReport(e partybus.Event) (string, string, error) {
	if err := checkEventType(e.Type, CLIReport); err != nil {
		return "", "", err
	}

	context, ok := e.Source.(string)
	if !ok {
		// this is optional
		context = ""
	}

	report, ok := e.Value.(string)
	if !ok {
		return "", "", newPayloadErr(e.Type, "Value", e.Value)
	}

	return context, report, nil
}

func ParseCLINotification(e partybus.Event) (string, string, error) {
	if err := checkEventType(e.Type, CLINotification); err != nil {
		return "", "", err
	}

	context, ok := e.Source.(string)
	if !ok {
		// this is optional
		context = ""
	}

	notification, ok := e.Value.(string)
	if !ok {
		return "", "", newPayloadErr(e.Type, "Value", e.Value)
	}

	return context, notification, nil
}
