package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/Disneyjr/Rate-Limiter/internal/config"
	"github.com/Disneyjr/Rate-Limiter/internal/limiter"
	"github.com/Disneyjr/Rate-Limiter/internal/middleware"
	"github.com/Disneyjr/Rate-Limiter/internal/storage/memory"
	redisstorage "github.com/Disneyjr/Rate-Limiter/internal/storage/redis"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatal(err)
	}

	storage, cleanup, err := newStorage(cfg)
	if err != nil {
		log.Fatalf("storage: %v", err)
	}
	defer cleanup()

	rl, err := limiter.New(storage, limiter.Config{
		IPLimit:            cfg.IPLimit,
		IPBlockDuration:    cfg.IPBlockDuration,
		TokenLimits:        cfg.TokenLimits,
		DefaultTokenLimit:  cfg.DefaultTokenLimit,
		TokenBlockDuration: cfg.TokenBlockDuration,
		Window:             cfg.Window,
	})
	if err != nil {
		log.Fatal(err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"message":"Hello, World!"}`))
	})

	handler := middleware.RateLimiter(rl, middleware.Options{
		TrustForwardedHeaders: cfg.TrustForwardedHeaders,
	})(mux)

	srv := &http.Server{
		Addr:              ":" + cfg.ServerPort,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
	}

	go func() {
		log.Printf("server listening on :%s (storage=%s, ip_limit=%d/%s, token_default=%d, tokens=%d)",
			cfg.ServerPort, cfg.StorageType, cfg.IPLimit, cfg.Window, cfg.DefaultTokenLimit, len(cfg.TokenLimits))
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatal(err)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = srv.Shutdown(ctx)
}

// newStorage escolhe a Strategy de persistência de acordo com STORAGE_TYPE.
func newStorage(cfg *config.Config) (limiter.Storage, func(), error) {
	switch cfg.StorageType {
	case "redis":
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		s, err := redisstorage.New(ctx, redisstorage.Options{
			Addr:     cfg.RedisAddr,
			Password: cfg.RedisPassword,
			DB:       cfg.RedisDB,
		})
		if err != nil {
			return nil, nil, err
		}
		return s, func() { _ = s.Close() }, nil
	case "memory":
		return memory.New(), func() {}, nil
	default:
		return nil, nil, fmt.Errorf("unknown STORAGE_TYPE %q", cfg.StorageType)
	}
}
