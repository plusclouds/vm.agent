// Package grpc provides the gRPC server foundation for the PlusClouds agent.
// Phase 1 establishes the server lifecycle, interceptors, and authentication
// infrastructure. Concrete service implementations will be added in Phase 2
// when the protobuf definitions are finalised.
package grpc

import (
	"context"
	"fmt"
	"net"

	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	"github.com/plusclouds/ubuntu-agent/internal/config"
)

// Server wraps a gRPC server with lifecycle management.
type Server struct {
	cfg        *config.Config
	logger     *zap.Logger
	grpcServer *grpc.Server
}

// New creates a gRPC Server with logging and authentication interceptors.
func New(cfg *config.Config, logger *zap.Logger) *Server {
	s := &Server{
		cfg:    cfg,
		logger: logger,
	}

	s.grpcServer = grpc.NewServer(
		grpc.ChainUnaryInterceptor(
			s.loggingInterceptor,
			s.authInterceptor,
		),
	)

	// Phase 2: register concrete service implementations here.
	// Example: agentpb.RegisterAgentServiceServer(s.grpcServer, &AgentServiceImpl{})

	return s
}

// Start begins listening on the configured gRPC port.
// It blocks until the listener encounters an unrecoverable error.
// Call Stop() to initiate a graceful shutdown.
func (s *Server) Start(ctx context.Context) error {
	addr := fmt.Sprintf(":%d", s.cfg.Server.GRPC.Port)
	lis, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("gRPC listen on %s: %w", addr, err)
	}

	s.logger.Info("gRPC server listening", zap.String("addr", addr))

	errCh := make(chan error, 1)
	go func() {
		if err := s.grpcServer.Serve(lis); err != nil {
			errCh <- err
		}
		close(errCh)
	}()

	select {
	case <-ctx.Done():
		s.logger.Info("gRPC server shutting down (context cancelled)")
		s.Stop()
		return nil
	case err := <-errCh:
		return err
	}
}

// Stop gracefully stops the gRPC server, waiting for in-flight RPCs to complete.
func (s *Server) Stop() {
	s.logger.Info("gRPC server stopping")
	s.grpcServer.GracefulStop()
	s.logger.Info("gRPC server stopped")
}

// loggingInterceptor is a unary server interceptor that logs every RPC call
// along with its result code and any error.
func (s *Server) loggingInterceptor(
	ctx context.Context,
	req interface{},
	info *grpc.UnaryServerInfo,
	handler grpc.UnaryHandler,
) (interface{}, error) {
	resp, err := handler(ctx, req)
	code := codes.OK
	if err != nil {
		code = status.Code(err)
	}

	s.logger.Info("grpc call",
		zap.String("method", info.FullMethod),
		zap.Stringer("code", code),
		zap.Error(err),
	)
	return resp, err
}

// authInterceptor is a unary server interceptor that validates the API key
// supplied in the gRPC metadata under the "x-api-key" key.
// Unauthenticated calls receive an UNAUTHENTICATED status code.
func (s *Server) authInterceptor(
	ctx context.Context,
	req interface{},
	_ *grpc.UnaryServerInfo,
	handler grpc.UnaryHandler,
) (interface{}, error) {
	apiKey := s.cfg.Auth.APIKey
	if apiKey == "" {
		// Agent not configured; reject all calls to prevent open access.
		return nil, status.Error(codes.Unauthenticated, "agent API key not configured")
	}

	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return nil, status.Error(codes.Unauthenticated, "missing metadata")
	}

	vals := md.Get("x-api-key")
	if len(vals) == 0 || vals[0] == "" {
		return nil, status.Error(codes.Unauthenticated, "missing x-api-key metadata")
	}

	if vals[0] != apiKey {
		return nil, status.Error(codes.Unauthenticated, "invalid API key")
	}

	return handler(ctx, req)
}
