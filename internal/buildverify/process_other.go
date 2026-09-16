//go:build !windows

package buildverify

import "os/exec"

func configureHiddenProcess(_ *exec.Cmd) {}
