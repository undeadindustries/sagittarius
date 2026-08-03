//go:build !windows

package session

import (
	"errors"
	"os"
	"syscall"
)

// errWouldBlock is returned by lockExclusive when the lock is held by another
// process. It is wrapped so callers can match it with errors.Is.
var errWouldBlock = errors.New("liveness: lock held by another process")

// lockExclusive takes a non-blocking exclusive advisory lock on f. It returns
// errWouldBlock when another live process holds the lock, or the underlying
// error for any other failure (including filesystems where flock is
// unsupported or unreliable, e.g. some NFS mounts).
func lockExclusive(f *os.File) error {
	err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
	if err == nil {
		return nil
	}
	if errors.Is(err, syscall.EWOULDBLOCK) || errors.Is(err, syscall.EAGAIN) {
		return errWouldBlock
	}
	return err
}

// unlockExclusive releases the advisory lock held on f.
func unlockExclusive(f *os.File) error {
	return syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
}
