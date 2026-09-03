package tools

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"syscall"
	"time"
)

const (
	// maxShellLogBytes is the hard ceiling on a single command's log file.
	// A TTY-aware child (observed: `lsblk -f` emitting ~100 MiB/s of column
	// padding) can otherwise fill the disk; rotation keeps the most recent
	// cap bytes so a live server's tail remains readable.
	maxShellLogBytes = 8 << 20
	ptyReadBufSize   = 32 * 1024
	maxZeroReads     = 8
	staleArtifactAge = 24 * time.Hour

	// drainStopTimeout bounds how long run waits for the PTY drain goroutine
	// after the child has exited or been killed. Closing a PTY master does not
	// reliably interrupt a goroutine already blocked in Read when the fd is not
	// registered with the runtime poller, and an unbounded wait there strands
	// the whole agent turn.
	drainStopTimeout = 2 * time.Second

	shellLogPattern = "sagittarius-shell-*.log"
	jobsPidPattern  = "sagittarius-jobs-*.pid"

	emptyShellOutput = "(empty)"
)

// cappedLogWriter wraps a log file and rotates in place when a write would
// exceed limit. After a rotation the file holds a marker plus the newest
// bytes, so readers that seek the tail (bgproc.Output) still see recent
// output. Only the PTY drain goroutine touches a given writer.
type cappedLogWriter struct {
	f     *os.File
	limit int64
	wrote int64
}

func newCappedLogWriter(f *os.File, limit int64) *cappedLogWriter {
	if limit <= 0 {
		limit = maxShellLogBytes
	}
	return &cappedLogWriter{f: f, limit: limit}
}

func capMarker(limit int64) []byte {
	msg := fmt.Sprintf("[sagittarius: log capped at %d bytes; earlier output dropped]\n", limit)
	if int64(len(msg)) > limit {
		short := []byte("[capped]\n")
		if int64(len(short)) > limit {
			return nil
		}
		return short
	}
	return []byte(msg)
}

// Write implements io.Writer. Bytes that do not fit are consumed (the
// prefix is dropped on rotation) so the caller never sees a short write
// unless the underlying file returns an error.
func (w *cappedLogWriter) Write(p []byte) (int, error) {
	if w == nil || w.f == nil {
		return 0, os.ErrInvalid
	}
	accepted := len(p)
	if accepted == 0 {
		return 0, nil
	}
	if w.wrote+int64(accepted) > w.limit {
		if err := w.rotate(); err != nil {
			return 0, err
		}
		p = tailThatFits(p, w.limit-w.wrote)
		if len(p) == 0 {
			return accepted, nil
		}
	}
	n, err := w.f.Write(p)
	w.wrote += int64(n)
	if err != nil {
		return n, err
	}
	return accepted, nil
}

func (w *cappedLogWriter) rotate() error {
	if err := w.f.Truncate(0); err != nil {
		return err
	}
	if _, err := w.f.Seek(0, io.SeekStart); err != nil {
		return err
	}
	w.wrote = 0
	marker := capMarker(w.limit)
	if int64(len(marker)) > w.limit {
		marker = marker[:w.limit]
	}
	n, err := w.f.Write(marker)
	w.wrote = int64(n)
	return err
}

func tailThatFits(p []byte, room int64) []byte {
	if room <= 0 {
		return nil
	}
	if int64(len(p)) <= room {
		return p
	}
	return p[int64(len(p))-room:]
}

func (t *shellTool) logLimit() int64 {
	if t != nil && t.maxLogBytes > 0 {
		return t.maxLogBytes
	}
	return maxShellLogBytes
}

func (t *shellTool) newLogWriter(f *os.File) io.Writer {
	w := io.Writer(newCappedLogWriter(f, t.logLimit()))
	if t != nil && t.wrapLog != nil {
		w = t.wrapLog(w)
	}
	return w
}

// applyLogWriteStatus turns a log snapshot plus the drain's first Write
// error into the tool-result output. An empty snapshot with a write error
// is never "(empty)" — the model needs to see that the disk (or log path)
// failed. A non-empty snapshot keeps the captured tail and appends a
// one-line warning.
func applyLogWriteStatus(snapshot string, writeErr error) (output, errKey string) {
	if writeErr == nil {
		if snapshot == "" {
			return emptyShellOutput, ""
		}
		return snapshot, ""
	}
	msg := logWriteErrorMessage(writeErr)
	if snapshot == "" {
		return msg, msg
	}
	return snapshot + "\n" + msg, ""
}

func logWriteErrorMessage(err error) string {
	if err == nil {
		return ""
	}
	if errors.Is(err, syscall.ENOSPC) {
		return fmt.Sprintf("shell log write failed: disk full (%v)", err)
	}
	return fmt.Sprintf("shell log write failed: %v", err)
}

// SweepStaleShellArtifacts removes sagittarius-shell-*.log and
// sagittarius-jobs-*.pid files in dir whose mtime is older than olderThan.
// Live logs are written continuously, so their mtime stays fresh; idle logs
// older than a day are orphans. dir empty means os.TempDir(); olderThan <= 0
// means 24h. Best-effort: individual remove errors are skipped.
func SweepStaleShellArtifacts(dir string, olderThan time.Duration) int {
	if dir == "" {
		dir = os.TempDir()
	}
	if olderThan <= 0 {
		olderThan = staleArtifactAge
	}
	cutoff := time.Now().Add(-olderThan)
	n := sweepGlob(filepath.Join(dir, shellLogPattern), cutoff)
	n += sweepGlob(filepath.Join(dir, jobsPidPattern), cutoff)
	return n
}

func sweepGlob(pattern string, cutoff time.Time) int {
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return 0
	}
	removed := 0
	for _, path := range matches {
		info, err := os.Lstat(path)
		if err != nil {
			continue
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			continue
		}
		if info.ModTime().After(cutoff) {
			continue
		}
		if err := os.Remove(path); err == nil {
			removed++
		}
	}
	return removed
}
