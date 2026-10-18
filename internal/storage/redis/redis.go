// Package redis fornece a implementação Redis da Strategy de persistência.
package redis

import (
	"context"
	"time"

	goredis "github.com/redis/go-redis/v9"
)

const (
	counterPrefix = "ratelimit:counter:"
	blockPrefix   = "ratelimit:block:"
)

// incrScript incrementa o contador e define a expiração apenas na criação,
// de forma atômica (evita chaves sem TTL em caso de falha entre comandos).
var incrScript = goredis.NewScript(`
local current = redis.call("INCR", KEYS[1])
if current == 1 then
  redis.call("PEXPIRE", KEYS[1], ARGV[1])
end
return current
`)

// Storage implementa limiter.Storage usando Redis.
type Storage struct {
	client *goredis.Client
}

// Options de conexão com o Redis.
type Options struct {
	Addr     string
	Password string
	DB       int
}

// New cria um Storage Redis e valida a conexão.
func New(ctx context.Context, opts Options) (*Storage, error) {
	client := goredis.NewClient(&goredis.Options{
		Addr:     opts.Addr,
		Password: opts.Password,
		DB:       opts.DB,
	})
	if err := client.Ping(ctx).Err(); err != nil {
		_ = client.Close()
		return nil, err
	}
	return &Storage{client: client}, nil
}

// NewFromClient cria um Storage a partir de um client existente.
func NewFromClient(client *goredis.Client) *Storage {
	return &Storage{client: client}
}

// Increment implementa limiter.Storage.
func (s *Storage) Increment(ctx context.Context, key string, window time.Duration) (int64, error) {
	return incrScript.Run(ctx, s.client, []string{counterPrefix + key}, window.Milliseconds()).Int64()
}

// Block implementa limiter.Storage.
func (s *Storage) Block(ctx context.Context, key string, duration time.Duration) error {
	return s.client.Set(ctx, blockPrefix+key, 1, duration).Err()
}

// IsBlocked implementa limiter.Storage.
func (s *Storage) IsBlocked(ctx context.Context, key string) (bool, error) {
	n, err := s.client.Exists(ctx, blockPrefix+key).Result()
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

// Close encerra a conexão com o Redis.
func (s *Storage) Close() error {
	return s.client.Close()
}
