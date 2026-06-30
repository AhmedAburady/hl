package main

import (
	"context"
	"errors"
	"io"
	"os"
	"runtime/debug"

	"charm.land/fang/v2"
	"github.com/AhmedAburady/hl/cmd"
)

var version = "dev"

func resolveVersion() string {
	if version != "dev" {
		return version
	}
	if info, ok := debug.ReadBuildInfo(); ok && info.Main.Version != "" && info.Main.Version != "(devel)" {
		return info.Main.Version
	}
	return "dev"
}

func main() {
	if err := fang.Execute(
		context.Background(),
		cmd.Root(),
		fang.WithVersion(resolveVersion()),
		fang.WithNotifySignal(os.Interrupt),
		// Commands that already rendered their failure in the styled UI return
		// cmd.ErrReported; swallow it here so fang does not print a second,
		// raw error box on top of the clean message.
		fang.WithErrorHandler(func(w io.Writer, styles fang.Styles, err error) {
			if errors.Is(err, cmd.ErrReported) {
				return
			}
			fang.DefaultErrorHandler(w, styles, err)
		}),
	); err != nil {
		os.Exit(1)
	}
}
