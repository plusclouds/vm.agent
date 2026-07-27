//go:build windows

package cron

import (
	"context"
	"fmt"

	"go.uber.org/zap"
)

// windowsLister is a stub that reports scheduled-task listing as
// unsupported. Windows Task Scheduler integration (e.g. shelling out to
// `schtasks /query`, or golang.org/x/sys/windows for a native binding) can
// replace this stub when Windows scheduled-task visibility is required.
type windowsLister struct {
	logger *zap.Logger
}

// New returns a stub Lister for Windows.
func New(logger *zap.Logger) Lister {
	logger.Info("cron: Windows Task Scheduler not yet implemented — cron.list will return unsupported")
	return &windowsLister{logger: logger}
}

func (l *windowsLister) List(_ context.Context) ([]Job, error) {
	return nil, fmt.Errorf("scheduled-task listing is not yet supported on Windows")
}
