package store

import (
	"time"

	"github.com/raven-clown/bridge/forge"
)

type mark struct {
	Seen     *time.Time `json:"seen,omitempty"`
	Answered *time.Time `json:"answered,omitempty"`
}

func (s *Store) Seen(id forge.ItemID) (seenAt, answeredAt *time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	m := s.seen[itemKey(id)]
	return m.Seen, m.Answered
}

func (s *Store) MarkSeen(id forge.ItemID, at time.Time) error {
	return s.updateMark(id, func(m *mark) { m.Seen = &at })
}

func (s *Store) MarkAnswered(id forge.ItemID, at time.Time) error {
	return s.updateMark(id, func(m *mark) { m.Answered = &at })
}

func (s *Store) updateMark(id forge.ItemID, fn func(*mark)) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	k := itemKey(id)
	m := s.seen[k]
	fn(&m)
	s.seen[k] = m
	return s.writeJSON("seen.json", s.seen)
}
