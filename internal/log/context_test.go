package log

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/anchore/go-logger"
	"github.com/anchore/go-logger/adapter/logrus"
	"github.com/anchore/go-logger/adapter/redact"
)

func TestWithNested(t *testing.T) {
	// Create a real logger at trace level
	cfg := logrus.Config{
		EnableConsole: true,
		Level:         logger.TraceLevel,
	}
	lgr, err := logrus.New(cfg)
	require.NoError(t, err)

	// Set as the global logger (like clio does)
	Set(lgr)

	// Create a context with the logger (like root.go does)
	ctx := context.Background()
	ctx = WithLogger(ctx, Get())

	// Test FromContext
	ctxLgr := FromContext(ctx)
	require.NotNil(t, ctxLgr)

	// Test direct trace on context logger
	t.Log("Testing direct trace on context logger...")
	ctxLgr.Trace("test 1: context logger trace")
	ctxLgr.WithFields("key", "value").Trace("test 2: context logger WithFields trace")

	// Test WithNested
	t.Log("Testing WithNested...")
	newCtx, nestedLgr := WithNested(ctx, "nested", "context")
	require.NotNil(t, nestedLgr)
	require.NotEqual(t, ctx, newCtx)

	// Test trace on nested logger
	t.Log("Testing trace on nested logger...")
	nestedLgr.Trace("test 3: nested logger trace")
	nestedLgr.WithFields("key", "value").Trace("test 4: nested logger WithFields trace")

	// Test FromContext on new context
	t.Log("Testing FromContext on new context...")
	fromNewCtx := FromContext(newCtx)
	fromNewCtx.Trace("test 5: FromContext nested trace")
	fromNewCtx.WithFields("key", "value").Trace("test 6: FromContext nested WithFields trace")
}

func TestWithNestedAndRedactWrapper(t *testing.T) {
	// Create a real logger at trace level
	cfg := logrus.Config{
		EnableConsole: true,
		Level:         logger.TraceLevel,
	}
	baseLgr, err := logrus.New(cfg)
	require.NoError(t, err)

	// Wrap with redact logger (like binny's log.Set does when store is not nil)
	redactStore := redact.NewStore()
	lgr := redact.New(baseLgr, redactStore)

	// Reset global log and set the wrapped logger
	log = lgr

	// Create a context with the logger (like root.go does)
	ctx := context.Background()
	ctx = WithLogger(ctx, Get())

	// Test FromContext
	ctxLgr := FromContext(ctx)
	require.NotNil(t, ctxLgr)

	// Test direct trace on context logger
	t.Log("Testing direct trace on context logger (with redact wrapper)...")
	ctxLgr.Trace("test 1: context logger trace")
	ctxLgr.WithFields("key", "value").Trace("test 2: context logger WithFields trace")

	// Test WithNested
	t.Log("Testing WithNested (with redact wrapper)...")
	newCtx, nestedLgr := WithNested(ctx, "nested", "context")
	require.NotNil(t, nestedLgr)
	require.NotEqual(t, ctx, newCtx)

	// Test trace on nested logger
	t.Log("Testing trace on nested logger (with redact wrapper)...")
	nestedLgr.Trace("test 3: nested logger trace")
	nestedLgr.WithFields("key", "value").Trace("test 4: nested logger WithFields trace")

	// Test FromContext on new context
	t.Log("Testing FromContext on new context (with redact wrapper)...")
	fromNewCtx := FromContext(newCtx)
	fromNewCtx.Trace("test 5: FromContext nested trace")
	fromNewCtx.WithFields("key", "value").Trace("test 6: FromContext nested WithFields trace")
}
