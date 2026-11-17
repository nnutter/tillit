package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"charm.land/fang/v2"

	"github.com/nnutter/spor/internal/cli"
	"github.com/nnutter/spor/internal/daemon"
)

// version is the spor release. Set it with -X at build time.
var version = "0.2.0"

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if len(os.Args) >= 3 && supervisorRequested() {
		if err := daemon.Supervise(os.Args[2], autoRequested(), os.NewFile(3, "startup")); err != nil {
			_, _ = fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}
	if len(os.Args) >= 3 && workerRequested() {
		if err := daemon.Worker(ctx, os.Args[2], autoRequested(), os.NewFile(3, "ready"), os.NewFile(5, "supervisor")); err != nil {
			_, _ = fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}
	if err := fang.Execute(ctx, cli.New(version), fang.WithVersion(version)); err != nil {
		os.Exit(1)
	}
}

func supervisorRequested() bool {
	return os.Args[1] == "__spor_supervisor"
}

func workerRequested() bool {
	return os.Args[1] == "__spor_worker"
}

func autoRequested() bool {
	return hasAutoArg() && autoArg()
}

func hasAutoArg() bool {
	return len(os.Args) > 3
}

func autoArg() bool {
	return os.Args[3] == "auto"
}
