package main

import (
	"context"
	"os"
	"testing"
	"time"
)

func TestShutdownContextCancelsOnFirstSignalAndForcesOnSecond(t *testing.T) {
	t.Parallel()

	var notifications chan<- os.Signal
	forced := make(chan int, 1)
	dependencies := shutdownSignalDependencies{
		notify: func(channel chan<- os.Signal, signals ...os.Signal) {
			if len(signals) == 0 {
				t.Fatal("no shutdown signals registered")
			}
			notifications = channel
		},
		stop: func(chan<- os.Signal) {},
		forceExit: func(code int) {
			forced <- code
		},
	}

	ctx, stop := newShutdownContext(context.Background(), dependencies)
	t.Cleanup(stop)
	notifications <- testSignal("first")
	select {
	case <-ctx.Done():
	case <-time.After(time.Second):
		t.Fatal("first signal did not cancel shutdown context")
	}

	notifications <- testSignal("second")
	if code := receiveSignalTest(t, forced); code != 1 {
		t.Fatalf("forced exit code = %d, want 1", code)
	}
}

func TestShutdownContextSupportsExternalCancellation(t *testing.T) {
	t.Parallel()

	parent, cancelParent := context.WithCancel(context.Background())
	forced := make(chan int, 1)
	ctx, stop := newShutdownContext(parent, shutdownSignalDependencies{
		notify: func(chan<- os.Signal, ...os.Signal) {},
		stop:   func(chan<- os.Signal) {},
		forceExit: func(code int) {
			forced <- code
		},
	})
	t.Cleanup(stop)

	cancelParent()
	select {
	case <-ctx.Done():
	case <-time.After(time.Second):
		t.Fatal("parent cancellation did not cancel shutdown context")
	}
	select {
	case code := <-forced:
		t.Fatalf("external cancellation forced exit with code %d", code)
	case <-time.After(25 * time.Millisecond):
	}
}

func receiveSignalTest[T any](t *testing.T, channel <-chan T) T {
	t.Helper()
	select {
	case value := <-channel:
		return value
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for signal test event")
		var zero T
		return zero
	}
}

type testSignal string

func (signal testSignal) String() string {
	return string(signal)
}

func (testSignal) Signal() {}
