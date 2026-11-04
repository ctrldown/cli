//go:build !windows

package supervisor

import (
	"os"
	"os/exec"
	"syscall"
	"time"
)

// ProcessGroup handles PGID isolation on POSIX systems.
type ProcessGroup struct{}

// NewProcessGroup initializes a POSIX process group manager.
func NewProcessGroup() (*ProcessGroup, error) {
	return &ProcessGroup{}, nil
}

// PrepareCmd sets Setpgid to create an isolated process group.
func (pg *ProcessGroup) PrepareCmd(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setpgid: true,
	}
}

// Attach is a no-op on POSIX since Setpgid is prepared before starting.
func (pg *ProcessGroup) Attach(cmd *exec.Cmd) error {
	return nil
}

// Kill terminates the entire process group.
func (pg *ProcessGroup) Kill(process *os.Process) error {
	if process == nil {
		return nil
	}
	pgid, err := syscall.Getpgid(process.Pid)
	if err == nil {
		_ = syscall.Kill(-pgid, syscall.SIGTERM)
		time.Sleep(100 * time.Millisecond)
		_ = syscall.Kill(-pgid, syscall.SIGKILL)
	} else {
		_ = process.Kill()
	}
	return nil
}
