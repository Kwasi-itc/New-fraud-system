//go:build linux

package main

import (
	"os/exec"
	"syscall"
	"time"
)

// The disposable verifier must cancel the test binary and its API/worker children,
// not only the go command that launched them.
func configureTestProcess(c *exec.Cmd) error {
	c.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	c.Cancel = func() error { return syscall.Kill(-c.Process.Pid, syscall.SIGKILL) }
	c.WaitDelay = 5 * time.Second
	return nil
}
