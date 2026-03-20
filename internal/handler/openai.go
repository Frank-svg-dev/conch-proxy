package handler

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/Frank-svg-dev/conch-proxy/internal/interceptor"
	"github.com/Frank-svg-dev/conch-proxy/internal/processor"
	"github.com/gin-gonic/gin"
)

type OpenAIHandler struct {
	apiKey      string
	apiURL      string
	proxyURL    string
	interceptor *interceptor.StreamInterceptor
	processor   *processor.SentenceProcessor
}

func NewOpenAIHandler(apiKey, apiURL, proxyURL string, enableInterceptor bool, agentSystemPrompt string, sendSingleChunk bool) *OpenAIHandler {
	var proc *processor.SentenceProcessor
	var streamInterceptor *interceptor.StreamInterceptor

	if enableInterceptor {
		procConfig := &processor.ProcessorConfig{
			OpenAIKey:         apiKey,
			OpenAIURL:         apiURL,
			EnableInterceptor: enableInterceptor,
			SendSingleChunk:   sendSingleChunk,
		}
		proc, _ = processor.NewSentenceProcessor(procConfig)
		streamInterceptor = interceptor.NewStreamInterceptor(proc)
	}

	return &OpenAIHandler{
		apiKey:      apiKey,
		apiURL:      apiURL,
		proxyURL:    proxyURL,
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
	// bodys, err := io.ReadAll(c.Request.Body)
	// if err != nil {
	// 	c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
	// 	return
	// }

	// fmt.Println(string(bodys))

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

	client := h.getHTTPClient()
	resp, err := client.Do(httpReq)
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
	// 检查最后一条用户消息是否包含Token，如果是则解锁Session
	// 只检查最后一条用户消息，避免消息历史中的过期口令重复触发解锁
	if h.processor != nil && len(req.Messages) > 0 {
		// 从后往前找最后一条用户消息
		for i := len(req.Messages) - 1; i >= 0; i-- {
			msg := req.Messages[i]
			if msg.Role == "user" {
				contentText := msg.GetContentText()
				if contentText != "" && strings.Contains(contentText, "口令: ") {
					h.processor.CheckAndUnlockSession(contentText)
				}
				break // 只检查最后一条用户消息
			}
		}
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

	client := h.getHTTPClient()
	resp, err := client.Do(httpReq)
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

	reader := resp.Body
	buf := make([]byte, 1024)

	for {
		n, err := reader.Read(buf)
		if err != nil {
			if err != io.EOF {
				fmt.Fprintf(c.Writer, "data: [ERROR] %s\n\n", err.Error())
			}
			break
		}

		data := string(buf[:n])
		lines := strings.Split(data, "\n")

		for _, line := range lines {
			if strings.HasPrefix(line, "data: ") {
				c.Writer.WriteString(line + "\n")
				flusher.Flush()
			}
		}
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

	if apiKey := c.GetHeader("Authorization"); apiKey != "" {
		req.Header.Set("Authorization", apiKey)
	}
}

func (h *OpenAIHandler) getHTTPClient() *http.Client {
	return &http.Client{}
}
