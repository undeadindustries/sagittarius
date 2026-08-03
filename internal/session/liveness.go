package session

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"time"
)

// Liveness is a per-session advisory lock used purely as a liveness registry:
// a running Sagittarius process holds an exclusive OS-level lock on a small
// file under the project's tmp dir for the lifetime of its session. The kernel
// releases that lock when the process dies — including SIGKILL and the SIGHUP a
// dropped SSH connection sends — so probing the lock is a reliable way to tell
// "this session is still live in another terminal" apart from "this session was
// abandoned." It is never used to block a second process: two terminals against
// one repo is legitimate, and callers must treat any probe failure as
// assume-live rather than assume-dead.
//
// The file's contents (session id, PID, start time, tty) identify which
// session holds the lock when a probe finds it held. They are informational
// only; liveness is determined by the lock itself, never by parsing the file.

// LivenessInfo is the payload written into the lock file. It is populated by
// the holder and read by a prober that finds the lock held, purely to label
// the live session for the user.
type LivenessInfo struct {
	SessionID string `json:"sessionId"`
	PID       int    `json:"pid"`
	StartTime string `json:"startTime"`
	TTY       string `json:"tty,omitempty"`
}

// livenessLockPath returns the lock-file path for a session id under dir.
func livenessLockPath(dir, sessionID string) string {
	return filepath.Join(dir, sanitizeLivenessKey(sessionID)+".lock")
}

// sanitizeLivenessKey makes a session id safe for use as a filename.
func sanitizeLivenessKey(sessionID string) string {
	out := make([]rune, 0, len(sessionID))
	for _, c := range sessionID {
		isAlnum := (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9')
		if isAlnum || c == '-' || c == '_' {
			out = append(out, c)
		} else {
			out = append(out, '_')
		}
	}
	if len(out) == 0 {
		return "session"
	}
	return string(out)
}

// Acquire attempts to take the exclusive liveness lock for sessionID under dir
// and writes the identifying payload. It returns a release function. On any
// error (including the lock already being held by another live session) it
// returns a nil release func and a nil error — acquisition is best-effort, and
// a process that fails to acquire its own lock still runs normally; it simply
// is not registered as the live holder.
func Acquire(dir string, info LivenessInfo) (release func()) {
	if dir == "" || info.SessionID == "" {
		return nil
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil
	}
	path := livenessLockPath(dir, info.SessionID)
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil
	}
	if err := lockExclusive(f); err != nil {
		_ = f.Close()
		return nil
	}
	if info.PID == 0 {
		info.PID = os.Getpid()
	}
	if info.StartTime == "" {
		info.StartTime = time.Now().UTC().Format(time.RFC3339)
	}
	if info.TTY == "" {
		info.TTY = os.Getenv("SSH_TTY")
	}
	// Best-effort payload write; liveness comes from the lock, not the file.
	_ = f.Truncate(0)
	_, _ = f.Seek(0, 0)
	_ = json.NewEncoder(f).Encode(info)

	var once bool
	return func() {
		if once {
			return
		}
		once = true
		_ = unlockExclusive(f)
		_ = f.Close()
	}
}

// ProbeState is the result of probing a session's liveness lock.
type ProbeState int

const (
	// ProbeLive means the lock is held by another running process.
	ProbeLive ProbeState = iota
	// ProbeFree means the lock could be acquired, so no live process holds it.
	ProbeFree
	// ProbeUnknown means the probe could not be completed (I/O error, or an
	// unsupported filesystem such as NFS where flock is unreliable). Callers
	// must treat this as assume-live: offering to resume in this state risks
	// two processes appending to one JSONL.
	ProbeUnknown
)

// Probe reports whether the session identified by sessionID under dir is live.
// When the lock is held it also returns the recorded LivenessInfo for display.
// A probe that acquires the lock releases it immediately; ProbeFree therefore
// means "abandoned," but the caller is racing nothing by acting on it.
func Probe(dir, sessionID string) (ProbeState, LivenessInfo) {
	var info LivenessInfo
	if dir == "" || sessionID == "" {
		return ProbeUnknown, info
	}
	path := livenessLockPath(dir, sessionID)
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return ProbeUnknown, info
	}
	defer func() { _ = f.Close() }()

	if err := lockExclusive(f); err != nil {
		if errors.Is(err, errWouldBlock) {
			// Held by a live process; read the payload for display.
			_, _ = f.Seek(0, 0)
			_ = json.NewDecoder(f).Decode(&info)
			return ProbeLive, info
		}
		return ProbeUnknown, info
	}
	// Acquired: no live holder. Release before returning.
	_ = unlockExclusive(f)
	return ProbeFree, info
}

// String renders a ProbeState for logging.
func (s ProbeState) String() string {
	switch s {
	case ProbeLive:
		return "live"
	case ProbeFree:
		return "free"
	default:
		return "unknown"
	}
}
