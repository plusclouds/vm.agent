//go:build linux

package main

import (
	"context"

	"github.com/coreos/go-systemd/v22/dbus"
	"go.uber.org/zap"

	"github.com/plusclouds/ubuntu-agent/internal/modules/cron"
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

// newCronLister connects to systemd D-Bus (separately from newServiceManager,
// its own lightweight connection) and returns a Lister. The returned cleanup
// function closes the connection; call it on shutdown. Cron-style file
// listing works even if this connection fails — only systemd timer listing
// is affected.
func newCronLister(ctx context.Context, logger *zap.Logger) (cron.Lister, func()) {
	conn, err := dbus.NewSystemdConnectionContext(ctx)
	if err != nil {
		logger.Warn("could not connect to systemd D-Bus; timer listing unavailable (cron-style files still work)",
			zap.Error(err))
		return cron.New(nil, logger), func() {}
	}
	return cron.New(conn, logger), func() { conn.Close() }
}
