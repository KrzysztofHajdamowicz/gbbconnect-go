package driver

import (
	"context"
	"time"
)

// Clock supplies time and cancellable waiting to the executor.
type Clock interface {
	Now() time.Time
	Wait(ctx context.Context, duration time.Duration) error
}

type systemClock struct{}

func (systemClock) Now() time.Time {
	return time.Now()
}

func (systemClock) Wait(ctx context.Context, duration time.Duration) error {
	if duration <= 0 {
		return nil
	}

	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

type executor struct {
	token    chan struct{}
	clock    Clock
	lastSend time.Time
}

func newExecutor(clock Clock) *executor {
	if clock == nil {
		clock = systemClock{}
	}

	executor := &executor{
		token: make(chan struct{}, 1),
		clock: clock,
	}
	executor.token <- struct{}{}
	return executor
}

func (executor *executor) execute(
	ctx context.Context,
	minimumDelay time.Duration,
	recordSend bool,
	send func() ([]byte, error),
) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	select {
	case <-executor.token:
		defer func() {
			executor.token <- struct{}{}
		}()
	case <-ctx.Done():
		return nil, ctx.Err()
	}

	if recordSend && !executor.lastSend.IsZero() {
		elapsed := executor.clock.Now().Sub(executor.lastSend)
		if remaining := minimumDelay - elapsed; remaining > 0 {
			if err := executor.clock.Wait(ctx, remaining); err != nil {
				return nil, err
			}
		}
	}

	response, err := send()
	if err == nil && recordSend {
		executor.lastSend = executor.clock.Now()
	}
	return response, err
}
