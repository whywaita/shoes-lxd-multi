package main

import (
	"log/slog"
	"os"

	"github.com/whywaita/shoes-lxd-multi/pool-agent/cmd"
)

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stderr, nil)))
	cmd.Execute()
}
