package ui

import (
	"context"
	"fmt"
	"os"
	"time"

	"golang.org/x/term"
)

var spinnerFrames = []rune("⠋⠙⠹⠸⠼⠴⠦⠧⠇⠏")

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

	fmt.Fprint(os.Stderr, "\x1b[?25l")
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	i := 0
	draw := func() {
		fmt.Fprintf(os.Stderr, "\r%c %s", spinnerFrames[i%len(spinnerFrames)], msg)
		i++
	}
	draw()

loop:
	for {
		select {
		case <-done:
			break loop
		case <-ctx.Done():
			break loop
		case <-ticker.C:
			draw()
		}
	}

	fmt.Fprint(os.Stderr, "\r\x1b[K\x1b[?25h")
	<-done
	return opErr
}
