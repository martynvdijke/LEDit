package logging

import (
	"log/slog"
	"strings"
)

// ParseLevel converts a string level name to slog.Level.
// Returns slog.LevelWarn for unknown values.
func ParseLevel(s string) slog.Level {
	switch strings.ToLower(s) {
	case "trace":
		return slog.Level(-8)
	case "debug":
		return slog.LevelDebug
	case "info":
		return slog.LevelInfo
	case "warn":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	case "fatal":
		return slog.Level(12)
	default:
		return slog.LevelWarn
	}
}

// LevelName returns the string name for a slog.Level.
func LevelName(l slog.Level) string {
	if l < slog.LevelDebug {
		return "trace"
	}
	switch l {
	case slog.LevelDebug:
		return "debug"
	case slog.LevelInfo:
		return "info"
	case slog.LevelWarn:
		return "warn"
	case slog.LevelError:
		return "error"
	default:
		return "fatal"
	}
}

// ValidLevels returns all valid level names.
func ValidLevels() []string {
	return []string{"trace", "debug", "info", "warn", "error", "fatal"}
}
