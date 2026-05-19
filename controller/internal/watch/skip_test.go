package watch

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestSkipControllerCancelsActiveOperationContexts(t *testing.T) {
	controller := NewSkipController()
	ctx, finish := controller.operationContext(context.Background())
	defer finish()

	controller.Skip()

	select {
	case <-ctx.Done():
	case <-time.After(time.Second):
		t.Fatal("operation context was not canceled")
	}
	if !errors.Is(context.Cause(ctx), ErrSkipRequested) {
		t.Fatalf("context cause = %v, want ErrSkipRequested", context.Cause(ctx))
	}
	if got := controller.Requests(); got != 1 {
		t.Fatalf("skip requests = %d, want 1", got)
	}
}
