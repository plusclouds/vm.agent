//go:build linux

package main

import (
	"context"

	"github.com/coreos/go-systemd/v22/dbus"
	"go.uber.org/zap"

	"github.com/plusclouds/ubuntu-agent/internal/modules/services"
)

// newServiceManager connects to systemd D-Bus and returns a Manager.
// The returned cleanup function closes the D-Bus connection; call it on shutdown.
func newServiceManager(ctx context.Context, logger *zap.Logger) (services.Manager, func()) {
	conn, err := dbus.NewSystemdConnectionContext(ctx)
	if err != nil {
		logger.Warn("could not connect to systemd D-Bus; service management unavailable",
			zap.Error(err))
		return services.New(nil, logger), func() {}
	}
	logger.Info("connected to systemd D-Bus")
	return services.New(conn, logger), func() { conn.Close() }
}
