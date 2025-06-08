package logging

import (
	"context"
	"log"
	"os"
	"strings"
)

// LogLevel represents the logging level
type LogLevel int

const (
	LevelDebug LogLevel = iota
	LevelInfo
	LevelWarn
	LevelError
)

// Logger provides structured logging with levels
type Logger struct {
	level  LogLevel
	logger *log.Logger
}

// NewLogger creates a new logger with the specified level
func NewLogger(levelStr string) *Logger {
	level := parseLogLevel(levelStr)
	return &Logger{
		level:  level,
		logger: log.New(os.Stdout, "", log.LstdFlags|log.Lshortfile),
	}
}

func parseLogLevel(levelStr string) LogLevel {
	switch strings.ToLower(levelStr) {
	case "debug":
		return LevelDebug
	case "info":
		return LevelInfo
	case "warn", "warning":
		return LevelWarn
	case "error":
		return LevelError
	default:
		return LevelInfo
	}
}

// Debug logs a debug message
func (l *Logger) Debug(msg string, fields ...interface{}) {
	if l.level <= LevelDebug {
		l.logWithLevel("DEBUG", msg, fields...)
	}
}

// Info logs an info message
func (l *Logger) Info(msg string, fields ...interface{}) {
	if l.level <= LevelInfo {
		l.logWithLevel("INFO", msg, fields...)
	}
}

// Warn logs a warning message
func (l *Logger) Warn(msg string, fields ...interface{}) {
	if l.level <= LevelWarn {
		l.logWithLevel("WARN", msg, fields...)
	}
}

// Error logs an error message
func (l *Logger) Error(msg string, fields ...interface{}) {
	if l.level <= LevelError {
		l.logWithLevel("ERROR", msg, fields...)
	}
}

// DebugCtx logs a debug message with context
func (l *Logger) DebugCtx(ctx context.Context, msg string, fields ...interface{}) {
	if l.level <= LevelDebug {
		l.logWithContext(ctx, "DEBUG", msg, fields...)
	}
}

// InfoCtx logs an info message with context
func (l *Logger) InfoCtx(ctx context.Context, msg string, fields ...interface{}) {
	if l.level <= LevelInfo {
		l.logWithContext(ctx, "INFO", msg, fields...)
	}
}

// WarnCtx logs a warning message with context
func (l *Logger) WarnCtx(ctx context.Context, msg string, fields ...interface{}) {
	if l.level <= LevelWarn {
		l.logWithContext(ctx, "WARN", msg, fields...)
	}
}

// ErrorCtx logs an error message with context
func (l *Logger) ErrorCtx(ctx context.Context, msg string, fields ...interface{}) {
	if l.level <= LevelError {
		l.logWithContext(ctx, "ERROR", msg, fields...)
	}
}

func (l *Logger) logWithLevel(level, msg string, fields ...interface{}) {
	if len(fields) > 0 {
		l.logger.Printf("[%s] %s %v", level, msg, fields)
	} else {
		l.logger.Printf("[%s] %s", level, msg)
	}
}

func (l *Logger) logWithContext(ctx context.Context, level, msg string, fields ...interface{}) {
	// Extract context values for logging
	contextFields := extractContextFields(ctx)
	allFields := append(contextFields, fields...)
	
	if len(allFields) > 0 {
		l.logger.Printf("[%s] %s %v", level, msg, allFields)
	} else {
		l.logger.Printf("[%s] %s", level, msg)
	}
}

func extractContextFields(ctx context.Context) []interface{} {
	var fields []interface{}
	
	// Extract common context values
	if sessionID := ctx.Value("session_id"); sessionID != nil {
		fields = append(fields, "session_id", sessionID)
	}
	if userID := ctx.Value("user_id"); userID != nil {
		fields = append(fields, "user_id", userID)
	}
	if channelID := ctx.Value("channel_id"); channelID != nil {
		fields = append(fields, "channel_id", channelID)
	}
	
	return fields
}

// WithContext adds fields to context for logging
func WithSessionID(ctx context.Context, sessionID string) context.Context {
	return context.WithValue(ctx, "session_id", sessionID)
}

func WithUserID(ctx context.Context, userID int64) context.Context {
	return context.WithValue(ctx, "user_id", userID)
}

func WithChannelID(ctx context.Context, channelID string) context.Context {
	return context.WithValue(ctx, "channel_id", channelID)
}

// Global logger instance
var defaultLogger *Logger

// InitGlobalLogger initializes the global logger
func InitGlobalLogger(level string) {
	defaultLogger = NewLogger(level)
}

// Global logging functions using the default logger
func Debug(msg string, fields ...interface{}) {
	if defaultLogger != nil {
		defaultLogger.Debug(msg, fields...)
	}
}

func Info(msg string, fields ...interface{}) {
	if defaultLogger != nil {
		defaultLogger.Info(msg, fields...)
	}
}

func Warn(msg string, fields ...interface{}) {
	if defaultLogger != nil {
		defaultLogger.Warn(msg, fields...)
	}
}

func Error(msg string, fields ...interface{}) {
	if defaultLogger != nil {
		defaultLogger.Error(msg, fields...)
	}
}

func DebugCtx(ctx context.Context, msg string, fields ...interface{}) {
	if defaultLogger != nil {
		defaultLogger.DebugCtx(ctx, msg, fields...)
	}
}

func InfoCtx(ctx context.Context, msg string, fields ...interface{}) {
	if defaultLogger != nil {
		defaultLogger.InfoCtx(ctx, msg, fields...)
	}
}

func WarnCtx(ctx context.Context, msg string, fields ...interface{}) {
	if defaultLogger != nil {
		defaultLogger.WarnCtx(ctx, msg, fields...)
	}
}

func ErrorCtx(ctx context.Context, msg string, fields ...interface{}) {
	if defaultLogger != nil {
		defaultLogger.ErrorCtx(ctx, msg, fields...)
	}
}