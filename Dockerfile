# ---------- build / test ----------
FROM golang:1.25-alpine AS builder
WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .

# Estágio usado pelo serviço "tests" do docker-compose
FROM builder AS test
CMD ["go", "test", "-v", "-count=1", "./..."]

FROM builder AS build
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /bin/rate-limiter ./cmd/server

# ---------- runtime ----------
FROM alpine:3.20 AS runtime
WORKDIR /app
RUN adduser -D -u 10001 appuser
COPY --from=build /bin/rate-limiter /app/rate-limiter
USER appuser
EXPOSE 8080
ENTRYPOINT ["/app/rate-limiter"]
