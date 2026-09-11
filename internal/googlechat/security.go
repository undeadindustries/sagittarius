package googlechat

import (
	"strings"
	"sync"
	"time"
)

// IsSingleUserBotDm verifies that the space is an authorized 1:1 DM with the bot.
func IsSingleUserBotDm(space *Space) bool {
	if space == nil {
		return false
	}
	return space.SingleUserBotDm
}

// IsAuthorizedSender checks if user is in the allowed users list.
// Checks user.Name (e.g. "users/123456789") as well as user.Email (case-insensitive).
// If allowed is empty, nobody is authorized.
func IsAuthorizedSender(user *User, allowed []string) bool {
	if user == nil || len(allowed) == 0 {
		return false
	}

	userName := strings.TrimSpace(user.Name)
	userEmail := strings.ToLower(strings.TrimSpace(user.Email))

	for _, a := range allowed {
		clean := strings.TrimSpace(a)
		if clean == "" {
			continue
		}
		// Match user resource name directly (e.g. "users/123456789" or "123456789")
		if clean == userName || "users/"+clean == userName {
			return true
		}
		// Match email case-insensitively if email is present
		if userEmail != "" && strings.EqualFold(clean, userEmail) {
			return true
		}
	}
	return false
}

// Deduplicator keeps a bounded in-memory cache of seen message IDs to deduplicate
// redelivered events from Google Chat (such as Pub/Sub at-least-once delivery).
type Deduplicator struct {
	mu         sync.Mutex
	maxEntries int
	ttl        time.Duration
	entries    map[string]time.Time
	order      []string
}

// NewDeduplicator constructs a Deduplicator with maximum capacity and TTL.
func NewDeduplicator(maxEntries int, ttl time.Duration) *Deduplicator {
	if maxEntries <= 0 {
		maxEntries = 1000
	}
	if ttl <= 0 {
		ttl = time.Hour
	}
	return &Deduplicator{
		maxEntries: maxEntries,
		ttl:        ttl,
		entries:    make(map[string]time.Time),
		order:      make([]string, 0, maxEntries),
	}
}

// SeenOrAdd checks if id was already recorded. If seen and not expired, returns true.
// Otherwise, records id and returns false.
func (d *Deduplicator) SeenOrAdd(id string) bool {
	if id == "" {
		return false
	}

	d.mu.Lock()
	defer d.mu.Unlock()

	now := time.Now()

	// Clean up expired entries if cache is growing
	if t, exists := d.entries[id]; exists {
		if now.Sub(t) < d.ttl {
			return true // seen and valid
		}
		// Expired: update timestamp and treat as fresh
		d.entries[id] = now
		return false
	}

	// Evict oldest if full
	for len(d.order) >= d.maxEntries {
		oldest := d.order[0]
		d.order = d.order[1:]
		delete(d.entries, oldest)
	}

	d.entries[id] = now
	d.order = append(d.order, id)
	return false
}
