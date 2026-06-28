package main

import (
	"context"
	"log/slog"
	"os"
	"runtime/debug"

	"github.com/AhmedAburady/hl/cmd"
	"github.com/AhmedAburady/hl/internal/ui"
	"github.com/charmbracelet/fang"
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
	slog.SetDefault(slog.New(ui.NewLogHandler(os.Stderr, slog.LevelInfo)))
	if err := fang.Execute(
		context.Background(),
		cmd.Root(),
		fang.WithVersion(resolveVersion()),
		fang.WithNotifySignal(os.Interrupt),
	); err != nil {
		os.Exit(1)
	}
}
