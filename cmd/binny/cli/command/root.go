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
		ctx := cmd.Context()

		// inject the global logger into the context
		lgr := log.Get()
		ctx = log.WithLogger(ctx, lgr)

		// inject a configured HTTP client into the context
		httpClient := retryablehttp.NewClient()
		httpClient.Logger = internalhttp.NewLeveledLogger(lgr.Nested("component", "http-client"))
		ctx = internalhttp.WithHTTPClient(ctx, httpClient)

		cmd.SetContext(ctx)

		if existingPreRunE != nil {
			return existingPreRunE(cmd, args)
		}
		return nil
	}

	return cmd
}
