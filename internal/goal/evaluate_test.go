package goal

import (
	"context"
	"testing"

	"github.com/undeadindustries/sagittarius/internal/provider"
)

func TestParseDecision(t *testing.T) {
	tests := []struct {
		name    string
		raw     string
		want    Decision
		wantErr bool
	}{
		{
			name: "clean JSON",
			raw:  `{"done": true, "reason": "all tests pass"}`,
			want: Decision{Done: true, Reason: "all tests pass"},
		},
		{
			name: "markdown wrapped",
			raw:  "```json\n{\"done\": false, \"reason\": \"still failing\"}\n```",
			want: Decision{Done: false, Reason: "still failing"},
		},
		{
			name:    "invalid",
			raw:     "not json",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseDecision(tt.raw)
			if (err != nil) != tt.wantErr {
				t.Fatalf("parseDecision() error = %v, wantErr %v", err, tt.wantErr)
			}
			if !tt.wantErr && (got.Done != tt.want.Done || got.Reason != tt.want.Reason) {
				t.Errorf("got %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestWithinCaps(t *testing.T) {
	g := &Goal{MaxTurns: 5, TurnCount: 5}
	ok, status := withinCaps(g)
	if ok {
		t.Error("expected false at max turns")
	}
	if status != StatusBudgetLimited {
		t.Errorf("expected StatusBudgetLimited, got %s", status)
	}

	g.TurnCount = 4
	ok, status = withinCaps(g)
	if !ok {
		t.Error("expected true below max turns")
	}
	if status != StatusActive {
		t.Errorf("expected StatusActive, got %s", status)
	}
}

type fakeGenerator struct {
	text string
	err  error
}

func (f *fakeGenerator) GenerateContentStream(ctx context.Context, req *provider.GenerateRequest) (<-chan provider.StreamResponse, error) {
	if f.err != nil {
		return nil, f.err
	}
	ch := make(chan provider.StreamResponse, 1)
	ch <- provider.StreamResponse{TextDelta: f.text}
	close(ch)
	return ch, nil
}

func TestRunModelEvaluator(t *testing.T) {
	ctx := context.Background()
	gen := &fakeGenerator{text: `{"done": true, "reason": "looks good"}`}
	
	dec, err := runModelEvaluator(ctx, "fix stuff", gen, "transcript", "checks")
	if err != nil {
		t.Fatal(err)
	}
	if !dec.Done || dec.Reason != "looks good" {
		t.Errorf("unexpected decision: %+v", dec)
	}
}
