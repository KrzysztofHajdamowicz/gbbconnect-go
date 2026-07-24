package main

import (
	"context"
	"os"
	"os/signal"
)

type shutdownSignalDependencies struct {
	notify    func(chan<- os.Signal, ...os.Signal)
	stop      func(chan<- os.Signal)
	forceExit func(int)
}

func defaultShutdownSignalDependencies() shutdownSignalDependencies {
	return shutdownSignalDependencies{
		notify:    signal.Notify,
		stop:      signal.Stop,
		forceExit: os.Exit,
	}
}

func newShutdownContext(
	parent context.Context,
	dependencies shutdownSignalDependencies,
) (context.Context, func()) {
	ctx, cancel := context.WithCancel(parent)
	notifications := make(chan os.Signal, 2)
	dependencies.notify(notifications, shutdownSignals()...)
	done := make(chan struct{})

	go func() {
		select {
		case <-parent.Done():
			cancel()
			return
		case <-done:
			return
		case <-notifications:
			cancel()
		}

		select {
		case <-done:
		case <-notifications:
			dependencies.forceExit(1)
		}
	}()

	var stopped bool
	return ctx, func() {
		if stopped {
			return
		}
		stopped = true
		dependencies.stop(notifications)
		close(done)
		cancel()
	}
}
