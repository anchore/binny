package event

import (
	"github.com/wagoodman/go-partybus"
)

const (
	typePrefix    = "binny"
	cliTypePrefix = typePrefix + "-cli"

	// Events from the binny library

	// ToolInstallationStartedEvent is a partybus event that occurs when a single tool installation has begun
	ToolInstallationStartedEvent partybus.EventType = typePrefix + "-tool-installation-started"

	// ToolUpdateVersionStartedEvent is a partybus event that occurs when a single tool version update has begun (for update-lock command)
	ToolUpdateVersionStartedEvent partybus.EventType = typePrefix + "-tool-update-version-started"

	// TaskStartedEvent is a generic, monitorable partybus event that occurs when a task has begun
	TaskStartedEvent partybus.EventType = typePrefix + "-task"

	// Events exclusively for the CLI

	// CLIInstallCmdStarted is a partybus event that occurs when the install CLI command has begun
	CLIInstallCmdStarted partybus.EventType = cliTypePrefix + "-install-cmd-started"

	// CLIUpdateLockCmdStarted is a partybus event that occurs when the install CLI command has begun
	CLIUpdateLockCmdStarted partybus.EventType = cliTypePrefix + "-update-lock-cmd-started"

	// CLIReport is a partybus event that occurs when an analysis result is ready for final presentation to stdout
	CLIReport partybus.EventType = cliTypePrefix + "-report"

	// CLINotification is a partybus event that occurs when auxiliary information is ready for presentation to stderr
	CLINotification partybus.EventType = cliTypePrefix + "-notification"
)
