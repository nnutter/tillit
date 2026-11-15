package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"charm.land/fang/v2"

	"github.com/nnutter/spor/internal/cli"
)

// version is the spor release. Set it with -X at build time.
var version = "0.1.0"

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := fang.Execute(ctx, cli.New(version), fang.WithVersion(version)); err != nil {
		os.Exit(1)
	}
}
