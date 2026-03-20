package router

import (
	"github.com/Frank-svg-dev/conch-proxy/internal/handler"
	"github.com/Frank-svg-dev/conch-proxy/internal/middleware"
	"github.com/gin-gonic/gin"
)

func Setup(openaiHandler *handler.OpenAIHandler) *gin.Engine {
	r := gin.Default()

	r.Use(middleware.Logger())
	r.Use(middleware.CORS())

	api := r.Group("/v1")
	{
		api.POST("/chat/completions", openaiHandler.ChatCompletion)
	}

	r.GET("/health", func(c *gin.Context) {
		c.JSON(200, gin.H{
			"status": "ok",
		})
	})

	return r
}
