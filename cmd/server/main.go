package main

import (
	"log"
	"os"

	"github.com/Frank-svg-dev/conch-proxy/internal/cache"
	"github.com/Frank-svg-dev/conch-proxy/internal/config"
	"github.com/Frank-svg-dev/conch-proxy/internal/handler"
	"github.com/Frank-svg-dev/conch-proxy/internal/router"
)

func main() {
	cfg := config.Load()

	if cfg.UpstreamAPIKey == "" {
		log.Fatal("UPSTREAM_API_KEY environment variable is required (or OPENAI_API_KEY for compatibility)")
	}

	if cfg.EnableInterceptor && cfg.SLMAPIURL == "" {
		log.Fatal("SLM_API_URL environment variable is required when ENABLE_INTERCEPTOR=true")
	}

	if cfg.EnableInterceptor && cfg.SLMAPIKey == "" {
		log.Fatal("SLM_API_KEY environment variable is required when ENABLE_INTERCEPTOR=true")
	}

	// 初始化Redis
	if cfg.RedisAddr != "" {
		if err := cache.InitRedis(&cache.RedisConfig{
			Addr:     cfg.RedisAddr,
			Password: cfg.RedisPassword,
			DB:       cfg.RedisDB,
		}); err != nil {
			log.Printf("Warning: Redis initialization failed: %v, continuing without Redis", err)
		} else {
			log.Printf("Redis initialized successfully")
		}
	} else {
		log.Fatalf("Redis not configured, application crash")
	}

	openaiHandler := handler.NewOpenAIHandler(
		cfg.UpstreamAPIKey,
		cfg.UpstreamAPIURL,
		cfg.ProxyURL,
		cfg.EnableInterceptor,
		cfg.AgentSystemPrompt,
		cfg.SendSingleChunk,
		cfg.SLMAPIKey,
		cfg.SLMAPIURL,
		cfg.SLMModel,
	)

	r := router.Setup(openaiHandler)

	port := ":" + cfg.Port
	log.Printf("Starting server on port %s", port)
	log.Printf("Upstream API URL: %s", cfg.UpstreamAPIURL)
	if cfg.EnableInterceptor {
		log.Printf("SLM API URL: %s", cfg.SLMAPIURL)
		log.Printf("SLM model: %s", cfg.SLMModel)
		log.Printf("Stream interceptor enabled with Agent")
	}
	if cfg.ProxyURL != "" {
		log.Printf("Proxy URL: %s", cfg.ProxyURL)
	}

	if cfg.EnableTLS {
		log.Printf("TLS enabled with cert file: %s, key file: %s", cfg.TLSCertFile, cfg.TLSKeyFile)
		if err := r.RunTLS(port, cfg.TLSCertFile, cfg.TLSKeyFile); err != nil {
			log.Fatalf("Failed to start TLS server: %v", err)
			os.Exit(1)
		}
	} else {
		if err := r.Run(port); err != nil {
			log.Fatalf("Failed to start server: %v", err)
			os.Exit(1)
		}
	}
}
