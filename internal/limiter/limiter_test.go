package limiter_test

import (
	"context"
	"errors"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	goredis "github.com/redis/go-redis/v9"

	"github.com/Disneyjr/Rate-Limiter/internal/limiter"
	"github.com/Disneyjr/Rate-Limiter/internal/storage/memory"
	redisstorage "github.com/Disneyjr/Rate-Limiter/internal/storage/redis"
)

// storages retorna as Strategies disponíveis para teste. O Redis é incluído
// apenas quando REDIS_ADDR está definido (ex.: via docker compose).
func storages(t *testing.T) map[string]func(t *testing.T) limiter.Storage {
	t.Helper()
	m := map[string]func(t *testing.T) limiter.Storage{
		"memory": func(t *testing.T) limiter.Storage { return memory.New() },
	}
	if addr := os.Getenv("REDIS_ADDR"); addr != "" {
		m["redis"] = func(t *testing.T) limiter.Storage {
			client := goredis.NewClient(&goredis.Options{Addr: addr, Password: os.Getenv("REDIS_PASSWORD")})
			if err := client.Ping(context.Background()).Err(); err != nil {
				t.Fatalf("redis unavailable: %v", err)
			}
			if err := client.FlushDB(context.Background()).Err(); err != nil {
				t.Fatalf("flush redis: %v", err)
			}
			t.Cleanup(func() { _ = client.Close() })
			return redisstorage.NewFromClient(client)
		}
	}
	return m
}

func baseConfig() limiter.Config {
	return limiter.Config{
		IPLimit:            5,
		IPBlockDuration:    1500 * time.Millisecond,
		TokenLimits:        map[string]int64{"abc123": 10},
		DefaultTokenLimit:  0,
		TokenBlockDuration: 1500 * time.Millisecond,
		Window:             time.Second,
	}
}

func newLimiter(t *testing.T, s limiter.Storage, cfg limiter.Config) *limiter.Limiter {
	t.Helper()
	l, err := limiter.New(s, cfg)
	if err != nil {
		t.Fatalf("new limiter: %v", err)
	}
	return l
}

// countAllowed dispara n requisições e retorna quantas foram aceitas.
func countAllowed(t *testing.T, l *limiter.Limiter, n int, ip, token string) int {
	t.Helper()
	allowed := 0
	for i := 0; i < n; i++ {
		res, err := l.Allow(context.Background(), ip, token)
		if err != nil {
			t.Fatalf("allow: %v", err)
		}
		if res.Allowed {
			allowed++
		}
	}
	return allowed
}

func TestIPLimit(t *testing.T) {
	for name, mk := range storages(t) {
		t.Run(name, func(t *testing.T) {
			l := newLimiter(t, mk(t), baseConfig())
			if got := countAllowed(t, l, 8, "10.0.0.1", ""); got != 5 {
				t.Fatalf("expected 5 allowed requests, got %d", got)
			}
			// Outro IP não é afetado.
			if got := countAllowed(t, l, 5, "10.0.0.2", ""); got != 5 {
				t.Fatalf("expected other IP to have 5 allowed, got %d", got)
			}
		})
	}
}

func TestTokenOverridesIP(t *testing.T) {
	for name, mk := range storages(t) {
		t.Run(name, func(t *testing.T) {
			l := newLimiter(t, mk(t), baseConfig())

			// IP limitado a 5, token "abc123" permite 10: o token prevalece.
			if got := countAllowed(t, l, 15, "10.0.0.1", "abc123"); got != 10 {
				t.Fatalf("expected token limit (10) to apply, got %d", got)
			}
			res, _ := l.Allow(context.Background(), "10.0.0.1", "abc123")
			if res.Source != limiter.SourceToken || res.Limit != 10 {
				t.Fatalf("expected token source with limit 10, got %+v", res)
			}
		})
	}
}

func TestTokenLowerThanIPStillOverrides(t *testing.T) {
	for name, mk := range storages(t) {
		t.Run(name, func(t *testing.T) {
			cfg := baseConfig()
			cfg.TokenLimits = map[string]int64{"low": 2}
			l := newLimiter(t, mk(t), cfg)
			if got := countAllowed(t, l, 5, "10.0.0.1", "low"); got != 2 {
				t.Fatalf("expected token limit (2) to apply, got %d", got)
			}
		})
	}
}

func TestTokenAndIPCountersAreIndependent(t *testing.T) {
	for name, mk := range storages(t) {
		t.Run(name, func(t *testing.T) {
			l := newLimiter(t, mk(t), baseConfig())
			// Esgota o IP.
			if got := countAllowed(t, l, 6, "10.0.0.1", ""); got != 5 {
				t.Fatalf("expected 5, got %d", got)
			}
			// Mesmo IP bloqueado, mas com token válido: usa o limite do token.
			if got := countAllowed(t, l, 10, "10.0.0.1", "abc123"); got != 10 {
				t.Fatalf("expected token to be allowed 10 times, got %d", got)
			}
		})
	}
}

func TestUnknownTokenFallsBackToIP(t *testing.T) {
	for name, mk := range storages(t) {
		t.Run(name, func(t *testing.T) {
			l := newLimiter(t, mk(t), baseConfig())
			if got := countAllowed(t, l, 8, "10.0.0.1", "unknown"); got != 5 {
				t.Fatalf("expected IP limit (5) for unknown token, got %d", got)
			}
		})
	}
}

func TestDefaultTokenLimit(t *testing.T) {
	for name, mk := range storages(t) {
		t.Run(name, func(t *testing.T) {
			cfg := baseConfig()
			cfg.DefaultTokenLimit = 7
			l := newLimiter(t, mk(t), cfg)
			if got := countAllowed(t, l, 10, "10.0.0.1", "any-token"); got != 7 {
				t.Fatalf("expected default token limit (7), got %d", got)
			}
			// Token configurado continua com seu limite próprio.
			if got := countAllowed(t, l, 12, "10.0.0.1", "abc123"); got != 10 {
				t.Fatalf("expected specific token limit (10), got %d", got)
			}
		})
	}
}

func TestBlockPersistsBeyondWindowAndExpires(t *testing.T) {
	for name, mk := range storages(t) {
		t.Run(name, func(t *testing.T) {
			cfg := baseConfig()
			cfg.Window = 200 * time.Millisecond
			cfg.IPBlockDuration = 1 * time.Second
			l := newLimiter(t, mk(t), cfg)

			if got := countAllowed(t, l, 6, "10.0.0.9", ""); got != 5 {
				t.Fatalf("expected 5, got %d", got)
			}

			// A janela já expirou, mas o bloqueio ainda está ativo.
			time.Sleep(400 * time.Millisecond)
			if got := countAllowed(t, l, 1, "10.0.0.9", ""); got != 0 {
				t.Fatalf("expected IP to remain blocked, got %d allowed", got)
			}

			// Após o tempo de bloqueio, volta a aceitar.
			time.Sleep(900 * time.Millisecond)
			if got := countAllowed(t, l, 5, "10.0.0.9", ""); got != 5 {
				t.Fatalf("expected IP to be unblocked, got %d allowed", got)
			}
		})
	}
}

func TestWindowResetWithoutExceeding(t *testing.T) {
	for name, mk := range storages(t) {
		t.Run(name, func(t *testing.T) {
			cfg := baseConfig()
			cfg.Window = 200 * time.Millisecond
			l := newLimiter(t, mk(t), cfg)
			for round := 0; round < 3; round++ {
				if got := countAllowed(t, l, 5, "10.0.0.3", ""); got != 5 {
					t.Fatalf("round %d: expected 5, got %d", round, got)
				}
				time.Sleep(300 * time.Millisecond)
			}
		})
	}
}

func TestConcurrency(t *testing.T) {
	for name, mk := range storages(t) {
		t.Run(name, func(t *testing.T) {
			cfg := baseConfig()
			cfg.IPLimit = 50
			cfg.Window = 5 * time.Second
			l := newLimiter(t, mk(t), cfg)

			var allowed int64
			var wg sync.WaitGroup
			for i := 0; i < 200; i++ {
				wg.Add(1)
				go func() {
					defer wg.Done()
					res, err := l.Allow(context.Background(), "10.0.0.50", "")
					if err != nil {
						t.Errorf("allow: %v", err)
						return
					}
					if res.Allowed {
						atomic.AddInt64(&allowed, 1)
					}
				}()
			}
			wg.Wait()
			if allowed != 50 {
				t.Fatalf("expected exactly 50 allowed, got %d", allowed)
			}
		})
	}
}

type failingStorage struct{}

func (failingStorage) Increment(context.Context, string, time.Duration) (int64, error) {
	return 0, errors.New("boom")
}
func (failingStorage) Block(context.Context, string, time.Duration) error { return nil }
func (failingStorage) IsBlocked(context.Context, string) (bool, error)    { return false, nil }

func TestStorageErrorIsPropagated(t *testing.T) {
	l := newLimiter(t, failingStorage{}, baseConfig())
	if _, err := l.Allow(context.Background(), "1.1.1.1", ""); err == nil {
		t.Fatal("expected error from storage")
	}
}

func TestNewValidation(t *testing.T) {
	cases := []struct {
		name string
		s    limiter.Storage
		mut  func(*limiter.Config)
	}{
		{"nil storage", nil, func(*limiter.Config) {}},
		{"zero ip limit", memory.New(), func(c *limiter.Config) { c.IPLimit = 0 }},
		{"zero block", memory.New(), func(c *limiter.Config) { c.IPBlockDuration = 0 }},
		{"bad token limit", memory.New(), func(c *limiter.Config) { c.TokenLimits = map[string]int64{"x": 0} }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := baseConfig()
			tc.mut(&cfg)
			if _, err := limiter.New(tc.s, cfg); err == nil {
				t.Fatalf("expected validation error for %s", tc.name)
			}
		})
	}
}
