package main

import (
	"context"
	"os"

	"github.com/AhmedAburady/hl/cmd"
	"github.com/charmbracelet/fang"
)

var version = "0.1.0"

func main() {
	if err := fang.Execute(
		context.Background(),
		cmd.Root(),
		fang.WithVersion(version),
		fang.WithNotifySignal(os.Interrupt),
	); err != nil {
		os.Exit(1)
	}
}
