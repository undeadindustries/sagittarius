package agent

import (
	"fmt"
	"os"
	"testing"
)

// TestMain points SAGITTARIUS_HOME at a throwaway directory for the whole
// package. Runners built by parallel tests (which cannot use t.Setenv) record
// sessions, and every recorder registers its workdir in projects.json and
// creates tmp/ and history/ slug directories — without this, each run leaked
// hundreds of /tmp roots into the developer's real ~/.sagittarius. Tests that
// need their own home still override it with t.Setenv.
func TestMain(m *testing.M) {
	home, err := os.MkdirTemp("", "sagittarius-agent-test-home-")
	if err != nil {
		fmt.Fprintf(os.Stderr, "create test home: %v\n", err)
		os.Exit(1)
	}
	if err := os.Setenv("SAGITTARIUS_HOME", home); err != nil {
		fmt.Fprintf(os.Stderr, "set SAGITTARIUS_HOME: %v\n", err)
		os.Exit(1)
	}
	code := m.Run()
	_ = os.RemoveAll(home)
	os.Exit(code)
}
