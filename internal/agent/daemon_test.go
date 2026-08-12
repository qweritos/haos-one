package agent

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

func TestSuperviseRestartsFailedComponentAndStops(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	ready := make(chan struct{})
	var calls atomic.Int32
	done := make(chan struct{})
	go func() {
		defer close(done)
		supervise(ctx, "test", func(runCtx context.Context, signalReady func()) error {
			current := calls.Add(1)
			signalReady()
			if current == 1 {
				return errors.New("first failure")
			}
			<-runCtx.Done()
			return nil
		}, ready)
	}()
	select {
	case <-ready:
	case <-time.After(time.Second):
		t.Fatal("component never became ready")
	}
	deadline := time.Now().Add(3 * time.Second)
	for calls.Load() < 2 && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if calls.Load() < 2 {
		t.Fatal("component was not restarted")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("supervisor did not stop")
	}
}
