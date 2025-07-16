package logger

import (
	"os"
	"strings"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

var Log *zap.SugaredLogger

func init() {

	levelStr := strings.ToLower(os.Getenv("LOG_LEVEL")) // "debug", "info", etc.
	var level zapcore.Level
	switch levelStr {
	case "debug":
		level = zapcore.DebugLevel
	case "warn":
		level = zapcore.WarnLevel
	case "error":
		level = zapcore.ErrorLevel
	default:
		level = zapcore.InfoLevel // default
	}
	config := zap.NewProductionConfig()
	config.Level = zap.NewAtomicLevelAt(level)

	raw, err := config.Build()
	if err != nil {
		panic("failed to build zap logger: " + err.Error())
	}
	Log = raw.Sugar()
}
