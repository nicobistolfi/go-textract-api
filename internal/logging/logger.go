package logging

import (
	"log/slog"
	"os"
)

// InitLogger initializes structured logging
func InitLogger() {
	// Create a JSON handler for structured logging
	opts := &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}

	// Determine output stream - AWS Lambda requires stderr for CloudWatch logs
	output := os.Stdout
	isLambda := os.Getenv("AWS_LAMBDA_FUNCTION_NAME") != ""
	if isLambda {
		output = os.Stderr
	}

	// Use JSON format for production/Lambda, text for development
	var handler slog.Handler
	var format string
	if os.Getenv("APP_ENV") == "production" || isLambda {
		handler = slog.NewJSONHandler(output, opts)
		format = "json"
	} else {
		handler = slog.NewTextHandler(output, opts)
		format = "text"
	}

	logger := slog.New(handler)
	slog.SetDefault(logger)

	slog.Info("Logger initialized",
		"level", opts.Level,
		"format", format,
		"output", func() string {
			if output == os.Stderr {
				return "stderr"
			}
			return "stdout"
		}(),
		"is_lambda", isLambda)
}
