package handlers

import (
	"encoding/json"
	"log/slog"
	"net/http"
)

type HealthResponse struct {
	Status string `json:"status"`
}

func HealthHandler(w http.ResponseWriter, r *http.Request) {
	slog.Info("Health check request",
		"method", r.Method,
		"path", r.URL.Path,
		"remote_addr", r.RemoteAddr,
		"user_agent", r.Header.Get("User-Agent"))

	if r.Method != http.MethodGet {
		slog.Warn("Invalid method for health check",
			"method", r.Method,
			"remote_addr", r.RemoteAddr)
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	response := HealthResponse{
		Status: "ok",
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(response); err != nil {
		slog.Error("Failed to encode health response", "error", err)
		return
	}

	slog.Info("Health check completed successfully",
		"remote_addr", r.RemoteAddr)
}
