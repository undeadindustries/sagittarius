package prompt

import (
	"strings"
	"testing"
)

func TestSysadminFullAnchors(t *testing.T) {
	t.Parallel()

	out := Build(Options{
		Personality: PersonalitySysadmin,
		Variant:     VariantFull,
		Interactive: true,
		Identity:    Identity{Model: "qwen3", ProviderName: "vLLM"},
		OS:          "linux",
	})

	anchors := []string{
		"# Core Mandates",
		"# This System",
		"Assess (read-only):",
		"nginx -t",
		"systemd-run",
		"is_background",
		"set -euo pipefail",
		"PEP 668",
		"Linux",
	}

	for _, a := range anchors {
		if !strings.Contains(out, a) {
			t.Errorf("sysadmin full prompt missing anchor %q", a)
		}
	}

	absent := []string{
		"This personality is an early preview",
		"make lint",
		"## Editing Rules",
	}
	for _, a := range absent {
		if strings.Contains(out, a) {
			t.Errorf("sysadmin full prompt should not contain %q", a)
		}
	}
	assertNoUnported(t, out)
}

func TestSysadminLiteAnchors(t *testing.T) {
	t.Parallel()

	out := Build(Options{
		Personality: PersonalitySysadmin,
		Variant:     VariantLite,
		Interactive: true,
		Identity:    Identity{Model: "qwen3", ProviderName: "vLLM"},
		OS:          "darwin",
	})

	anchors := []string{
		"## No Assumptions",
		"## Change Safety",
		"## This System",
		"## Workflow",
		"## Shell Commands",
		"macOS",
	}

	for _, a := range anchors {
		if !strings.Contains(out, a) {
			t.Errorf("sysadmin lite prompt missing anchor %q", a)
		}
	}

	absent := []string{
		"This personality is an early preview",
		"make lint",
		"## Editing Rules",
	}
	for _, a := range absent {
		if strings.Contains(out, a) {
			t.Errorf("sysadmin lite prompt should not contain %q", a)
		}
	}
	assertNoUnported(t, out)
}

func TestSysadminOSLabel(t *testing.T) {
	t.Parallel()

	cases := []struct {
		goos string
		want string
	}{
		{"linux", "Linux"},
		{"darwin", "macOS"},
		{"windows", "Windows"},
		{"freebsd", "FreeBSD"},
		{"openbsd", ""},
		{"", ""},
	}

	for _, tc := range cases {
		t.Run(tc.goos, func(t *testing.T) {
			if got := osLabel(tc.goos); got != tc.want {
				t.Errorf("osLabel(%q) = %q, want %q", tc.goos, got, tc.want)
			}
		})
	}

	// End-to-end check that an empty OS suppresses the sentence.
	outEmpty := Build(Options{
		Personality: PersonalitySysadmin,
		Variant:     VariantFull,
	})
	if strings.Contains(outEmpty, "Sagittarius is running on") {
		t.Errorf("sysadmin full with empty OS should not emit platform sentence")
	}

	outEmptyLite := Build(Options{
		Personality: PersonalitySysadmin,
		Variant:     VariantLite,
	})
	if strings.Contains(outEmptyLite, "Sagittarius is running on") {
		t.Errorf("sysadmin lite with empty OS should not emit platform sentence")
	}
}
