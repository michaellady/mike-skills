//go:build unix

package muse

import (
	"os/exec"
	"syscall"
	"time"
)

// harden makes a killed muse process not orphan its descendants past the merge.
// Same shape as the codex provider's harden: muse spawns tool subprocesses;
// exec.CommandContext SIGKILLs only the direct process on cancellation, leaving
// grandchildren running. Setpgid puts muse in its own process group and Cancel
// SIGKILLs the WHOLE group; WaitDelay bounds cmd.Wait() so a child still
// holding the stdout pipe open can't wedge the fan-out after the kill.
//
// All three only bite on cancellation; a normal muse run is unaffected.
func harden(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.WaitDelay = 10 * time.Second
	cmd.Cancel = func() error {
		if cmd.Process != nil {
			// Negative pid targets the process group led by muse.
			_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		}
		return nil
	}
}
