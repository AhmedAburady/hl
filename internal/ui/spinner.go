package ui

import (
	"context"
	"fmt"
	"os"

	"charm.land/huh/v2/spinner"
	"golang.org/x/term"
)

func WithSpinner(ctx context.Context, msg string, fn func(context.Context) error) error {
	if !term.IsTerminal(int(os.Stderr.Fd())) {
		fmt.Fprintf(os.Stderr, "%s\n", msg)
		return fn(ctx)
	}

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	var opErr error
	done := make(chan struct{})
	go func() {
		defer close(done)
		opErr = fn(ctx)
	}()

	runErr := spinner.New().
		Type(spinner.MiniDot).
		Title(msg).
		WithOutput(os.Stderr).
		Context(ctx).
		ActionWithErr(func(c context.Context) error {
			select {
			case <-done:
				return nil
			case <-c.Done():
				return c.Err()
			}
		}).
		Run()

	if runErr != nil {
		cancel()
		<-done
		if opErr != nil {
			return opErr
		}
		return runErr
	}
	<-done
	return opErr
}
