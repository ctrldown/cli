//go:build windows

package supervisor

import (
	"fmt"
	"os"
	"os/exec"
	"syscall"
	"unsafe"
)

var (
	kernel32                     = syscall.NewLazyDLL("kernel32.dll")
	procCreateJobObjectW         = kernel32.NewProc("CreateJobObjectW")
	procSetInformationJobObject  = kernel32.NewProc("SetInformationJobObject")
	procAssignProcessToJobObject = kernel32.NewProc("AssignProcessToJobObject")
	procCloseHandle              = kernel32.NewProc("CloseHandle")
)

const (
	jobObjectExtendedLimitInformation = 9
	jobObjectLimitKillOnJobClose      = 0x00002000
	processAllAccess                  = 0x1F0FFF
)

type ioCounters struct {
	ReadOperationCount  uint64
	WriteOperationCount uint64
	OtherOperationCount uint64
	ReadTransferCount   uint64
	WriteTransferCount  uint64
	OtherTransferCount  uint64
}

type jobObjectBasicLimitInformation struct {
	PerProcessUserTimeLimit int64
	PerJobUserTimeLimit     int64
	LimitFlags              uint32
	MinimumWorkingSetSize   uintptr
	MaximumWorkingSetSize   uintptr
	ActiveProcessLimit      uint32
	Affinity                uintptr
	PriorityClass           uint32
	SchedulingClass         uint32
}

type jobObjectExtendedLimitInformationStruct struct {
	BasicLimitInformation jobObjectBasicLimitInformation
	IoInfo                ioCounters
	ProcessMemoryLimit    uintptr
	JobMemoryLimit        uintptr
	PeakProcessMemoryUsed uintptr
	PeakJobMemoryUsed     uintptr
}

// ProcessGroup isolates child processes so they can be cascaded killed cleanly.
type ProcessGroup struct {
	jobHandle syscall.Handle
}

// NewProcessGroup initializes a Windows Job Object configured to kill all processes on close.
func NewProcessGroup() (*ProcessGroup, error) {
	h, _, err := procCreateJobObjectW.Call(0, 0)
	if h == 0 {
		return nil, fmt.Errorf("failed to create job object: %w", err)
	}

	info := jobObjectExtendedLimitInformationStruct{
		BasicLimitInformation: jobObjectBasicLimitInformation{
			LimitFlags: jobObjectLimitKillOnJobClose,
		},
	}

	ret, _, err := procSetInformationJobObject.Call(
		h,
		uintptr(jobObjectExtendedLimitInformation),
		uintptr(unsafe.Pointer(&info)),
		uintptr(unsafe.Sizeof(info)),
	)
	if ret == 0 {
		procCloseHandle.Call(h)
		return nil, fmt.Errorf("failed to set job object limit flags: %w", err)
	}

	return &ProcessGroup{jobHandle: syscall.Handle(h)}, nil
}

// Attach attaches a newly created command/process to this Windows Job Object.
func (pg *ProcessGroup) Attach(cmd *exec.Cmd) error {
	if pg == nil || pg.jobHandle == 0 || cmd.Process == nil {
		return nil
	}
	handle, err := syscall.OpenProcess(processAllAccess, false, uint32(cmd.Process.Pid))
	if err != nil {
		return fmt.Errorf("failed to open process for job attachment: %w", err)
	}
	defer syscall.CloseHandle(handle)

	ret, _, callErr := procAssignProcessToJobObject.Call(uintptr(pg.jobHandle), uintptr(handle))
	if ret == 0 {
		return fmt.Errorf("failed to assign process to job object: %w", callErr)
	}
	return nil
}

// PrepareCmd sets up any OS-specific attributes prior to process launch.
func (pg *ProcessGroup) PrepareCmd(cmd *exec.Cmd) {
	// Handled post-Start via Attach on Windows
}

// Kill terminates the entire job object tree.
func (pg *ProcessGroup) Kill(process *os.Process) error {
	if process != nil {
		_ = process.Kill()
	}
	if pg != nil && pg.jobHandle != 0 {
		procCloseHandle.Call(uintptr(pg.jobHandle))
		pg.jobHandle = 0
	}
	return nil
}
