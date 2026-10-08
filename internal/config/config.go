// Package config loads Equinox settings from environment variables.
//
// Every variable listed in docs/environment.md is read here with its default.
// Load fails fast, reporting every missing or invalid variable at once, and
// never includes secret values in its errors.
package config

import (
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/shopspring/decimal"
)

// Defaults for optional variables, as documented in docs/environment.md.
const (
	DefaultPort                  = 8080
	DefaultKalshiBaseURL         = "https://api.elections.kalshi.com/trade-api/v2"
	DefaultPolymarketGammaURL    = "https://gamma-api.polymarket.com"
	DefaultPolymarketClobURL     = "https://clob.polymarket.com"
	DefaultUniverseDir           = "config/universe"
	DefaultFactsFile             = "config/facts.yaml"
	DefaultEvalSummaryFile       = "config/eval_summary.json"
	DefaultHTTPTimeoutSeconds    = 10
	DefaultMaxBodyBytes          = 10485760
	DefaultStaleAfterSeconds     = 120
	DefaultSkewWarnSeconds       = 5
	DefaultSkewMaxSeconds        = 30
	DefaultMinRouteConfidence    = "0.9"
	DefaultMinBookDepth          = "1"
	DefaultPublicRateLimitPerMin = 60
	DefaultStaleRoundWarnMinutes = 10
	DefaultLLMModel              = "claude-haiku-4-5-20251001"
	DefaultLLMDailyCallCap       = 200
	DefaultLLMMaxOutputTokens    = 400

	// MinAdminPasswordLen is the shortest ADMIN_PASSWORD accepted.
	MinAdminPasswordLen = 24
)

// App environments accepted in APP_ENV.
const (
	EnvLocal   = "local"
	EnvCI      = "ci"
	EnvStaging = "staging"
	EnvProd    = "prod"
)

const redacted = "[REDACTED]"

// Config holds every setting from docs/environment.md.
type Config struct {
	AppEnv          string
	Port            int
	DatabaseURL     string
	DatabaseURLTest string
	AdminUser       string
	AdminPassword   string

	KalshiBaseURL      string
	PolymarketGammaURL string
	PolymarketClobURL  string
	UniverseDir        string
	FactsFile          string
	EvalSummaryFile    string

	HTTPTimeout  time.Duration
	MaxBodyBytes int64
	StaleAfter   time.Duration
	SkewWarn     time.Duration
	SkewMax      time.Duration

	MinRouteConfidence    decimal.Decimal
	MinBookDepth          decimal.Decimal
	PublicRateLimitPerMin int
	StaleRoundWarnMinutes int

	LLMEnabled         bool
	AnthropicAPIKey    string
	LLMModel           string
	LLMDailyCallCap    int
	LLMMaxOutputTokens int

	LogLevel slog.Level
}

// LookupFunc reports the value of an environment variable and whether it is set.
type LookupFunc func(key string) (string, bool)

// FromEnv loads the configuration from the process environment.
func FromEnv() (Config, error) {
	return Load(os.LookupEnv)
}

// Load reads the configuration through lookup. An empty value counts as unset.
// It returns every problem found, joined into one error.
func Load(lookup LookupFunc) (Config, error) {
	l := loader{lookup: lookup}
	c := Config{
		AppEnv:          l.required("APP_ENV"),
		Port:            l.integer("PORT", DefaultPort, 1, 65535),
		DatabaseURL:     l.required("DATABASE_URL"),
		DatabaseURLTest: l.optional("DATABASE_URL_TEST", ""),
		AdminUser:       l.required("ADMIN_USER"),
		AdminPassword:   l.required("ADMIN_PASSWORD"),

		KalshiBaseURL:      l.httpURL("KALSHI_BASE_URL", DefaultKalshiBaseURL),
		PolymarketGammaURL: l.httpURL("POLYMARKET_GAMMA_URL", DefaultPolymarketGammaURL),
		PolymarketClobURL:  l.httpURL("POLYMARKET_CLOB_URL", DefaultPolymarketClobURL),
		UniverseDir:        l.optional("UNIVERSE_DIR", DefaultUniverseDir),
		FactsFile:          l.optional("FACTS_FILE", DefaultFactsFile),
		EvalSummaryFile:    l.optional("EVAL_SUMMARY_FILE", DefaultEvalSummaryFile),

		HTTPTimeout:  l.seconds("HTTP_TIMEOUT_SECONDS", DefaultHTTPTimeoutSeconds),
		MaxBodyBytes: int64(l.integer("MAX_BODY_BYTES", DefaultMaxBodyBytes, 1, 1<<31-1)),
		StaleAfter:   l.seconds("STALE_AFTER_SECONDS", DefaultStaleAfterSeconds),
		SkewWarn:     l.seconds("SKEW_WARN_SECONDS", DefaultSkewWarnSeconds),
		SkewMax:      l.seconds("SKEW_MAX_SECONDS", DefaultSkewMaxSeconds),

		MinRouteConfidence:    l.probability("MIN_ROUTE_CONFIDENCE", DefaultMinRouteConfidence),
		MinBookDepth:          l.positiveDecimal("MIN_BOOK_DEPTH", DefaultMinBookDepth),
		PublicRateLimitPerMin: l.integer("PUBLIC_RATE_LIMIT_PER_MIN", DefaultPublicRateLimitPerMin, 1, 1_000_000),
		StaleRoundWarnMinutes: l.integer("STALE_ROUND_WARN_MINUTES", DefaultStaleRoundWarnMinutes, 1, 10_000),

		LLMEnabled:         l.boolean("LLM_ENABLED", false),
		AnthropicAPIKey:    l.optional("ANTHROPIC_API_KEY", ""),
		LLMModel:           l.optional("LLM_MODEL", DefaultLLMModel),
		LLMDailyCallCap:    l.integer("LLM_DAILY_CALL_CAP", DefaultLLMDailyCallCap, 0, 100_000),
		LLMMaxOutputTokens: l.integer("LLM_MAX_OUTPUT_TOKENS", DefaultLLMMaxOutputTokens, 1, 100_000),

		LogLevel: l.logLevel("LOG_LEVEL", slog.LevelInfo),
	}
	l.errs = append(l.errs, c.validate()...)
	if len(l.errs) > 0 {
		return Config{}, fmt.Errorf("loading config: %w", errors.Join(l.errs...))
	}
	return c, nil
}

// validate checks rules that span fields or depend on required values.
func (c Config) validate() []error {
	var errs []error
	switch c.AppEnv {
	case "", EnvLocal, EnvCI, EnvStaging, EnvProd:
	default:
		errs = append(errs, fmt.Errorf("APP_ENV must be one of local, ci, staging, prod; got %q", c.AppEnv))
	}
	if c.AdminPassword != "" && len(c.AdminPassword) < MinAdminPasswordLen {
		errs = append(errs, fmt.Errorf("ADMIN_PASSWORD must be at least %d characters", MinAdminPasswordLen))
	}
	if c.SkewWarn > c.SkewMax {
		errs = append(errs, errors.New("SKEW_WARN_SECONDS must not exceed SKEW_MAX_SECONDS"))
	}
	if c.LLMEnabled && c.AnthropicAPIKey == "" {
		errs = append(errs, errors.New("ANTHROPIC_API_KEY is required when LLM_ENABLED=true"))
	}
	return errs
}

// LogValue implements slog.LogValuer so a logged Config never exposes secrets.
func (c Config) LogValue() slog.Value {
	return slog.GroupValue(
		slog.String("app_env", c.AppEnv),
		slog.Int("port", c.Port),
		slog.String("database_url", redactIfSet(c.DatabaseURL)),
		slog.String("database_url_test", redactIfSet(c.DatabaseURLTest)),
		slog.String("admin_user", c.AdminUser),
		slog.String("admin_password", redactIfSet(c.AdminPassword)),
		slog.String("kalshi_base_url", c.KalshiBaseURL),
		slog.String("polymarket_gamma_url", c.PolymarketGammaURL),
		slog.String("polymarket_clob_url", c.PolymarketClobURL),
		slog.String("universe_dir", c.UniverseDir),
		slog.String("facts_file", c.FactsFile),
		slog.String("eval_summary_file", c.EvalSummaryFile),
		slog.Duration("http_timeout", c.HTTPTimeout),
		slog.Int64("max_body_bytes", c.MaxBodyBytes),
		slog.Duration("stale_after", c.StaleAfter),
		slog.Duration("skew_warn", c.SkewWarn),
		slog.Duration("skew_max", c.SkewMax),
		slog.String("min_route_confidence", c.MinRouteConfidence.String()),
		slog.String("min_book_depth", c.MinBookDepth.String()),
		slog.Int("public_rate_limit_per_min", c.PublicRateLimitPerMin),
		slog.Bool("llm_enabled", c.LLMEnabled),
		slog.String("anthropic_api_key", redactIfSet(c.AnthropicAPIKey)),
		slog.String("llm_model", c.LLMModel),
		slog.Int("llm_daily_call_cap", c.LLMDailyCallCap),
		slog.Int("llm_max_output_tokens", c.LLMMaxOutputTokens),
		slog.String("log_level", c.LogLevel.String()),
	)
}

func redactIfSet(s string) string {
	if s == "" {
		return ""
	}
	return redacted
}

// loader collects errors while reading variables so Load can report them all.
type loader struct {
	lookup LookupFunc
	errs   []error
}

func (l *loader) get(key string) (string, bool) {
	v, ok := l.lookup(key)
	v = strings.TrimSpace(v)
	return v, ok && v != ""
}

func (l *loader) fail(format string, args ...any) {
	l.errs = append(l.errs, fmt.Errorf(format, args...))
}

func (l *loader) required(key string) string {
	v, ok := l.get(key)
	if !ok {
		l.fail("%s is required", key)
	}
	return v
}

func (l *loader) optional(key, def string) string {
	if v, ok := l.get(key); ok {
		return v
	}
	return def
}

func (l *loader) integer(key string, def, minVal, maxVal int) int {
	v, ok := l.get(key)
	if !ok {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		l.fail("%s must be an integer; got %q", key, v)
		return def
	}
	if n < minVal || n > maxVal {
		l.fail("%s must be between %d and %d; got %d", key, minVal, maxVal, n)
		return def
	}
	return n
}

func (l *loader) seconds(key string, def int) time.Duration {
	return time.Duration(l.integer(key, def, 1, 86400)) * time.Second
}

func (l *loader) boolean(key string, def bool) bool {
	v, ok := l.get(key)
	if !ok {
		return def
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		l.fail("%s must be true or false; got %q", key, v)
		return def
	}
	return b
}

func (l *loader) probability(key, def string) decimal.Decimal {
	v, ok := l.get(key)
	if !ok {
		v = def
	}
	d, err := decimal.NewFromString(v)
	if err != nil {
		l.fail("%s must be a decimal; got %q", key, v)
		return decimal.RequireFromString(def)
	}
	if d.LessThan(decimal.Zero) || d.GreaterThan(decimal.NewFromInt(1)) {
		l.fail("%s must be between 0 and 1; got %s", key, d)
		return decimal.RequireFromString(def)
	}
	return d
}

func (l *loader) positiveDecimal(key, def string) decimal.Decimal {
	v, ok := l.get(key)
	if !ok {
		v = def
	}
	d, err := decimal.NewFromString(v)
	if err != nil {
		l.fail("%s must be a decimal; got %q", key, v)
		return decimal.RequireFromString(def)
	}
	if !d.IsPositive() {
		l.fail("%s must be positive; got %s", key, d)
		return decimal.RequireFromString(def)
	}
	return d
}

func (l *loader) httpURL(key, def string) string {
	v, ok := l.get(key)
	if !ok {
		return def
	}
	u, err := url.Parse(v)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		l.fail("%s must be an absolute http(s) URL; got %q", key, v)
		return def
	}
	return strings.TrimRight(v, "/")
}

func (l *loader) logLevel(key string, def slog.Level) slog.Level {
	v, ok := l.get(key)
	if !ok {
		return def
	}
	var lvl slog.Level
	switch strings.ToLower(v) {
	case "debug":
		lvl = slog.LevelDebug
	case "info":
		lvl = slog.LevelInfo
	case "warn":
		lvl = slog.LevelWarn
	case "error":
		lvl = slog.LevelError
	default:
		l.fail("%s must be one of debug, info, warn, error; got %q", key, v)
		return def
	}
	return lvl
}
