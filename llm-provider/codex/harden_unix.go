//go:build unix

package codex

import (
	"os/exec"
	"syscall"
	"time"
)

// harden makes a killed codex process not orphan its descendants past the merge.
// codex spawns children (web_search, tool subprocesses); by default
// exec.CommandContext SIGKILLs only the direct process on context cancellation,
// leaving grandchildren running. Setpgid puts codex in its own process group and
// Cancel SIGKILLs the WHOLE group, so when the fan-out's wall-clock deadline
// fires everything under codex dies together. WaitDelay bounds cmd.Wait() so a
// child still holding the stdout pipe open can't wedge the fan-out after the kill.
//
// All three only bite on cancellation; a normal codex run is unaffected.
func harden(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.WaitDelay = 10 * time.Second
	cmd.Cancel = func() error {
		if cmd.Process != nil {
			// Negative pid targets the process group led by codex.
			_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		}
		return nil
	}
}
