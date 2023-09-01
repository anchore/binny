package cli

import (
	"github.com/anchore/binny/cmd/binny/cli/command"
	"github.com/anchore/binny/internal/bus"
	"github.com/anchore/binny/internal/log"
	"github.com/anchore/binny/internal/redact"
	"github.com/anchore/clio"
	"github.com/anchore/go-logger"
)

// New constructs the `syft packages` command, aliases the root command to `syft packages`,
// and constructs the `syft power-user` command. It is also responsible for
// organizing flag usage and injecting the application config for each command.
// It also constructs the syft attest command and the syft version command.
// `RunE` is the earliest that the complete application configuration can be loaded.
func New(id clio.Identification) clio.Application {
	clioCfg := clio.NewSetupConfig(id).
		WithGlobalConfigFlag().   // add persistent -c <path> for reading an application config from
		WithGlobalLoggingFlags(). // add persistent -v and -q flags tied to the logging config
		WithConfigInRootHelp().   // --help on the root command renders the full application config in the help text
		WithNoBus().
		WithLoggingConfig(clio.LoggingConfig{
			Level: logger.InfoLevel,
		}).
		WithInitializers(
			func(state *clio.State) error {
				// clio is setting up and providing the bus, redact store, and logger to the application. Once loaded,
				// we can hoist them into the internal packages for global use.
				bus.Set(state.Bus)
				redact.Set(state.RedactStore)
				log.Set(state.Logger)

				return nil
			},
		)

	app := clio.New(*clioCfg)

	root := command.Root(app)

	root.AddCommand(command.Install(app))
	root.AddCommand(command.Check(app))
	root.AddCommand(command.Run(app))
	root.AddCommand(command.UpdateLock(app))

	return app
}
