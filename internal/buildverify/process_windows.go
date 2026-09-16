//go:build windows

package buildverify

import (
	"fmt"
	"os/exec"
	"strconv"
	"syscall"
)

func configureHiddenProcess(command *exec.Cmd) {
	command.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
	command.Cancel = func() error {
		if command.Process == nil {
			return nil
		}
		killer := exec.Command("taskkill", "/PID", strconv.Itoa(command.Process.Pid), "/T", "/F")
		killer.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
		if output, err := killer.CombinedOutput(); err != nil && command.ProcessState == nil {
			return fmt.Errorf("stop build process tree: %w: %s", err, output)
		}
		return nil
	}
}
