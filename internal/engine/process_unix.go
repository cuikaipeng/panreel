//go:build !windows

package engine

import "os/exec"

func configureProcess(cmd *exec.Cmd) {}
