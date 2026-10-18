package config

import "testing"

func TestParseTokenLimits(t *testing.T) {
	got, err := ParseTokenLimits(" abc123:100, xyz:50 ,")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got["abc123"] != 100 || got["xyz"] != 50 {
		t.Fatalf("unexpected result: %v", got)
	}

	for _, bad := range []string{"abc", "abc:", ":10", "abc:0", "abc:x"} {
		if _, err := ParseTokenLimits(bad); err == nil {
			t.Fatalf("expected error for %q", bad)
		}
	}
}

func TestLoadFromEnv(t *testing.T) {
	t.Setenv("RATE_LIMIT_IP", "7")
	t.Setenv("BLOCK_DURATION_IP_SECONDS", "60")
	t.Setenv("RATE_LIMIT_TOKENS", "tk:20")
	t.Setenv("REDIS_ADDR", "redis:6379")

	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.IPLimit != 7 || cfg.IPBlockDuration.Seconds() != 60 || cfg.TokenLimits["tk"] != 20 || cfg.RedisAddr != "redis:6379" {
		t.Fatalf("unexpected config: %+v", cfg)
	}
}
