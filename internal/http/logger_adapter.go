package http

import (
	"github.com/hashicorp/go-retryablehttp"

	"github.com/anchore/go-logger"
)

var _ retryablehttp.LeveledLogger = (*leveledLoggerAdapter)(nil)

// leveledLoggerAdapter adapts a go-logger.Logger to the retryablehttp.LeveledLogger interface.
type leveledLoggerAdapter struct {
	lgr logger.Logger
}

// NewLeveledLogger creates a retryablehttp.LeveledLogger from a go-logger.Logger.
func NewLeveledLogger(lgr logger.Logger) retryablehttp.LeveledLogger {
	return &leveledLoggerAdapter{lgr: lgr}
}

func (l *leveledLoggerAdapter) Error(msg string, keysAndValues ...interface{}) {
	l.lgr.WithFields(keysAndValues...).Error(msg)
}

func (l *leveledLoggerAdapter) Warn(msg string, keysAndValues ...interface{}) {
	l.lgr.WithFields(keysAndValues...).Warn(msg)
}

func (l *leveledLoggerAdapter) Info(msg string, keysAndValues ...interface{}) {
	l.lgr.WithFields(keysAndValues...).Info(msg)
}

func (l *leveledLoggerAdapter) Debug(msg string, keysAndValues ...interface{}) {
	l.lgr.WithFields(keysAndValues...).Debug(msg)
}
