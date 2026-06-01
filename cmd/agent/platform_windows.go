//go:build windows

package main

import (
	"context"

	"go.uber.org/zap"

	"github.com/plusclouds/ubuntu-agent/internal/modules/services"
)

// newServiceManager returns the Windows stub service manager.
func newServiceManager(_ context.Context, logger *zap.Logger) (services.Manager, func()) {
	return services.New(logger), func() {}
}
