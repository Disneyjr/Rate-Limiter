// Package limiter contém a lógica de negócio do rate limiter, totalmente
// desacoplada do transporte HTTP e do mecanismo de persistência.
package limiter

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// Origem da limitação aplicada a uma requisição.
const (
	SourceIP    = "ip"
	SourceToken = "token"
)

// Config define as regras de limitação.
type Config struct {
	// IPLimit é o número máximo de requisições por janela para um único IP.
	IPLimit int64
	// IPBlockDuration é o tempo de bloqueio de um IP que excedeu o limite.
	IPBlockDuration time.Duration

	// TokenLimits contém limites específicos por token (token -> req/janela).
	TokenLimits map[string]int64
	// DefaultTokenLimit é aplicado a tokens não listados em TokenLimits.
	// Se for <= 0, tokens desconhecidos são ignorados e a limitação por IP é aplicada.
	DefaultTokenLimit int64
	// TokenBlockDuration é o tempo de bloqueio de um token que excedeu o limite.
	TokenBlockDuration time.Duration

	// Window é a janela de contagem (padrão: 1 segundo => req/s).
	Window time.Duration
}

// Result é o resultado da avaliação de uma requisição.
type Result struct {
	Allowed   bool
	Limit     int64
	Remaining int64
	Source    string // "ip" ou "token"
	Key       string
}

// Limiter aplica as regras de rate limiting usando uma Storage (Strategy).
type Limiter struct {
	storage Storage
	cfg     Config
}

// New cria um novo Limiter.
func New(storage Storage, cfg Config) (*Limiter, error) {
	if storage == nil {
		return nil, errors.New("limiter: storage is required")
	}
	if cfg.Window <= 0 {
		cfg.Window = time.Second
	}
	if cfg.IPLimit <= 0 {
		return nil, errors.New("limiter: IPLimit must be greater than zero")
	}
	if cfg.IPBlockDuration <= 0 || cfg.TokenBlockDuration <= 0 {
		return nil, errors.New("limiter: block durations must be greater than zero")
	}
	for token, limit := range cfg.TokenLimits {
		if limit <= 0 {
			return nil, fmt.Errorf("limiter: invalid limit for token %q", token)
		}
	}
	return &Limiter{storage: storage, cfg: cfg}, nil
}

// Allow avalia se uma requisição vinda de "ip" com o token "token" (pode ser
// vazio) deve ser aceita. A configuração do token tem precedência sobre a do IP.
func (l *Limiter) Allow(ctx context.Context, ip, token string) (Result, error) {
	res := l.resolve(ip, token)

	blocked, err := l.storage.IsBlocked(ctx, res.Key)
	if err != nil {
		return res, fmt.Errorf("limiter: checking block: %w", err)
	}
	if blocked {
		res.Allowed = false
		res.Remaining = 0
		return res, nil
	}

	count, err := l.storage.Increment(ctx, res.Key, l.cfg.Window)
	if err != nil {
		return res, fmt.Errorf("limiter: incrementing counter: %w", err)
	}

	if count > res.Limit {
		if err := l.storage.Block(ctx, res.Key, l.blockDuration(res.Source)); err != nil {
			return res, fmt.Errorf("limiter: blocking key: %w", err)
		}
		res.Allowed = false
		res.Remaining = 0
		return res, nil
	}

	res.Allowed = true
	res.Remaining = res.Limit - count
	return res, nil
}

// resolve decide qual regra (token ou IP) se aplica à requisição.
func (l *Limiter) resolve(ip, token string) Result {
	if token != "" {
		if limit, ok := l.cfg.TokenLimits[token]; ok {
			return Result{Limit: limit, Source: SourceToken, Key: "token:" + token}
		}
		if l.cfg.DefaultTokenLimit > 0 {
			return Result{Limit: l.cfg.DefaultTokenLimit, Source: SourceToken, Key: "token:" + token}
		}
	}
	return Result{Limit: l.cfg.IPLimit, Source: SourceIP, Key: "ip:" + ip}
}

func (l *Limiter) blockDuration(source string) time.Duration {
	if source == SourceToken {
		return l.cfg.TokenBlockDuration
	}
	return l.cfg.IPBlockDuration
}
