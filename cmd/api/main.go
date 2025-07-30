package main

import (
	"log/slog"
	"os"

	"github.com/joho/godotenv"
	"github.com/nicobistolfi/go-textract-api/internal/logging"
	"github.com/nicobistolfi/go-textract-api/internal/server"
)

func main() {
	// Load .env file if it exists (ignore error if file doesn't exist)
	_ = godotenv.Load()

	// Initialize structured logging
	logging.InitLogger()

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	addr := ":" + port
	srv := server.New(addr)

	if err := srv.Start(); err != nil {
		slog.Error("Server failed to start", "error", err)
		os.Exit(1)
	}
}
