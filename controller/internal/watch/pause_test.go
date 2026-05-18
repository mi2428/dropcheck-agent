package watch

import (
	"context"
	"testing"
	"time"
)

func TestPauseControllerWaitsUntilResume(t *testing.T) {
	controller := NewPauseController()
	controller.Pause()
	if !controller.Paused() {
		t.Fatal("controller should report paused")
	}

	done := make(chan error, 1)
	go func() {
		done <- controller.Wait(context.Background())
	}()

	select {
	case err := <-done:
		t.Fatalf("Wait returned while paused: %v", err)
	case <-time.After(10 * time.Millisecond):
	}

	controller.Resume()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Wait returned error after resume: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Wait did not return after resume")
	}
	if controller.Paused() {
		t.Fatal("controller should report resumed")
	}
}
