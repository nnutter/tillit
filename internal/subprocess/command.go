// Package subprocess starts cancellable commands in their own process groups.
package subprocess

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"syscall"
)

// CommandContext stops the command and its process group when ctx is canceled.
func CommandContext(ctx context.Context, name string, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		if errors.Is(err, syscall.ESRCH) {
			return os.ErrProcessDone
		}
		return err
	}
	return cmd
}
