// Package config carrega a configuração a partir de variáveis de ambiente
// (ou de um arquivo .env na raiz do projeto).
package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/joho/godotenv"
)

// Config da aplicação.
type Config struct {
	ServerPort string

	StorageType string // "redis" (padrão) ou "memory"

	RedisAddr     string
	RedisPassword string
	RedisDB       int

	IPLimit            int64
	IPBlockDuration    time.Duration
	TokenLimits        map[string]int64
	DefaultTokenLimit  int64
	TokenBlockDuration time.Duration
	Window             time.Duration

	TrustForwardedHeaders bool
}

// Load lê o .env (se existir) e as variáveis de ambiente.
// Variáveis de ambiente já definidas têm precedência sobre o .env.
func Load() (*Config, error) {
	_ = godotenv.Load()

	var err error
	cfg := &Config{
		ServerPort:    getEnv("SERVER_PORT", "8080"),
		StorageType:   strings.ToLower(getEnv("STORAGE_TYPE", "redis")),
		RedisAddr:     getEnv("REDIS_ADDR", "localhost:6379"),
		RedisPassword: getEnv("REDIS_PASSWORD", ""),
	}

	if cfg.RedisDB, err = getInt("REDIS_DB", 0); err != nil {
		return nil, err
	}
	if cfg.IPLimit, err = getInt64("RATE_LIMIT_IP", 10); err != nil {
		return nil, err
	}
	if cfg.DefaultTokenLimit, err = getInt64("RATE_LIMIT_TOKEN_DEFAULT", 100); err != nil {
		return nil, err
	}
	if cfg.IPBlockDuration, err = getSeconds("BLOCK_DURATION_IP_SECONDS", 300); err != nil {
		return nil, err
	}
	if cfg.TokenBlockDuration, err = getSeconds("BLOCK_DURATION_TOKEN_SECONDS", 300); err != nil {
		return nil, err
	}
	if cfg.Window, err = getSeconds("RATE_LIMIT_WINDOW_SECONDS", 1); err != nil {
		return nil, err
	}
	if cfg.TokenLimits, err = ParseTokenLimits(os.Getenv("RATE_LIMIT_TOKENS")); err != nil {
		return nil, err
	}
	cfg.TrustForwardedHeaders, _ = strconv.ParseBool(getEnv("TRUST_FORWARDED_HEADERS", "false"))

	return cfg, nil
}

// ParseTokenLimits converte "tokenA:100,tokenB:50" em um mapa.
func ParseTokenLimits(raw string) (map[string]int64, error) {
	limits := make(map[string]int64)
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return limits, nil
	}
	for _, pair := range strings.Split(raw, ",") {
		pair = strings.TrimSpace(pair)
		if pair == "" {
			continue
		}
		idx := strings.LastIndex(pair, ":")
		if idx <= 0 || idx == len(pair)-1 {
			return nil, fmt.Errorf("config: invalid RATE_LIMIT_TOKENS entry %q (expected token:limit)", pair)
		}
		token := strings.TrimSpace(pair[:idx])
		limit, err := strconv.ParseInt(strings.TrimSpace(pair[idx+1:]), 10, 64)
		if err != nil || limit <= 0 {
			return nil, fmt.Errorf("config: invalid limit in RATE_LIMIT_TOKENS entry %q", pair)
		}
		limits[token] = limit
	}
	return limits, nil
}

func getEnv(key, def string) string {
	if v, ok := os.LookupEnv(key); ok && strings.TrimSpace(v) != "" {
		return strings.TrimSpace(v)
	}
	return def
}

func getInt(key string, def int) (int, error) {
	v, err := strconv.Atoi(getEnv(key, strconv.Itoa(def)))
	if err != nil {
		return 0, fmt.Errorf("config: invalid %s: %w", key, err)
	}
	return v, nil
}

func getInt64(key string, def int64) (int64, error) {
	v, err := strconv.ParseInt(getEnv(key, strconv.FormatInt(def, 10)), 10, 64)
	if err != nil {
		return 0, fmt.Errorf("config: invalid %s: %w", key, err)
	}
	return v, nil
}

func getSeconds(key string, def int64) (time.Duration, error) {
	v, err := getInt64(key, def)
	if err != nil {
		return 0, err
	}
	return time.Duration(v) * time.Second, nil
}
