package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"runtime/debug"
	"syscall"

	"charm.land/fang/v2"

	"github.com/nnutter/tillit/internal/cli"
	"github.com/nnutter/tillit/internal/daemon"
)

// version is the tillit release. Set it at build time with
// -ldflags "-X main.version=vX.Y.Z".
var version string

// resolveVersion prefers the build-time version and falls back to the module
// version embedded in the binary.
func resolveVersion() string {
	if version != "" {
		return version
	}
	if info, ok := debug.ReadBuildInfo(); ok && hasBuildVersion(info.Main.Version) {
		return info.Main.Version
	}
	return "dev"
}

func hasBuildVersion(v string) bool {
	return isTaggedVersion(v) && isReleasedVersion(v)
}

func isTaggedVersion(v string) bool {
	return v != ""
}

func isReleasedVersion(v string) bool {
	return v != "(devel)"
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if len(os.Args) >= 3 && supervisorRequested() {
		if err := daemon.Supervise(ctx, os.Args[2], os.NewFile(3, "startup")); err != nil {
			_, _ = fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}
	if len(os.Args) >= 3 && workerRequested() {
		if err := daemon.Worker(ctx, os.Args[2], os.NewFile(3, "ready"), os.NewFile(5, "supervisor")); err != nil {
			_, _ = fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}
	if err := fang.Execute(ctx, cli.New(resolveVersion()), fang.WithVersion(resolveVersion())); err != nil {
		os.Exit(1)
	}
}

func supervisorRequested() bool {
	return os.Args[1] == "__tillit_supervisor"
}

func workerRequested() bool {
	return os.Args[1] == "__tillit_worker"
}
