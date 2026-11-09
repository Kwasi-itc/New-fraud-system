//go:build linux

package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestVerifierCancelsChildProcessGroup(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	c := exec.CommandContext(ctx, "sh", "-c", "sleep 30 & echo $!; wait")
	if e := configureTestProcess(c); e != nil {
		t.Fatal(e)
	}
	var output bytes.Buffer
	c.Stdout = &output
	started := time.Now()
	if e := c.Run(); e == nil {
		t.Fatal("expected cancellation")
	}
	if time.Since(started) > 3*time.Second {
		t.Fatal("cancellation did not join")
	}
	pid, e := strconv.Atoi(strings.TrimSpace(output.String()))
	if e != nil {
		t.Fatal(e)
	}
	// A minimal container PID 1 may retain a terminated zombie; it is not running.
	data, e := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
	// /proc can return ESRCH when the child is reaped between opening and
	// reading stat. Both disappearance errors prove it is no longer running.
	if os.IsNotExist(e) || errors.Is(e, syscall.ESRCH) {
		return
	}
	if e != nil {
		t.Fatal(e)
	}
	fields := strings.Fields(string(data))
	if len(fields) < 3 || fields[2] != "Z" {
		t.Fatal("child still running", string(data))
	}
}
