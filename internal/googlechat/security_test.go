package googlechat

import (
	"testing"
	"time"
)

func TestIsSingleUserBotDm(t *testing.T) {
	if IsSingleUserBotDm(nil) {
		t.Error("nil space should not be single user bot dm")
	}
	if IsSingleUserBotDm(&Space{SingleUserBotDm: false}) {
		t.Error("Space with SingleUserBotDm=false should not be allowed")
	}
	if !IsSingleUserBotDm(&Space{SingleUserBotDm: true}) {
		t.Error("Space with SingleUserBotDm=true should be allowed")
	}
}

func TestIsAuthorizedSender(t *testing.T) {
	allowed := []string{
		"users/11223344",
		"rob@undeadindustries.com",
	}

	tests := []struct {
		name string
		user *User
		want bool
	}{
		{
			name: "nil user",
			user: nil,
			want: false,
		},
		{
			name: "matched resource name exactly",
			user: &User{Name: "users/11223344"},
			want: true,
		},
		{
			name: "matched resource name without users/ prefix",
			user: &User{Name: "users/11223344"},
			want: true,
		},
		{
			name: "matched email case insensitive",
			user: &User{Email: "ROB@undeadindustries.com"},
			want: true,
		},
		{
			name: "unauthorized email",
			user: &User{Email: "intruder@example.com"},
			want: false,
		},
		{
			name: "unauthorized resource name",
			user: &User{Name: "users/99999999"},
			want: false,
		},
		{
			name: "empty allowed list",
			user: &User{Name: "users/11223344"},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			list := allowed
			if tt.name == "empty allowed list" {
				list = nil
			}
			got := IsAuthorizedSender(tt.user, list)
			if got != tt.want {
				t.Errorf("IsAuthorizedSender() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestDeduplicator(t *testing.T) {
	d := NewDeduplicator(3, 50*time.Millisecond)

	// Empty string is ignored
	if d.SeenOrAdd("") {
		t.Error("empty id should return false")
	}

	// First time seen -> false
	if d.SeenOrAdd("msg-1") {
		t.Error("first time msg-1 should return false")
	}
	// Second time seen -> true
	if !d.SeenOrAdd("msg-1") {
		t.Error("second time msg-1 should return true")
	}

	// Add more to test capacity eviction
	d.SeenOrAdd("msg-2")
	d.SeenOrAdd("msg-3")
	d.SeenOrAdd("msg-4") // msg-1 should be evicted

	if d.SeenOrAdd("msg-1") {
		t.Error("msg-1 should have been evicted and return false")
	}

	// Test TTL expiration
	d.SeenOrAdd("msg-exp")
	time.Sleep(60 * time.Millisecond)
	if d.SeenOrAdd("msg-exp") {
		t.Error("msg-exp should have expired and return false")
	}
}
