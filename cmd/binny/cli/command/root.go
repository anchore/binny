package command

import (
	"github.com/hashicorp/go-retryablehttp"
	"github.com/spf13/cobra"

	internalhttp "github.com/anchore/binny/internal/http"
	"github.com/anchore/binny/internal/log"
	"github.com/anchore/clio"
)

func Root(app clio.Application) *cobra.Command {
	cmd := app.SetupRootCommand(&cobra.Command{})

	// wrap any existing PersistentPreRunE to inject dependencies into context
	existingPreRunE := cmd.PersistentPreRunE
	cmd.PersistentPreRunE = func(cmd *cobra.Command, args []string) error {
		// FIRST: run clio's initialization (which sets up the logger via PreRunE)
		if existingPreRunE != nil {
			if err := existingPreRunE(cmd, args); err != nil {
				return err
			}
		}

		// Note: we intentionally do NOT store the base logger in context here.
		// The logger may not be fully initialized yet (clio initializes it in PreRunE, not PersistentPreRunE).
		// Instead, FromContext() will fall back to log.Get() which returns the current global logger.
		// When installers call WithNested(), they'll get the properly initialized logger at that point.

		// inject a configured HTTP client into the context
		ctx := cmd.Context()
		lgr := log.Get()
		httpClient := retryablehttp.NewClient()
		httpClient.Logger = internalhttp.NewLeveledLogger(lgr.Nested("component", "http-client"))
		ctx = internalhttp.WithHTTPClient(ctx, httpClient)

		cmd.SetContext(ctx)

		return nil
	}

	return cmd
}
