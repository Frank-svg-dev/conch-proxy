package config

import (
	"os"
	"strconv"
)

type Config struct {
	Port              string
	OpenAIKey         string
	OpenAIURL         string
	ProxyURL          string
	EnableInterceptor bool
	AgentSystemPrompt string
	SendSingleChunk   bool
	EnableTLS         bool
	TLSCertFile       string
	TLSKeyFile        string
}

func Load() *Config {
	return &Config{
		Port:              getEnv("PORT", "8080"),
		OpenAIKey:         getEnv("OPENAI_API_KEY", ""),
		OpenAIURL:         getEnv("OPENAI_API_URL", "https://api.openai.com/v1"),
		ProxyURL:          getEnv("PROXY_URL", ""),
		EnableInterceptor: getEnvBool("ENABLE_INTERCEPTOR", false),
		AgentSystemPrompt: getEnv("AGENT_SYSTEM_PROMPT", AgentSystemPrompt),
		SendSingleChunk:   getEnvBool("SEND_SINGLE_CHUNK", false),
		EnableTLS:         getEnvBool("ENABLE_TLS", false),
		TLSCertFile:       getEnv("TLS_CERT_FILE", "server.crt"),
		TLSKeyFile:        getEnv("TLS_KEY_FILE", "server.key"),
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
