package config

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/shopspring/decimal"
)

const testPassword = "test-only-password-0123456789"

func lookupFrom(m map[string]string) LookupFunc {
	return func(k string) (string, bool) {
		v, ok := m[k]
		return v, ok
	}
}

func requiredEnv() map[string]string {
	return map[string]string{ //nolint:gosec // fake local-only DSN, used to check redaction
		"APP_ENV":        "local",
		"DATABASE_URL":   "postgres://u:p@localhost:5432/db?sslmode=disable",
		"ADMIN_USER":     "admin",
		"ADMIN_PASSWORD": testPassword,
	}
}

func TestLoadDefaults(t *testing.T) {
	c, err := Load(lookupFrom(requiredEnv()))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	checks := []struct {
		name      string
		got, want any
	}{
		{"AppEnv", c.AppEnv, "local"},
		{"Port", c.Port, 8080},
		{"DatabaseURLTest", c.DatabaseURLTest, ""},
		{"KalshiBaseURL", c.KalshiBaseURL, DefaultKalshiBaseURL},
		{"PolymarketGammaURL", c.PolymarketGammaURL, DefaultPolymarketGammaURL},
		{"PolymarketClobURL", c.PolymarketClobURL, DefaultPolymarketClobURL},
		{"UniverseDir", c.UniverseDir, "config/universe"},
		{"FactsFile", c.FactsFile, "config/facts.yaml"},
		{"EvalSummaryFile", c.EvalSummaryFile, "config/eval_summary.json"},
		{"HTTPTimeout", c.HTTPTimeout, 10 * time.Second},
		{"MaxBodyBytes", c.MaxBodyBytes, int64(10485760)},
		{"StaleAfter", c.StaleAfter, 120 * time.Second},
		{"SkewWarn", c.SkewWarn, 5 * time.Second},
		{"SkewMax", c.SkewMax, 30 * time.Second},
		{"MinRouteConfidence", c.MinRouteConfidence.String(), "0.9"},
		{"MinBookDepth", c.MinBookDepth.String(), "1"},
		{"PublicRateLimitPerMin", c.PublicRateLimitPerMin, 60},
		{"LLMEnabled", c.LLMEnabled, false},
		{"AnthropicAPIKey", c.AnthropicAPIKey, ""},
		{"LLMModel", c.LLMModel, "claude-haiku-4-5-20251001"},
		{"LLMDailyCallCap", c.LLMDailyCallCap, 200},
		{"LLMMaxOutputTokens", c.LLMMaxOutputTokens, 400},
		{"LogLevel", c.LogLevel, slog.LevelInfo},
	}
	for _, ch := range checks {
		if ch.got != ch.want {
			t.Errorf("%s = %v, want %v", ch.name, ch.got, ch.want)
		}
	}
}

func TestLoadOverrides(t *testing.T) {
	env := requiredEnv()
	for k, v := range map[string]string{
		"APP_ENV":                   "prod",
		"PORT":                      "9090",
		"DATABASE_URL_TEST":         "postgres://t",
		"KALSHI_BASE_URL":           "http://127.0.0.1:8090/kalshi/",
		"POLYMARKET_GAMMA_URL":      "http://127.0.0.1:8090/gamma",
		"POLYMARKET_CLOB_URL":       "http://127.0.0.1:8090/clob",
		"UNIVERSE_DIR":              "/tmp/u",
		"FACTS_FILE":                "/tmp/f.yaml",
		"HTTP_TIMEOUT_SECONDS":      "3",
		"MAX_BODY_BYTES":            "1024",
		"STALE_AFTER_SECONDS":       "60",
		"SKEW_WARN_SECONDS":         "2",
		"SKEW_MAX_SECONDS":          "10",
		"MIN_ROUTE_CONFIDENCE":      "0.95",
		"MIN_BOOK_DEPTH":            "5",
		"PUBLIC_RATE_LIMIT_PER_MIN": "30",
		"LLM_ENABLED":               "true",
		"ANTHROPIC_API_KEY":         "test-key",
		"LLM_MODEL":                 "other-model",
		"LLM_DAILY_CALL_CAP":        "0",
		"LLM_MAX_OUTPUT_TOKENS":     "100",
		"LOG_LEVEL":                 "DEBUG",
	} {
		env[k] = v
	}
	c, err := Load(lookupFrom(env))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if c.AppEnv != "prod" || c.Port != 9090 || c.DatabaseURLTest != "postgres://t" {
		t.Errorf("basic overrides not applied: %+v", c)
	}
	if c.KalshiBaseURL != "http://127.0.0.1:8090/kalshi" {
		t.Errorf("KalshiBaseURL = %q, want trailing slash trimmed", c.KalshiBaseURL)
	}
	if c.UniverseDir != "/tmp/u" || c.FactsFile != "/tmp/f.yaml" {
		t.Errorf("path overrides not applied: %q %q", c.UniverseDir, c.FactsFile)
	}
	if c.HTTPTimeout != 3*time.Second || c.MaxBodyBytes != 1024 || c.StaleAfter != time.Minute ||
		c.SkewWarn != 2*time.Second || c.SkewMax != 10*time.Second {
		t.Errorf("duration overrides not applied: %+v", c)
	}
	if !c.MinRouteConfidence.Equal(decimal.RequireFromString("0.95")) || !c.MinBookDepth.Equal(decimal.RequireFromString("5")) || c.PublicRateLimitPerMin != 30 {
		t.Errorf("routing overrides not applied: %s %s %d", c.MinRouteConfidence, c.MinBookDepth, c.PublicRateLimitPerMin)
	}
	if !c.LLMEnabled || c.AnthropicAPIKey != "test-key" || c.LLMModel != "other-model" ||
		c.LLMDailyCallCap != 0 || c.LLMMaxOutputTokens != 100 {
		t.Errorf("LLM overrides not applied: %+v", c)
	}
	if c.LogLevel != slog.LevelDebug {
		t.Errorf("LogLevel = %v, want debug", c.LogLevel)
	}
}

func TestLoadReportsAllMissingRequired(t *testing.T) {
	_, err := Load(lookupFrom(map[string]string{"ADMIN_USER": "   "}))
	if err == nil {
		t.Fatal("Load succeeded with no required vars")
	}
	for _, key := range []string{"APP_ENV", "DATABASE_URL", "ADMIN_USER", "ADMIN_PASSWORD"} {
		if !strings.Contains(err.Error(), key+" is required") {
			t.Errorf("error %q does not name missing %s", err, key)
		}
	}
}

func TestLoadRejectsInvalidValues(t *testing.T) {
	cases := []struct {
		key, value, want string
	}{
		{"APP_ENV", "dev", "APP_ENV must be one of"},
		{"PORT", "http", "PORT must be an integer"},
		{"PORT", "0", "PORT must be between 1 and 65535"},
		{"PORT", "70000", "PORT must be between"},
		{"ADMIN_PASSWORD", "short", "ADMIN_PASSWORD must be at least 24 characters"},
		{"KALSHI_BASE_URL", "ftp://example.com", "KALSHI_BASE_URL must be an absolute http(s) URL"},
		{"POLYMARKET_GAMMA_URL", "not a url", "POLYMARKET_GAMMA_URL must be an absolute"},
		{"POLYMARKET_CLOB_URL", "https://", "POLYMARKET_CLOB_URL must be an absolute"},
		{"HTTP_TIMEOUT_SECONDS", "0", "HTTP_TIMEOUT_SECONDS must be between"},
		{"MAX_BODY_BYTES", "-1", "MAX_BODY_BYTES must be between"},
		{"STALE_AFTER_SECONDS", "1.5", "STALE_AFTER_SECONDS must be an integer"},
		{"SKEW_WARN_SECONDS", "31", "SKEW_WARN_SECONDS must not exceed SKEW_MAX_SECONDS"},
		{"MIN_ROUTE_CONFIDENCE", "1.01", "MIN_ROUTE_CONFIDENCE must be between 0 and 1"},
		{"MIN_ROUTE_CONFIDENCE", "-0.1", "MIN_ROUTE_CONFIDENCE must be between 0 and 1"},
		{"MIN_ROUTE_CONFIDENCE", "high", "MIN_ROUTE_CONFIDENCE must be a decimal"},
		{"PUBLIC_RATE_LIMIT_PER_MIN", "0", "PUBLIC_RATE_LIMIT_PER_MIN must be between"},
		{"LLM_ENABLED", "yes", "LLM_ENABLED must be true or false"},
		{"LLM_ENABLED", "true", "ANTHROPIC_API_KEY is required when LLM_ENABLED=true"},
		{"LLM_DAILY_CALL_CAP", "-1", "LLM_DAILY_CALL_CAP must be between"},
		{"LLM_MAX_OUTPUT_TOKENS", "0", "LLM_MAX_OUTPUT_TOKENS must be between"},
		{"LOG_LEVEL", "trace", "LOG_LEVEL must be one of"},
	}
	for _, tc := range cases {
		t.Run(tc.key+"="+tc.value, func(t *testing.T) {
			env := requiredEnv()
			env[tc.key] = tc.value
			_, err := Load(lookupFrom(env))
			if err == nil {
				t.Fatalf("Load succeeded with %s=%q", tc.key, tc.value)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error %q does not contain %q", err, tc.want)
			}
		})
	}
}

func TestLoadErrorsNeverContainSecrets(t *testing.T) {
	env := requiredEnv()
	env["ADMIN_PASSWORD"] = "s3cr3t-short"
	env["LLM_ENABLED"] = "true"
	env["PORT"] = "bad"
	_, err := Load(lookupFrom(env))
	if err == nil {
		t.Fatal("expected error")
	}
	if strings.Contains(err.Error(), "s3cr3t-short") {
		t.Errorf("error leaks ADMIN_PASSWORD: %q", err)
	}
}

func TestLogValueRedactsSecrets(t *testing.T) {
	env := requiredEnv()
	env["ANTHROPIC_API_KEY"] = "sk-test-secret-value"
	env["DATABASE_URL_TEST"] = "postgres://u:testdbpass@h/db"
	c, err := Load(lookupFrom(env))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	var buf bytes.Buffer
	slog.New(slog.NewJSONHandler(&buf, nil)).Info("config", "cfg", c)
	out := buf.String()
	for _, secret := range []string{testPassword, "sk-test-secret-value", "u:p@localhost", "testdbpass"} {
		if strings.Contains(out, secret) {
			t.Errorf("log output leaks %q: %s", secret, out)
		}
	}
	if !strings.Contains(out, `"admin_password":"[REDACTED]"`) || !strings.Contains(out, `"admin_user":"admin"`) {
		t.Errorf("log output missing expected fields: %s", out)
	}
}

func TestFromEnv(t *testing.T) {
	for k, v := range requiredEnv() {
		t.Setenv(k, v)
	}
	t.Setenv("PORT", "8181")
	c, err := FromEnv()
	if err != nil {
		t.Fatalf("FromEnv: %v", err)
	}
	if c.Port != 8181 {
		t.Errorf("Port = %d, want 8181", c.Port)
	}
}

func TestLoadInvalidMinBookDepth(t *testing.T) {
	tests := []struct {
		name  string
		value string
		want  string
	}{
		{"negative", "-1", "must be positive"},
		{"zero", "0", "must be positive"},
		{"not a number", "abc", "must be a decimal"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env := requiredEnv()
			env["MIN_BOOK_DEPTH"] = tt.value
			_, err := Load(lookupFrom(env))
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Errorf("Load with MIN_BOOK_DEPTH=%q: err=%v, want %q", tt.value, err, tt.want)
			}
		})
	}
}
