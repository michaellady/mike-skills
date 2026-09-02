//go:build !unix

package muse

import (
	"os/exec"
	"time"
)

// harden (non-unix): process-group signaling is unix-only, so bound cmd.Wait()
// with WaitDelay and leave exec.CommandContext's default single-process kill in
// place.
func harden(cmd *exec.Cmd) {
	cmd.WaitDelay = 10 * time.Second
}
