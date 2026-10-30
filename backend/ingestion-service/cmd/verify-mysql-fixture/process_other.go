//go:build !linux

package main

import (
	"errors"
	"os/exec"
)

func configureTestProcess(*exec.Cmd) error {
	return errors.New("isolated Linux verifier required for process-tree cancellation")
}
