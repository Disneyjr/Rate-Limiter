package limiter

import (
	"context"
	"time"
)

// Storage é a interface Strategy de persistência utilizada pelo Limiter.
//
// Qualquer mecanismo (Redis, memória, Memcached, banco relacional, ...) pode ser
// utilizado pelo rate limiter bastando implementar esta interface e injetá-la
// em limiter.New. A lógica de negócio não conhece detalhes da persistência.
type Storage interface {
	// Increment incrementa atomicamente o contador da chave e retorna o novo valor.
	// Quando a chave é criada, ela deve expirar após "window".
	Increment(ctx context.Context, key string, window time.Duration) (int64, error)

	// Block marca a chave como bloqueada pelo período informado.
	Block(ctx context.Context, key string, duration time.Duration) error

	// IsBlocked informa se a chave está bloqueada no momento.
	IsBlocked(ctx context.Context, key string) (bool, error)
}
