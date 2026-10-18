// Package middleware expõe o rate limiter como um middleware HTTP.
package middleware

import (
	"context"
	"log"
	"net"
	"net/http"
	"strconv"
	"strings"

	"github.com/Disneyjr/Rate-Limiter/internal/limiter"
)

// TokenHeader é o header onde o token de acesso é informado.
const TokenHeader = "API_KEY"

// BlockedMessage é a mensagem retornada quando o limite é excedido.
const BlockedMessage = "you have reached the maximum number of requests or actions allowed within a certain time frame"

// Allower abstrai o Limiter para o middleware (facilita testes e desacoplamento).
type Allower interface {
	Allow(ctx context.Context, ip, token string) (limiter.Result, error)
}

// Options do middleware.
type Options struct {
	// TrustForwardedHeaders habilita o uso de X-Forwarded-For / X-Real-IP
	// para identificar o IP do cliente (use apenas atrás de um proxy confiável).
	TrustForwardedHeaders bool
}

// RateLimiter retorna um middleware que aplica o rate limiting.
func RateLimiter(l Allower, opts Options) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ip := ClientIP(r, opts.TrustForwardedHeaders)
			token := strings.TrimSpace(r.Header.Get(TokenHeader))

			res, err := l.Allow(r.Context(), ip, token)
			if err != nil {
				// Fail-open: uma indisponibilidade da persistência não derruba o serviço.
				log.Printf("rate limiter error: %v", err)
				next.ServeHTTP(w, r)
				return
			}

			w.Header().Set("X-RateLimit-Limit", strconv.FormatInt(res.Limit, 10))
			w.Header().Set("X-RateLimit-Remaining", strconv.FormatInt(res.Remaining, 10))

			if !res.Allowed {
				w.Header().Set("Content-Type", "text/plain; charset=utf-8")
				w.WriteHeader(http.StatusTooManyRequests)
				_, _ = w.Write([]byte(BlockedMessage))
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// ClientIP extrai o IP do cliente da requisição.
func ClientIP(r *http.Request, trustForwarded bool) string {
	if trustForwarded {
		if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
			if ip := strings.TrimSpace(strings.Split(xff, ",")[0]); ip != "" {
				return ip
			}
		}
		if xrip := strings.TrimSpace(r.Header.Get("X-Real-IP")); xrip != "" {
			return xrip
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
