package logger

import (
	"log"
	"log/slog"
	"strings"
)

type Logf func(format string, args ...any)

type StdLogger struct {
	Level slog.Level
	Logf  Logf
}

var stdLogger = StdLogger{
	Level: slog.LevelInfo,
	Logf:  log.Printf,
}

func SetUp(level slog.Level, logf Logf) {
	stdLogger.Level = level
	stdLogger.Logf = logf
}

func Warnf(format string, args ...any) {
	if stdLogger.Level > slog.LevelWarn {
		return
	}
	stdLogger.Logf(format, args...)
}

func Infof(format string, args ...any) {
	if stdLogger.Level > slog.LevelInfo {
		return
	}
	stdLogger.Logf(format, args...)
}

func Debugf(format string, args ...any) {
	if stdLogger.Level > slog.LevelDebug {
		return
	}
	stdLogger.Logf(format, args...)
}

func Error(args ...any) {
	if stdLogger.Level > slog.LevelError {
		return
	}
	fmtStr := strings.Repeat("%v ", len(args))
	stdLogger.Logf(fmtStr, args...)
}

func Errorf(format string, args ...any) {
	if stdLogger.Level > slog.LevelError {
		return
	}
	stdLogger.Logf(format, args...)
}
