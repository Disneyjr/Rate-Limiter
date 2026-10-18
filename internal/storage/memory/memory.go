// Package memory fornece uma implementação em memória da Strategy de
// persistência. Útil para testes e para execução sem Redis (instância única).
package memory

import (
	"context"
	"sync"
	"time"
)

type counter struct {
	value     int64
	expiresAt time.Time
}

// Storage é uma implementação thread-safe em memória de limiter.Storage.
type Storage struct {
	mu       sync.Mutex
	counters map[string]*counter
	blocked  map[string]time.Time
	now      func() time.Time
}

// New cria um novo Storage em memória.
func New() *Storage {
	return &Storage{
		counters: make(map[string]*counter),
		blocked:  make(map[string]time.Time),
		now:      time.Now,
	}
}

// Increment implementa limiter.Storage.
func (s *Storage) Increment(_ context.Context, key string, window time.Duration) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := s.now()
	c, ok := s.counters[key]
	if !ok || !now.Before(c.expiresAt) {
		c = &counter{expiresAt: now.Add(window)}
		s.counters[key] = c
	}
	c.value++
	return c.value, nil
}

// Block implementa limiter.Storage.
func (s *Storage) Block(_ context.Context, key string, duration time.Duration) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.blocked[key] = s.now().Add(duration)
	return nil
}

// IsBlocked implementa limiter.Storage.
func (s *Storage) IsBlocked(_ context.Context, key string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	until, ok := s.blocked[key]
	if !ok {
		return false, nil
	}
	if !s.now().Before(until) {
		delete(s.blocked, key)
		return false, nil
	}
	return true, nil
}
