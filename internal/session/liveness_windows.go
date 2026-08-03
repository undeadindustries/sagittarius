//go:build windows

package session

import (
	"errors"
	"os"
	"syscall"
	"unsafe"
)

// errWouldBlock is returned by lockExclusive when the lock is held by another
// process. It is wrapped so callers can match it with errors.Is.
var errWouldBlock = errors.New("liveness: lock held by another process")

var (
	kernel32          = syscall.NewLazyDLL("kernel32.dll")
	procLockFileEx    = kernel32.NewProc("LockFileEx")
	procUnlockFileEx  = kernel32.NewProc("UnlockFileEx")
	errLockViolation  = syscall.Errno(33) // ERROR_LOCK_VIOLATION
	errIoPending      = syscall.Errno(997)
	lockExclusiveFlag = 0x00000002 // LOCKFILE_EXCLUSIVE_LOCK
	lockFailImmediate = 0x00000001 // LOCKFILE_FAIL_IMMEDIATELY
)

// lockExclusive takes a non-blocking exclusive byte-range lock on f via
// LockFileEx. It returns errWouldBlock when another live process holds the
// lock, or the underlying error otherwise.
func lockExclusive(f *os.File) error {
	var ol syscall.Overlapped
	r, _, err := procLockFileEx.Call(
		f.Fd(),
		uintptr(lockExclusiveFlag|lockFailImmediate),
		0,
		1, 0, // lock 1 byte at offset 0
		uintptr(unsafe.Pointer(&ol)),
	)
	if r != 0 {
		return nil
	}
	if errors.Is(err, errLockViolation) || errors.Is(err, errIoPending) {
		return errWouldBlock
	}
	if err == syscall.Errno(0) {
		return errWouldBlock
	}
	return err
}

// unlockExclusive releases the byte-range lock held on f.
func unlockExclusive(f *os.File) error {
	var ol syscall.Overlapped
	r, _, err := procUnlockFileEx.Call(
		f.Fd(),
		0,
		1, 0,
		uintptr(unsafe.Pointer(&ol)),
	)
	if r != 0 {
		return nil
	}
	return err
}
