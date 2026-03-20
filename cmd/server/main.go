package main

import (
	"log"
	"os"

	"github.com/Frank-svg-dev/conch-proxy/internal/config"
	"github.com/Frank-svg-dev/conch-proxy/internal/handler"
	"github.com/Frank-svg-dev/conch-proxy/internal/router"
)

func main() {
	cfg := config.Load()

	if cfg.OpenAIKey == "" {
		log.Fatal("OPENAI_API_KEY environment variable is required")
	}

	openaiHandler := handler.NewOpenAIHandler(cfg.OpenAIKey, cfg.OpenAIURL, cfg.ProxyURL, cfg.EnableInterceptor, cfg.AgentSystemPrompt, cfg.SendSingleChunk)

	r := router.Setup(openaiHandler)

	port := ":" + cfg.Port
	log.Printf("Starting server on port %s", port)
	log.Printf("OpenAI API URL: %s", cfg.OpenAIURL)
	if cfg.ProxyURL != "" {
		log.Printf("Proxy URL: %s", cfg.ProxyURL)
	}
	if cfg.EnableInterceptor {
		log.Printf("Stream interceptor enabled with Agent")
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
