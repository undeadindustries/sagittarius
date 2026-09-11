package googlechat

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestRESTClientCreateMessage(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("got method %s, want POST", r.Method)
		}
		if r.URL.Path != "/spaces/SPACE1/messages" {
			t.Errorf("got path %s, want /spaces/SPACE1/messages", r.URL.Path)
		}
		if r.URL.Query().Get("threadKey") != "sess-123" {
			t.Errorf("got threadKey %q, want sess-123", r.URL.Query().Get("threadKey"))
		}

		var in Message
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		if in.Text != "hello world" {
			t.Errorf("got text %q, want hello world", in.Text)
		}

		resp := Message{
			Name: "spaces/SPACE1/messages/msg-1",
			Text: in.Text,
			Thread: &Thread{
				Name: "spaces/SPACE1/threads/th-1",
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer ts.Close()

	client := NewRESTClient(ts.Client(), ts.URL)
	msg := &Message{
		Text: "hello world",
		Thread: &Thread{
			ThreadKey: "sess-123",
		},
	}

	created, err := client.CreateMessage(context.Background(), "spaces/SPACE1", msg)
	if err != nil {
		t.Fatalf("CreateMessage failed: %v", err)
	}
	if created.Name != "spaces/SPACE1/messages/msg-1" {
		t.Errorf("got name %q, want spaces/SPACE1/messages/msg-1", created.Name)
	}
}

func TestRESTClientPatchMessage(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPatch {
			t.Errorf("got method %s, want PATCH", r.Method)
		}
		if r.URL.Path != "/spaces/SPACE1/messages/msg-1" {
			t.Errorf("got path %s, want /spaces/SPACE1/messages/msg-1", r.URL.Path)
		}
		if r.URL.Query().Get("updateMask") != "text,cardsV2" {
			t.Errorf("got updateMask %q, want text,cardsV2", r.URL.Query().Get("updateMask"))
		}

		resp := Message{
			Name: "spaces/SPACE1/messages/msg-1",
			Text: "updated",
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer ts.Close()

	client := NewRESTClient(ts.Client(), ts.URL)
	msg := &Message{
		Text: "updated",
	}

	updated, err := client.PatchMessage(context.Background(), "spaces/SPACE1/messages/msg-1", msg, "text,cardsV2")
	if err != nil {
		t.Fatalf("PatchMessage failed: %v", err)
	}
	if updated.Text != "updated" {
		t.Errorf("got text %q, want updated", updated.Text)
	}
}

func TestRESTClientDeleteMessage(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			t.Errorf("got method %s, want DELETE", r.Method)
		}
		if r.URL.Path != "/spaces/SPACE1/messages/msg-1" {
			t.Errorf("got path %s, want /spaces/SPACE1/messages/msg-1", r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	client := NewRESTClient(ts.Client(), ts.URL)
	err := client.DeleteMessage(context.Background(), "spaces/SPACE1/messages/msg-1")
	if err != nil {
		t.Fatalf("DeleteMessage failed: %v", err)
	}
}

func TestFakeClient(t *testing.T) {
	client := NewFakeClient()
	ctx := context.Background()

	created, err := client.CreateMessage(ctx, "spaces/DM", &Message{Text: "first"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if len(client.Created) != 1 {
		t.Fatalf("got %d created messages, want 1", len(client.Created))
	}

	patched, err := client.PatchMessage(ctx, created.Name, &Message{Text: "first updated"}, "text")
	if err != nil {
		t.Fatalf("patch: %v", err)
	}
	if patched.Text != "first updated" {
		t.Errorf("got patched text %q, want 'first updated'", patched.Text)
	}

	err = client.DeleteMessage(ctx, created.Name)
	if err != nil {
		t.Fatalf("delete: %v", err)
	}
	if len(client.Deleted) != 1 || client.Deleted[0] != created.Name {
		t.Errorf("got deleted %v, want [%s]", client.Deleted, created.Name)
	}
}

func TestExpandPath(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("UserHomeDir not available")
	}

	tests := []struct {
		input string
		want  string
	}{
		{"~", home},
		{"~/foo/bar.json", filepath.Join(home, "foo", "bar.json")},
		{"~/.sagittarius/key.json", filepath.Join(home, ".sagittarius", "key.json")},
		{"/abs/path/key.json", "/abs/path/key.json"},
		{"rel/path/key.json", "rel/path/key.json"},
		{"", ""},
	}

	for _, tc := range tests {
		got := ExpandPath(tc.input)
		if got != tc.want {
			t.Errorf("ExpandPath(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}
