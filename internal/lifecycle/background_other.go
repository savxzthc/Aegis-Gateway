//go:build !windows

package lifecycle

import "os/exec"

func prepareBackgroundCommand(cmd *exec.Cmd) {}
