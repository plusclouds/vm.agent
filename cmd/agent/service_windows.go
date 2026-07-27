//go:build windows

package main

import (
	"context"
	"fmt"

	"golang.org/x/sys/windows/svc"
)

// windowsServiceName must match the -Name used when registering the service
// (see scripts/install.windows.ps1).
const windowsServiceName = "PlusCloudsAgent"

// maybeRunAsService detects whether this process was launched by the
// Windows Service Control Manager rather than interactively. If so, it
// drives run() through svc.Run — which requires responding to the SCM's
// control protocol (start/stop/shutdown) within a timeout, unlike a plain
// foreground process — and returns handled=true so main() does not also
// fall through to the normal CLI path.
func maybeRunAsService() (handled bool, err error) {
	isService, err := svc.IsWindowsService()
	if err != nil {
		return false, fmt.Errorf("checking Windows service context: %w", err)
	}
	if !isService {
		return false, nil
	}
	return true, svc.Run(windowsServiceName, &windowsServiceHandler{})
}

// windowsServiceHandler adapts run()'s context-cancellation shutdown model
// to the SCM's control-request protocol: Stop/Shutdown requests cancel the
// context run() is waiting on and wait for it to return before reporting
// svc.Stopped, so systemd-style graceful shutdown (NATS drain, cleanup
// defers) still happens under SCM control too.
type windowsServiceHandler struct{}

func (h *windowsServiceHandler) Execute(_ []string, r <-chan svc.ChangeRequest, s chan<- svc.Status) (svcSpecificEC bool, exitCode uint32) {
	const accepted = svc.AcceptStop | svc.AcceptShutdown

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- run(ctx, cancel) }()

	s <- svc.Status{State: svc.StartPending}
	s <- svc.Status{State: svc.Running, Accepts: accepted}

	for {
		select {
		case <-done:
			s <- svc.Status{State: svc.Stopped}
			return false, 0

		case req := <-r:
			switch req.Cmd {
			case svc.Interrogate:
				s <- req.CurrentStatus
			case svc.Stop, svc.Shutdown:
				s <- svc.Status{State: svc.StopPending}
				cancel()
				<-done
				s <- svc.Status{State: svc.Stopped}
				return false, 0
			}
		}
	}
}
