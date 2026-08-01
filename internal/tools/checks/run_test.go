package checks

import "testing"

func TestArgvNarrowsFileScoped(t *testing.T) {
	t.Parallel()

	fileScoped := Check{Name: "format", Command: "gofmt", Args: []string{"-l", "."}, FileScoped: true}
	got := Argv(fileScoped, []string{"a.go", "b.go"})
	want := []string{"-l", "a.go", "b.go"}
	if len(got) != len(want) {
		t.Fatalf("argv = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("argv = %v, want %v", got, want)
		}
	}

	moduleScoped := Check{Name: "vet", Command: "go", Args: []string{"vet", "./..."}}
	got = Argv(moduleScoped, []string{"a.go"})
	if len(got) != 2 || got[1] != "./..." {
		t.Fatalf("module-scoped argv should be unchanged, got %v", got)
	}
}

func TestTruncateCaps(t *testing.T) {
	t.Parallel()

	short := "hello"
	if got := Truncate(short); got != short {
		t.Fatalf("Truncate(%q) = %q, want unchanged", short, got)
	}

	long := make([]byte, maxCheckOutputBytes+100)
	for i := range long {
		long[i] = 'x'
	}
	got := Truncate(string(long))
	if len(got) <= maxCheckOutputBytes {
		t.Fatalf("Truncate should still be capped-ish with suffix, got len %d", len(got))
	}
	if got[:maxCheckOutputBytes] != string(long[:maxCheckOutputBytes]) {
		t.Fatal("Truncate must preserve the leading maxCheckOutputBytes bytes")
	}
}
