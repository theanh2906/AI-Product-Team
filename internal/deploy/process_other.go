//go:build !windows

package deploy

import "os/exec"

func configureHiddenProcess(_ *exec.Cmd) {}
