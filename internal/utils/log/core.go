package log

import (
	"context"
	"io"
	"log/slog"
	"os"

	"gopkg.in/natefinch/lumberjack.v2"
)

// contextHandler wraps slog.Handler and adds trace and identity from context
type contextHandler struct {
	slog.Handler
}

func (h *contextHandler) Handle(ctx context.Context, r slog.Record) error {
	// Add trace context if available
	if tc, ok := TraceFromContext(ctx); ok {
		r.AddAttrs(
			slog.String("trace_id", tc.TraceID),
			slog.String("span_id", tc.SpanID),
		)
	}

	// Add identity if available
	if id, ok := IdentityFromContext(ctx); ok {
		r.AddAttrs(
			slog.String("tenant_id", id.TenantID),
			slog.String("user_id", id.UserID),
			slog.String("user_type", id.UserType),
		)
	}

	return h.Handler.Handle(ctx, r)
}

const (
	defaultLogPath = "./logs"
)

var (
	configuredLogPath string
)

type Config struct {
	Filename   string
	MaxSize    int
	MaxBackups int
	MaxAge     int
	Compress   bool
}

// Init initializes the global slog logger with lumberjack rotation
func Init(logPath string) error {
	configuredLogPath = logPath

	config := Config{
		Filename:   getLogPath() + "/app.log",
		MaxSize:    100,
		MaxBackups: 30,
		MaxAge:     30,
		Compress:   true,
	}

	return initLogger(config)
}

func initLogger(config Config) error {
	lumberjackLogger := &lumberjack.Logger{
		Filename:   config.Filename,
		MaxSize:    config.MaxSize,
		MaxBackups: config.MaxBackups,
		MaxAge:     config.MaxAge,
		Compress:   config.Compress,
		LocalTime:  true,
	}

	writer := io.MultiWriter(lumberjackLogger, os.Stdout)

	jsonHandler := slog.NewJSONHandler(writer, &slog.HandlerOptions{
		AddSource: false,
		Level:     slog.LevelDebug,
	})

	// Wrap with contextHandler to automatically extract trace and identity
	handler := &contextHandler{jsonHandler}

	slog.SetDefault(slog.New(handler))
	return nil
}

func getLogPath() string {
	if configuredLogPath != "" {
		return configuredLogPath
	}
	return defaultLogPath
}

func init() {
	config := Config{
		Filename:   getLogPath() + "/app.log",
		MaxSize:    100,
		MaxBackups: 30,
		MaxAge:     30,
		Compress:   true,
	}

	if err := initLogger(config); err != nil {
		panic(err)
	}
}
