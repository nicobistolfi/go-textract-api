package server

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/nicobistolfi/go-textract-api/internal/handlers"
	"github.com/nicobistolfi/go-textract-api/internal/middleware"
)

type Server struct {
	httpServer *http.Server
}

func New(addr string) *Server {
	mux := http.NewServeMux()

	// Health endpoint - accessible without authentication
	mux.HandleFunc("/health", handlers.HealthHandler)

	// Text extraction endpoint - requires authentication
	mux.HandleFunc("/extract", middleware.AuthMiddleware(handlers.TextractHandler))

	// Wrap the entire mux with logging
	handler := loggingMiddleware(mux)

	return &Server{
		httpServer: &http.Server{
			Addr:         addr,
			Handler:      handler,
			ReadTimeout:  15 * time.Second, // Increased for file uploads
			WriteTimeout: 15 * time.Second, // Increased for PDF processing
			IdleTimeout:  60 * time.Second,
		},
	}
}

func (s *Server) Start() error {
	slog.Info("Starting server", "addr", s.httpServer.Addr)

	// Start server in a goroutine
	errChan := make(chan error, 1)
	go func() {
		if err := s.httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errChan <- err
		}
	}()

	// Wait for interrupt signal to gracefully shutdown the server
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	select {
	case err := <-errChan:
		return err
	case <-quit:
		slog.Info("Received shutdown signal, shutting down server...")
		return s.Shutdown()
	}
}

func (s *Server) Shutdown() error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := s.httpServer.Shutdown(ctx); err != nil {
		return err
	}

	slog.Info("Server gracefully stopped")
	return nil
}

func loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		// Create a custom response writer to capture the status code
		rw := &responseWriter{ResponseWriter: w, statusCode: http.StatusOK}

		next.ServeHTTP(rw, r)

		duration := time.Since(start)
		slog.Info("HTTP request completed",
			"method", r.Method,
			"path", r.URL.Path,
			"remote_addr", r.RemoteAddr,
			"status_code", rw.statusCode,
			"duration_ms", duration.Milliseconds(),
			"user_agent", r.Header.Get("User-Agent"),
			"content_length", r.ContentLength,
		)
	})
}

type responseWriter struct {
	http.ResponseWriter
	statusCode int
}

func (rw *responseWriter) WriteHeader(code int) {
	rw.statusCode = code
	rw.ResponseWriter.WriteHeader(code)
}
