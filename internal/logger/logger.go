package logger

import (
	"log"
	"os"

	"go.uber.org/zap"
)

var logger *zap.Logger

func init() {
	var err error

	isServerMode := false
	for _, arg := range os.Args[1:] {
		if arg == "mcp" || arg == "http" {
			isServerMode = true
			break
		}
	}

	if isServerMode {
		logger, err = zap.NewProduction()
	} else {
		config := zap.NewDevelopmentConfig()
		config.Level = zap.NewAtomicLevelAt(zap.WarnLevel)
		logger, err = config.Build()
	}

	if err != nil {
		log.Fatalf("Failed to initialize zap logger: %v", err)
	}
}

func GetLogger() *zap.Logger {
	return logger
}
