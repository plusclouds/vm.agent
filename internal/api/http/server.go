package http

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"go.uber.org/zap"

	"github.com/plusclouds/ubuntu-agent/internal/config"
)

const (
	readTimeout       = 15 * time.Second
	writeTimeout      = 30 * time.Second
	idleTimeout       = 60 * time.Second
	shutdownTimeout   = 10 * time.Second
)

// Server wraps net/http.Server with graceful startup and shutdown.
type Server struct {
	httpServer *http.Server
	logger     *zap.Logger
}

// New creates an HTTP server that listens on the port specified in cfg.
func New(cfg *config.Config, router http.Handler, logger *zap.Logger) *Server {
	addr := fmt.Sprintf(":%d", cfg.Server.HTTP.Port)
	return &Server{
		httpServer: &http.Server{
			Addr:         addr,
			Handler:      router,
			ReadTimeout:  readTimeout,
			WriteTimeout: writeTimeout,
			IdleTimeout:  idleTimeout,
		},
		logger: logger,
	}
}

// Start begins listening for HTTP requests. It blocks until the context is
// cancelled, at which point it initiates a graceful shutdown.
func (s *Server) Start(ctx context.Context) error {
	// Run the server in a goroutine so Start can block on context cancellation.
	errCh := make(chan error, 1)
	go func() {
		s.logger.Info("HTTP server listening", zap.String("addr", s.httpServer.Addr))
		if err := s.httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errCh <- err
		}
		close(errCh)
	}()

	select {
	case <-ctx.Done():
		s.logger.Info("HTTP server shutting down (context cancelled)")
		return s.Stop(context.Background())
	case err := <-errCh:
		return err
	}
}

// Stop performs a graceful shutdown: existing requests are given
// shutdownTimeout to complete before the server is forcibly closed.
func (s *Server) Stop(ctx context.Context) error {
	shutdownCtx, cancel := context.WithTimeout(ctx, shutdownTimeout)
	defer cancel()

	if err := s.httpServer.Shutdown(shutdownCtx); err != nil {
		s.logger.Error("HTTP server shutdown error", zap.Error(err))
		return fmt.Errorf("shutting down HTTP server: %w", err)
	}

	s.logger.Info("HTTP server stopped cleanly")
	return nil
}
