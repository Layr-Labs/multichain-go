// Package logger provides structured logging functionality for the multichain-go application.
// This package configures and creates zap loggers with appropriate settings for
// production and development environments, including HTTP request logging middleware.
package logger

import (
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"net/http"
	"regexp"
	"time"
)

// LoggerConfig holds the configuration for logger creation.
// This configuration controls the logging level and behavior.
type LoggerConfig struct {
	// Debug enables debug-level logging when true, otherwise uses info level
	Debug bool
}

// NewLogger creates a new structured logger with the specified configuration.
// The logger is configured for production use with JSON encoding and ISO8601 timestamps.
// Debug mode can be enabled through the configuration to include debug-level logs.
//
// Parameters:
//   - cfg: The logger configuration
//   - options: Additional zap options to apply to the logger
//
// Returns:
//   - *zap.Logger: A configured zap logger instance
//   - error: An error if the logger cannot be created
func NewLogger(cfg *LoggerConfig, options ...zap.Option) (*zap.Logger, error) {
	mergedOptions := []zap.Option{
		zap.WithCaller(true),
	}
	copy(mergedOptions, options)

	c := zap.NewProductionConfig()
	c.EncoderConfig = zap.NewProductionEncoderConfig()
	c.EncoderConfig.EncodeTime = zapcore.ISO8601TimeEncoder

	if cfg.Debug {
		c.Level = zap.NewAtomicLevelAt(zap.DebugLevel)
	} else {
		c.Level = zap.NewAtomicLevelAt(zap.InfoLevel)
	}

	return c.Build(mergedOptions...)
}

// HttpLoggerMiddleware creates an HTTP middleware for request logging.
// This middleware logs HTTP requests with method, path, and duration information.
// Health and ready check endpoints are excluded from logging to reduce noise.
//
// Parameters:
//   - next: The next HTTP handler in the middleware chain
//   - l: The zap logger to use for request logging
//
// Returns:
//   - http.Handler: An HTTP handler that logs requests and calls the next handler
func HttpLoggerMiddleware(next http.Handler, l *zap.Logger) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		healthRegex := regexp.MustCompile(`v1\/health$`)
		readyRegex := regexp.MustCompile(`v1\/ready$`)

		if !healthRegex.MatchString(r.URL.Path) && !readyRegex.MatchString(r.URL.Path) {
			l.Sugar().Infow("http_request",
				zap.String("system", "http"),
				zap.String("method", r.Method),
				zap.String("path", r.URL.Path),
				zap.Duration("duration", time.Since(start)),
			)
		}
	})
}
