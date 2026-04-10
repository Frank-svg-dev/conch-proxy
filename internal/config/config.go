package config

import (
	"os"
	"strconv"
)

type Config struct {
	Port              string
	UpstreamAPIKey    string
	UpstreamAPIURL    string
	ProxyURL          string
	EnableInterceptor bool
	AgentSystemPrompt string
	SendSingleChunk   bool
	EnableTLS         bool
	TLSCertFile       string
	TLSKeyFile        string
	SLMAPIKey         string
	SLMAPIURL         string
	SLMModel          string
	RedisAddr         string
	RedisPassword     string
	RedisDB           int
}

func Load() *Config {
	upstreamURL := getEnv("UPSTREAM_API_URL", "")
	if upstreamURL == "" {
		upstreamURL = getEnv("OPENAI_API_URL", "https://api.openai.com/v1")
	}

	upstreamKey := getEnv("UPSTREAM_API_KEY", "")
	if upstreamKey == "" {
		upstreamKey = getEnv("OPENAI_API_KEY", "")
	}

	slmURL := getEnv("SLM_API_URL", "")
	slmKey := getEnv("SLM_API_KEY", "")

	redisAddr := getEnv("REDIS_ADDR", "")
	redisPassword := getEnv("REDIS_PASSWORD", "")
	redisDB := getEnvInt("REDIS_DB", 0)

	return &Config{
		Port:              getEnv("PORT", "8080"),
		UpstreamAPIKey:    upstreamKey,
		UpstreamAPIURL:    upstreamURL,
		ProxyURL:          getEnv("PROXY_URL", ""),
		EnableInterceptor: getEnvBool("ENABLE_INTERCEPTOR", false),
		AgentSystemPrompt: getEnv("AGENT_SYSTEM_PROMPT", AgentSystemPrompt),
		SendSingleChunk:   getEnvBool("SEND_SINGLE_CHUNK", false),
		EnableTLS:         getEnvBool("ENABLE_TLS", false),
		TLSCertFile:       getEnv("TLS_CERT_FILE", "server.crt"),
		TLSKeyFile:        getEnv("TLS_KEY_FILE", "server.key"),
		SLMAPIKey:         slmKey,
		SLMAPIURL:         slmURL,
		SLMModel:          getEnv("SLM_MODEL", "gpt-4.1-mini"),
		RedisAddr:         redisAddr,
		RedisPassword:     redisPassword,
		RedisDB:           redisDB,
	}
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

func getEnvBool(key string, defaultValue bool) bool {
	if value := os.Getenv(key); value != "" {
		if parsed, err := strconv.ParseBool(value); err == nil {
			return parsed
		}
	}
	return defaultValue
}

func getEnvInt(key string, defaultValue int) int {
	if value := os.Getenv(key); value != "" {
		if parsed, err := strconv.Atoi(value); err == nil {
			return parsed
		}
	}
	return defaultValue
}
