package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	ServiceName        string
	Environment        string
	Addr               string
	ReadTimeout        time.Duration
	WriteTimeout       time.Duration
	IdleTimeout        time.Duration
	ShutdownTimeout    time.Duration
	ProviderTimeout    time.Duration
	DependencyRetry    time.Duration
	ProviderPriority   []string
	StooqBaseURL       string
	YFinancePython     string
	YFinanceScript     string
	OpenBBBaseURL      string
	DatabaseURL        string
	RedisAddr          string
	RedisPassword      string
	RedisDB            int
	CacheTTL           time.Duration
	FeedEnabled        bool
	FeedInterval       time.Duration
	RateLimitPerMinute int
	Version            string
	Commit             string
	BuildTime          string
	LogOutput          string
	LogFilePath        string
}

func Load() Config {
	return Config{
		ServiceName:        env("SERVICE_NAME", "market-data-service"),
		Environment:        env("ENVIRONMENT", "local"),
		Addr:               env("ADDR", ":8080"),
		ReadTimeout:        durationEnv("READ_TIMEOUT", 10*time.Second),
		WriteTimeout:       durationEnv("WRITE_TIMEOUT", 60*time.Second),
		IdleTimeout:        durationEnv("IDLE_TIMEOUT", 120*time.Second),
		ShutdownTimeout:    durationEnv("SHUTDOWN_TIMEOUT", 15*time.Second),
		ProviderTimeout:    durationEnv("PROVIDER_TIMEOUT", 30*time.Second),
		DependencyRetry:    durationEnv("DEPENDENCY_RETRY", 30*time.Second),
		ProviderPriority:   splitCSV(env("PROVIDER_PRIORITY", "stooq,yfinance,openbb")),
		StooqBaseURL:       env("STOOQ_BASE_URL", "https://stooq.com/q/d/l/"),
		YFinancePython:     env("YFINANCE_PYTHON", "python3"),
		YFinanceScript:     env("YFINANCE_SCRIPT", "/app/scripts/yfinance_fetch.py"),
		OpenBBBaseURL:      env("OPENBB_BASE_URL", ""),
		DatabaseURL:        env("DATABASE_URL", ""),
		RedisAddr:          env("REDIS_ADDR", ""),
		RedisPassword:      env("REDIS_PASSWORD", ""),
		RedisDB:            intEnv("REDIS_DB", 0),
		CacheTTL:           durationEnv("CACHE_TTL", 5*time.Minute),
		FeedEnabled:        boolEnv("FEED_ENABLED", true),
		FeedInterval:       durationEnv("FEED_INTERVAL", 12*time.Hour),
		RateLimitPerMinute: intEnv("RATE_LIMIT_PER_MINUTE", 120),
		Version:            env("VERSION", "dev"),
		Commit:             env("COMMIT_SHA", "unknown"),
		BuildTime:          env("BUILD_TIME", "unknown"),
		LogOutput:          strings.ToLower(env("LOG_OUTPUT", "both")),
		LogFilePath:        env("LOG_FILE_PATH", "logs/market-data-service.log"),
	}
}

func (c Config) Validate() error {
	if c.Addr == "" {
		return fmt.Errorf("ADDR is required")
	}
	if len(c.ProviderPriority) == 0 {
		return fmt.Errorf("PROVIDER_PRIORITY must contain at least one provider")
	}
	if c.ReadTimeout <= 0 || c.WriteTimeout <= 0 || c.IdleTimeout <= 0 || c.ShutdownTimeout <= 0 {
		return fmt.Errorf("server timeouts must be positive")
	}
	if c.ProviderTimeout <= 0 || c.DependencyRetry < 0 {
		return fmt.Errorf("provider timeout must be positive and dependency retry cannot be negative")
	}
	if c.RateLimitPerMinute <= 0 {
		return fmt.Errorf("RATE_LIMIT_PER_MINUTE must be positive")
	}
	if c.LogOutput != "stdout" && c.LogOutput != "file" && c.LogOutput != "both" {
		return fmt.Errorf("LOG_OUTPUT must be stdout, file, or both")
	}
	if (c.LogOutput == "file" || c.LogOutput == "both") && strings.TrimSpace(c.LogFilePath) == "" {
		return fmt.Errorf("LOG_FILE_PATH is required when file logging is enabled")
	}
	return nil
}

func env(key, fallback string) string {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	return value
}

func splitCSV(value string) []string {
	parts := strings.Split(value, ",")
	items := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.ToLower(strings.TrimSpace(part))
		if part != "" {
			items = append(items, part)
		}
	}
	return items
}

func durationEnv(key string, fallback time.Duration) time.Duration {
	value := env(key, "")
	if value == "" {
		return fallback
	}
	duration, err := time.ParseDuration(value)
	if err != nil {
		return fallback
	}
	return duration
}

func boolEnv(key string, fallback bool) bool {
	value := strings.ToLower(env(key, ""))
	if value == "" {
		return fallback
	}
	return value == "true" || value == "1" || value == "yes"
}

func intEnv(key string, fallback int) int {
	value := env(key, "")
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed < 0 {
		return fallback
	}
	return parsed
}
