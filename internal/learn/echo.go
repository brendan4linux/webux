// Package learn implements the CLI echo system — every action Webux takes
// emits the equivalent shell command so users can learn how Linux works.
package learn

import (
	"context"
	"database/sql"
	"sync"
	"time"
)

const ringSize = 500

// EchoEntry is one emitted CLI command with optional explanation.
type EchoEntry struct {
	ID          int64     `json:"id"`
	Cmd         string    `json:"cmd"`
	Explanation string    `json:"explanation"`
	Context     string    `json:"context"`
	CreatedAt   time.Time `json:"created_at"`
}

// Store holds the in-memory ring buffer and persists to SQLite.
type Store struct {
	mu   sync.RWMutex
	ring []EchoEntry
	db   *sql.DB

	// subscribers are WebSocket broadcast functions registered by the hub
	subs []func(EchoEntry)
}

// NewStore creates a Store backed by the given database.
func NewStore(db *sql.DB) *Store {
	return &Store{
		db:   db,
		ring: make([]EchoEntry, 0, ringSize),
	}
}

// Subscribe registers a function that will be called on every new echo.
func (s *Store) Subscribe(fn func(EchoEntry)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.subs = append(s.subs, fn)
}

// Echo emits a CLI command string and optional explanation.
// It is safe to call from any goroutine.
func (s *Store) Echo(ctx context.Context, cmd, explanation string) {
	entry := EchoEntry{
		Cmd:         cmd,
		Explanation: explanation,
		CreatedAt:   time.Now().UTC(),
	}

	// Extract context tag from ctx if set
	if tag, ok := ctx.Value(contextKey{}).(string); ok {
		entry.Context = tag
	}

	s.mu.Lock()
	// Persist to DB (best-effort)
	if s.db != nil {
		row := s.db.QueryRowContext(ctx,
			`INSERT INTO webux_cli_echo_log (cmd, explanation, context)
			 VALUES (?, ?, ?) RETURNING id`,
			entry.Cmd, entry.Explanation, entry.Context)
		_ = row.Scan(&entry.ID)
	}

	// Append to ring, dropping oldest if full
	if len(s.ring) >= ringSize {
		s.ring = s.ring[1:]
	}
	s.ring = append(s.ring, entry)
	subs := s.subs
	s.mu.Unlock()

	// Notify subscribers outside the lock
	for _, fn := range subs {
		fn(entry)
	}
}

// Recent returns the last n entries from the ring buffer.
func (s *Store) Recent(n int) []EchoEntry {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if n >= len(s.ring) {
		out := make([]EchoEntry, len(s.ring))
		copy(out, s.ring)
		return out
	}
	out := make([]EchoEntry, n)
	copy(out, s.ring[len(s.ring)-n:])
	return out
}

// WithContext returns a child context tagged with a learn context label
// (e.g. "ports", "services") so echoes can be filtered in the UI.
func WithContext(ctx context.Context, tag string) context.Context {
	return context.WithValue(ctx, contextKey{}, tag)
}

type contextKey struct{}
