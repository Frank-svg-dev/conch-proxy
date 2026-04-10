package handler

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/Frank-svg-dev/conch-proxy/internal/cache"
	"github.com/Frank-svg-dev/conch-proxy/internal/interceptor"
	"github.com/Frank-svg-dev/conch-proxy/internal/processor"
	"github.com/gin-gonic/gin"
)

type OpenAIHandler struct {
	apiKey      string
	apiURL      string
	proxyURL    string
	httpClient  *http.Client
	interceptor *interceptor.StreamInterceptor
	processor   *processor.SentenceProcessor
}

func NewOpenAIHandler(upstreamAPIKey, upstreamAPIURL, proxyURL string, enableInterceptor bool, agentSystemPrompt string, sendSingleChunk bool, slmAPIKey, slmAPIURL, slmModel string) *OpenAIHandler {
	var proc *processor.SentenceProcessor
	var streamInterceptor *interceptor.StreamInterceptor

	if enableInterceptor {
		procConfig := &processor.ProcessorConfig{
			SLMAPIKey:         slmAPIKey,
			SLMAPIURL:         slmAPIURL,
			SLMModel:          slmModel,
			EnableInterceptor: enableInterceptor,
			SendSingleChunk:   sendSingleChunk,
			SystemPrompt:      agentSystemPrompt,
		}
		proc, _ = processor.NewSentenceProcessor(procConfig)
		streamInterceptor = interceptor.NewStreamInterceptor(proc)
	}

	transport := &http.Transport{
		MaxIdleConns:        100,
		MaxIdleConnsPerHost: 20,
		IdleConnTimeout:     90 * time.Second,
	}

	return &OpenAIHandler{
		apiKey:      upstreamAPIKey,
		apiURL:      upstreamAPIURL,
		proxyURL:    proxyURL,
		httpClient:  &http.Client{Timeout: 20 * time.Minute, Transport: transport},
		interceptor: streamInterceptor,
		processor:   proc,
	}
}

type PathType struct {
	Description string `json:"description,omitempty"`
	Type        string `json:"type,omitempty"`
}

// ==================== 请求结构体 ====================

type ChatCompletionRequest struct {
	Model            string    `json:"model,omitempty"`
	Messages         []Message `json:"messages,omitempty"`
	Temperature      *float32  `json:"temperature,omitempty"`
	TopP             *float32  `json:"top_p,omitempty"`
	MaxTokens        *int      `json:"max_tokens,omitempty"`
	Stream           bool      `json:"stream,omitempty"`
	Stop             []string  `json:"stop,omitempty"`
	PresencePenalty  *float32  `json:"presence_penalty,omitempty"`
	FrequencyPenalty *float32  `json:"frequency_penalty,omitempty"`
	User             string    `json:"user,omitempty"`
	Tools            []Tool    `json:"tools,omitempty"`
	ToolChoice       string    `json:"tool_choice,omitempty"`
}

type Message struct {
	Role      string      `json:"role,omitempty"`
	Content   interface{} `json:"content,omitempty"`
	Name      string      `json:"name,omitempty"`
	ToolCalls []ToolCall  `json:"tool_calls,omitempty"`
}

func (m *Message) GetContentText() string {
	if m.Content == nil {
		return ""
	}

	switch v := m.Content.(type) {
	case string:
		return v
	case []interface{}:
		for _, item := range v {
			if content, ok := item.(map[string]interface{}); ok {
				if text, ok := content["text"].(string); ok {
					return text
				}
			}
		}
		return ""
	default:
		return ""
	}
}

type Tool struct {
	Type     string    `json:"type,omitempty"`
	Function *Function `json:"function,omitempty"`
}

type Function struct {
	Name        string                 `json:"name,omitempty"`
	Description string                 `json:"description,omitempty"`
	Parameters  map[string]interface{} `json:"parameters,omitempty"`
}

type ToolCall struct {
	Index    int           `json:"index,omitempty"`
	ID       string        `json:"id,omitempty"`
	Type     string        `json:"type,omitempty"`
	Function *FunctionCall `json:"function,omitempty"`
}

type FunctionCall struct {
	Name      string `json:"name,omitempty"`
	Arguments string `json:"arguments,omitempty"`
}

// ==================== 响应结构体 ====================

type ChatCompletionChunk struct {
	ID      string   `json:"id,omitempty"`
	Object  string   `json:"object,omitempty"`
	Created int64    `json:"created,omitempty"`
	Model   string   `json:"model,omitempty"`
	Choices []Choice `json:"choices,omitempty"`
	Usage   *Usage   `json:"usage,omitempty"`
}

type Choice struct {
	Index        int     `json:"index,omitempty"`
	Delta        Delta   `json:"delta,omitempty"`
	FinishReason *string `json:"finish_reason,omitempty"`
}

type Delta struct {
	Role      string     `json:"role,omitempty"`
	Content   *string    `json:"content,omitempty"`
	ToolCalls []ToolCall `json:"tool_calls,omitempty"`
}

type Usage struct {
	PromptTokens     int `json:"prompt_tokens,omitempty"`
	CompletionTokens int `json:"completion_tokens,omitempty"`
	TotalTokens      int `json:"total_tokens,omitempty"`
}

func (h *OpenAIHandler) ChatCompletion(c *gin.Context) {
	var req ChatCompletionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	fmt.Printf("Request: %+v\n", req)

	if req.Stream {
		h.handleStreamRequest(c, req)
		return
	}

	jsonData, err := json.Marshal(req)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	httpReq, err := http.NewRequest("POST", h.apiURL+"/chat/completions", bytes.NewBuffer(jsonData))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	h.setRequestHeaders(httpReq, c)

	resp, err := h.getHTTPClient().Do(httpReq)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.Data(resp.StatusCode, resp.Header.Get("Content-Type"), body)
}

func (h *OpenAIHandler) handleStreamRequest(c *gin.Context, req ChatCompletionRequest) {
	sessionID := h.getSessionID(c)
	if h.processor != nil {
		h.processor.SetCurrentSessionID(sessionID)
	}

	// 处理用户口令解锁逻辑
	if h.processor != nil && len(req.Messages) > 0 {
		for i := len(req.Messages) - 1; i >= 0; i-- {
			msg := req.Messages[i]
			if msg.Role == "user" {
				contentText := msg.GetContentText()
				if contentText != "" && strings.Contains(contentText, "口令: ") {
					h.processor.CheckAndUnlockSession(contentText, sessionID)
				}
				break
			}
		}
	}

	// 处理assistant消息，还原Redis中存储的原始内容
	h.restoreAssistantMessages(&req.Messages)

	jsonData, err := json.Marshal(req)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	httpReq, err := http.NewRequest("POST", h.apiURL+"/chat/completions", bytes.NewBuffer(jsonData))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	h.setRequestHeaders(httpReq, c)

	resp, err := h.getHTTPClient().Do(httpReq)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	defer resp.Body.Close()

	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")

	flusher, ok := c.Writer.(http.Flusher)
	if !ok {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "streaming not supported"})
		return
	}

	if h.interceptor != nil && h.processor != nil {
		h.interceptor.InterceptStream(c, resp.Body, flusher)
		return
	}

	h.streamSSEPassthrough(c, resp.Body, flusher)
}

func (h *OpenAIHandler) streamSSEPassthrough(c *gin.Context, reader io.Reader, flusher http.Flusher) {
	scanner := bufio.NewScanner(reader)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		if strings.HasPrefix(line, "data: ") {
			c.Writer.WriteString(line + "\n\n")
			flusher.Flush()
			if line == "data: [DONE]" {
				return
			}
		}
	}

	if err := scanner.Err(); err != nil {
		fmt.Fprintf(c.Writer, "data: [ERROR] %s\n\n", err.Error())
		flusher.Flush()
	}

	c.Writer.WriteString("data: [DONE]\n\n")
	flusher.Flush()
}

func (h *OpenAIHandler) setRequestHeaders(req *http.Request, c *gin.Context) {
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+h.apiKey)

	if h.proxyURL != "" {
		req.Header.Set("X-Proxy-URL", h.proxyURL)
	}
}

func (h *OpenAIHandler) getHTTPClient() *http.Client {
	return h.httpClient
}

func (h *OpenAIHandler) getSessionID(c *gin.Context) string {
	if sessionID := strings.TrimSpace(c.GetHeader("X-Session-ID")); sessionID != "" {
		return sessionID
	}

	auth := strings.TrimSpace(c.GetHeader("Authorization"))
	if auth != "" {
		return auth
	}

	return c.ClientIP()
}

func (h *OpenAIHandler) restoreAssistantMessages(messages *[]Message) {
	if messages == nil {
		return
	}
	ctx := context.Background()
	for i := range *messages {
		msg := (*messages)[i]
		if msg.Role == "assistant" {
			contentText := msg.GetContentText()
			if contentText != "" {
				originalContent, err := cache.GetOriginalContent(ctx, contentText)
				if err != nil {
					log.Printf("[restoreAssistantMessages] Failed to get from Redis: %v", err)
					continue
				}
				if originalContent != "" {
					log.Printf("[restoreAssistantMessages] Restored original content for message %d, hash: %s", i, cache.ComputeSHA256(contentText))
					(*messages)[i].Content = originalContent
				}
			}
		}
	}
}
