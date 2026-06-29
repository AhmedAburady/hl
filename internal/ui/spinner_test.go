package ui

import (
	"context"
	"errors"
	"testing"
)

func TestWithSpinnerRunsFnAndReturnsError(t *testing.T) {
	wantErr := errors.New("boom")
	ran := false
	var gotCtx context.Context
	err := WithSpinner(context.Background(), "working", func(ctx context.Context) error {
		ran = true
		gotCtx = ctx
		return wantErr
	})
	if !ran {
		t.Fatal("fn did not run")
	}
	if gotCtx == nil {
		t.Error("fn received a nil context")
	}
	if !errors.Is(err, wantErr) {
		t.Errorf("got %v, want %v", err, wantErr)
	}
}

func TestWithSpinnerNilOnSuccess(t *testing.T) {
	if err := WithSpinner(context.Background(), "ok", func(context.Context) error { return nil }); err != nil {
		t.Errorf("got %v, want nil", err)
	}
}
