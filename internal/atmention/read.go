package atmention

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
)

const (
	// perFileCap bounds a single injected file so one large file cannot dominate
	// the context window.
	perFileCap = 256 * 1024
	// combinedCap bounds the total injected across all "@" references in one turn.
	combinedCap = 512 * 1024
)

// errBinary marks a file that contains NUL bytes and is therefore unsuitable for
// inline injection.
var errBinary = errors.New("appears to be a binary file")

// readCapped reads up to min(perFileCap, budget) bytes from abs. It reports
// truncation when the file is larger than the cap, and rejects files containing
// NUL bytes as binary. budget is the remaining combined byte allowance.
func readCapped(abs string, budget int) (content string, truncated bool, err error) {
	limit := perFileCap
	if budget < limit {
		limit = budget
	}
	if limit <= 0 {
		return "", true, nil
	}

	f, err := os.Open(abs)
	if err != nil {
		return "", false, err
	}
	defer f.Close()

	buf := make([]byte, limit+1)
	n, err := io.ReadFull(f, buf)
	if err != nil && err != io.ErrUnexpectedEOF && err != io.EOF {
		return "", false, fmt.Errorf("read: %w", err)
	}
	data := buf[:n]
	if n > limit {
		data = data[:limit]
		truncated = true
	}
	if bytes.IndexByte(data, 0) >= 0 {
		return "", false, errBinary
	}
	return string(data), truncated, nil
}
