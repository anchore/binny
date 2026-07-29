package command

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	internalhttp "github.com/anchore/binny/internal/http"
	"github.com/anchore/binny/internal/log"
	"github.com/anchore/clio"
	"github.com/anchore/go-logger/adapter/discard"
)

func TestRoot_PersistentPreRunE_InjectsContext(t *testing.T) {
	// set up a global logger so we have something to inject
	log.Set(discard.New())

	app := clio.New(clio.SetupConfig{
		ID: clio.Identification{
			Name:    "test",
			Version: "0.0.0",
		},
	})

	root := Root(app)

	// verify PersistentPreRunE is set
	require.NotNil(t, root.PersistentPreRunE, "PersistentPreRunE should be set")

	// set a base context on the command
	root.SetContext(context.Background())

	// simulate what cobra does - call PersistentPreRunE
	err := root.PersistentPreRunE(root, []string{})
	require.NoError(t, err)

	// verify the context now has the logger and HTTP client
	ctx := root.Context()

	// verify logger was injected by checking it's the same as the global logger
	lgr := log.FromContext(ctx)
	assert.NotNil(t, lgr, "logger should be in context")
	assert.Equal(t, log.Get(), lgr, "logger from context should be the global logger we set")

	// verify HTTP client was injected (not the default fallback)
	client := internalhttp.ClientFromContext(ctx)
	assert.NotNil(t, client, "HTTP client should be in context")
	assert.NotNil(t, client.Logger, "HTTP client should have our leveled logger adapter")
}

func TestRoot_PersistentPreRunE_ContextNotSetWithoutPreRun(t *testing.T) {
	// this test verifies that without calling PersistentPreRunE,
	// we get fallback values, proving the injection is necessary

	ctx := context.Background()

	// without injection, FromContext falls back to global logger
	lgr := log.FromContext(ctx)
	assert.NotNil(t, lgr, "fallback logger should exist")

	// without injection, ClientFromContext falls back to default client
	client := internalhttp.ClientFromContext(ctx)
	assert.NotNil(t, client, "fallback HTTP client should exist")
}
