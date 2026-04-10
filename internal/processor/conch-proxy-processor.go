package processor

import (
	"context"
	"encoding/json"
	"log"
	"math/rand"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Frank-svg-dev/conch-proxy/internal/agent"
	"github.com/Frank-svg-dev/conch-proxy/internal/cache"
	configv1 "github.com/Frank-svg-dev/conch-proxy/internal/config"
	"github.com/go-kratos/blades"
	"github.com/go-kratos/blades/contrib/openai"
)

// ToolCallAccumulator 用于累积单个工具调用的碎片数据
type ToolCallAccumulator struct {
	Index     int
	ID        string
	Type      string
	Name      string
	Arguments strings.Builder
	Complete  bool
}

type SentenceProcessor struct {
	fullResponse              strings.Builder
	reasoningBuffer           strings.Builder
	sentencePattern           *regexp.Regexp
	agent                     blades.Agent
	modelProvider             blades.ModelProvider
	mu                        sync.Mutex
	enableInterceptor         bool
	responseComplete          bool
	sendSingleChunk           bool
	toolAccumulators          map[int]*ToolCallAccumulator
	lastToolCallIndex         int
	sessionCounters           map[string]int       // Session计数器
	sessionUnlocks            map[string]bool      // Session解锁状态
	sessionUnlockTime         map[string]time.Time // Session解锁时间
	currentSessionID          string               // 当前请求SessionID
	unlockDuration            time.Duration        // 解锁有效期
	token                     string               // 校验Token
	originalEnableInterceptor bool                 // 原始拦截器开关
}

type ProcessorConfig struct {
	ModelProvider     blades.ModelProvider
	SLMAPIKey         string
	SLMAPIURL         string
	SLMModel          string
	SystemPrompt      string
	EnableInterceptor bool
	SendSingleChunk   bool
}

func NewSentenceProcessor(config *ProcessorConfig) (*SentenceProcessor, error) {
	modelProvider := config.ModelProvider
	if modelProvider == nil {
		modelName := config.SLMModel
		if modelName == "" {
			modelName = "gpt-4.1-mini"
		}

		modelProvider = openai.NewModel(modelName, openai.Config{
			APIKey:  config.SLMAPIKey,
			BaseURL: config.SLMAPIURL,
		})
	}

	systemPrompt := config.SystemPrompt
	if systemPrompt == "" {
		systemPrompt = os.Getenv("AGENT_SYSTEM_PROMPT")
		if systemPrompt == "" {
			systemPrompt = configv1.AgentSystemPrompt
		}
	}

	defaultAgent := agent.NewProcessingAgent(
		"default",
		"Default agent for processing sentences",
		systemPrompt,
		modelProvider,
	)

	unlockMinutes, err := strconv.Atoi(os.Getenv("UNLOCK_TTL_MINUTES"))
	if err != nil || unlockMinutes <= 0 {
		unlockMinutes = 60
	}

	return &SentenceProcessor{
		sentencePattern:           regexp.MustCompile(`[.!?。！？]+\s*$`),
		modelProvider:             modelProvider,
		agent:                     defaultAgent,
		enableInterceptor:         config.EnableInterceptor,
		originalEnableInterceptor: config.EnableInterceptor,
		sendSingleChunk:           config.SendSingleChunk,
		toolAccumulators:          make(map[int]*ToolCallAccumulator),
		lastToolCallIndex:         -1,
		sessionCounters:           make(map[string]int),
		sessionUnlocks:            make(map[string]bool),
		sessionUnlockTime:         make(map[string]time.Time),
		currentSessionID:          "default-session",
		unlockDuration:            time.Duration(unlockMinutes) * time.Minute,
		token:                     os.Getenv("SENSITIVE_TOKEN"),
	}, nil
}

func (sp *SentenceProcessor) ProcessChunk(chunk string, callback func(string)) {
	sp.mu.Lock()
	defer sp.mu.Unlock()

	if !sp.enableInterceptor {
		log.Printf("[ProcessChunk] Input chunk: %s", chunk)
	}

	var parsedChunk map[string]interface{}
	if err := json.Unmarshal([]byte(chunk), &parsedChunk); err != nil {
		if !sp.enableInterceptor {
			log.Printf("[ProcessChunk] Failed to parse JSON: %v", err)
		}

		var parsedArray []interface{}
		if arrayErr := json.Unmarshal([]byte(chunk), &parsedArray); arrayErr == nil {
			if !sp.enableInterceptor {
				log.Printf("[ProcessChunk] Detected array format with %d items", len(parsedArray))
			}
			for _, item := range parsedArray {
				if itemStr, ok := item.(string); ok {
					if !sp.enableInterceptor {
						log.Printf("[ProcessChunk] Processing array item: %s", itemStr)
					}
					sp.ProcessChunk(itemStr, callback)
				} else if itemMap, ok := item.(map[string]interface{}); ok {
					itemJson, _ := json.Marshal(itemMap)
					if !sp.enableInterceptor {
						log.Printf("[ProcessChunk] Processing array item (object): %s", string(itemJson))
					}
					sp.ProcessChunk(string(itemJson), callback)
				}
			}
			return
		}

		if sp.sendSingleChunk {
			callback(chunk)
		}
		return
	}

	if choices, ok := parsedChunk["choices"].([]interface{}); ok {
		if len(choices) == 0 {
			if !sp.enableInterceptor {
				log.Printf("[ProcessChunk] Empty choices array (usage info), skipping")
			}
			return
		}

		if choice, ok := choices[0].(map[string]interface{}); ok {
			if delta, ok := choice["delta"].(map[string]interface{}); ok {
				if len(delta) == 0 {
					if !sp.enableInterceptor {
						log.Printf("[ProcessChunk] Empty delta (finish signal), skipping")
					}
					return
				}

				content := sp.extractContentFromDelta(delta)
				if content != "" {
					sp.fullResponse.WriteString(content)
				}

				reasoningContent := sp.extractFieldFromDelta(delta, "reasoning_content")
				if reasoningContent != "" {
					// sp.reasoningBuffer.WriteString(reasoningContent)
					sseMsg := sp.formatAsSSE("", reasoningContent, "")
					callback(sseMsg)
				}

				// 处理工具调用 - 累积并实时发送
				toolCallsJSON := sp.extractToolCallsFromDelta(delta)
				if toolCallsJSON != "" {
					sp.processToolCalls(toolCallsJSON, callback)
				}

				finishReason := sp.extractFinishReason(choice)
				if finishReason != "" && finishReason != "null" {
					sp.responseComplete = true
					log.Printf("[ProcessChunk] Response complete with finish_reason: %s", finishReason)

					// 响应完成时发送所有累积的工具调用
					sp.flushToolCalls(callback)

					// 清空累积器
					sp.toolAccumulators = make(map[int]*ToolCallAccumulator)
					sp.lastToolCallIndex = -1
					return
				}

				if sp.enableInterceptor {
					return
				}
			}
		}
		if !sp.enableInterceptor {
			log.Printf("[ProcessChunk] Choices exist but no content to process, skipping")
		}
		return
	}

	if !sp.enableInterceptor {
		log.Printf("[ProcessChunk] No choices or empty choices, passing through: %s", chunk)
		callback(chunk)
	}
}

// processToolCalls 处理工具调用：只累积，不发送（等待 finish_reason）
func (sp *SentenceProcessor) processToolCalls(toolCallsJSON string, callback func(string)) {
	var toolCallsArray []interface{}
	if err := json.Unmarshal([]byte(toolCallsJSON), &toolCallsArray); err != nil {
		log.Printf("[processToolCalls] Unmarshal error: %v", err)
		return
	}

	// 检测是否是新的工具调用序列（通过比较第一个 tool_call 的 ID）
	if len(toolCallsArray) > 0 {
		if firstCall, ok := toolCallsArray[0].(map[string]interface{}); ok {
			if id, ok := firstCall["id"].(string); ok && id != "" {
				// 检查是否已经累积过这个 ID
				alreadyExists := false
				for _, acc := range sp.toolAccumulators {
					if acc.ID == id {
						alreadyExists = true
						break
					}
				}
				// 如果是全新的 ID 且已有累积，说明是新序列
				if !alreadyExists && len(sp.toolAccumulators) > 0 {
					log.Printf("[processToolCalls] New tool call sequence detected (new ID: %s), clearing accumulators", id)
					sp.toolAccumulators = make(map[int]*ToolCallAccumulator)
				}
			}
		}
	}

	for _, tc := range toolCallsArray {
		toolCall, ok := tc.(map[string]interface{})
		if !ok {
			continue
		}

		// 获取 index
		indexFloat, ok := toolCall["index"].(float64)
		if !ok {
			continue
		}
		index := int(indexFloat)

		// 更新最大 index
		if index > sp.lastToolCallIndex {
			sp.lastToolCallIndex = index
		}

		// 获取或创建累积器
		acc, exists := sp.toolAccumulators[index]
		if !exists {
			acc = &ToolCallAccumulator{Index: index}
			sp.toolAccumulators[index] = acc
		}

		// 累积字段（只累积非空值）
		if id, ok := toolCall["id"].(string); ok && id != "" {
			acc.ID = id
		}
		if toolType, ok := toolCall["type"].(string); ok && toolType != "" {
			acc.Type = toolType
		}

		// 处理 function 字段
		if fn, ok := toolCall["function"].(map[string]interface{}); ok {
			if name, ok := fn["name"].(string); ok && name != "" {
				acc.Name = name
			}
			if args, ok := fn["arguments"].(string); ok {
				acc.Arguments.WriteString(args)
				log.Printf("[processToolCalls] Accumulating args[%d]: %s", index, args)
			}
		}

		log.Printf("[processToolCalls] Accumulated tool call[%d]: id=%s, name=%s, args_len=%d",
			index, acc.ID, acc.Name, acc.Arguments.Len())
	}
	// 注意：这里不发送，等待 finish_reason 触发时统一发送
}

// buildToolCall 从累积器构建完整的工具调用对象
func (sp *SentenceProcessor) buildToolCall(acc *ToolCallAccumulator) map[string]interface{} {
	if acc.ID == "" || acc.Type == "" || acc.Name == "" {
		return nil // 必要字段不完整
	}

	return map[string]interface{}{
		"index": acc.Index,
		"id":    acc.ID,
		"type":  acc.Type,
		"function": map[string]interface{}{
			"name":      acc.Name,
			"arguments": acc.Arguments.String(),
		},
	}
}

// flushToolCalls 在 finish_reason 时发送所有累积的工具调用
func (sp *SentenceProcessor) flushToolCalls(callback func(string)) {
	if len(sp.toolAccumulators) == 0 {
		log.Printf("[flushToolCalls] No tool calls to flush")
		return
	}

	// 按 index 排序后输出
	var completeToolCalls []interface{}
	for i := 0; i <= sp.lastToolCallIndex; i++ {
		if acc, exists := sp.toolAccumulators[i]; exists {
			tc := sp.buildToolCall(acc)
			if tc != nil {
				completeToolCalls = append(completeToolCalls, tc)
				log.Printf("[flushToolCalls] Flushed tool call[%d]: %s", i, acc.ID)
			} else {
				log.Printf("[flushToolCalls] Incomplete tool call[%d]: id=%s, name=%s, type=%s, args_len=%d",
					i, acc.ID, acc.Name, acc.Type, acc.Arguments.Len())
			}
		}
	}

	if len(completeToolCalls) > 0 {
		jsonBytes, err := json.Marshal(completeToolCalls)
		if err != nil {
			log.Printf("[flushToolCalls] Marshal error: %v", err)
			return
		}

		result := sp.formatAsSSE("", "", string(jsonBytes))
		log.Printf("[flushToolCalls] Sending complete tool calls: %s", string(jsonBytes))
		callback(result)
	} else {
		log.Printf("[flushToolCalls] No complete tool calls to send")
	}
}

func (sp *SentenceProcessor) extractContentFromDelta(delta map[string]interface{}) string {
	return sp.extractFieldFromDelta(delta, "content")
}

func (sp *SentenceProcessor) extractFieldFromDelta(delta map[string]interface{}, fieldName string) string {
	if field, ok := delta[fieldName]; ok {
		switch v := field.(type) {
		case string:
			return v
		case []interface{}:
			var text strings.Builder
			for _, item := range v {
				if itemMap, ok := item.(map[string]interface{}); ok {
					if textContent, ok := itemMap["text"].(string); ok {
						text.WriteString(textContent)
					}
				}
			}
			return text.String()
		default:
			return ""
		}
	}
	return ""
}

func (sp *SentenceProcessor) extractToolCallsFromDelta(delta map[string]interface{}) string {
	if toolCalls, ok := delta["tool_calls"]; ok {
		switch v := toolCalls.(type) {
		case []interface{}:
			if len(v) > 0 {
				jsonBytes, err := json.Marshal(v)
				if err != nil {
					return ""
				}
				return string(jsonBytes)
			}
		default:
			return ""
		}
	}
	return ""
}

func (sp *SentenceProcessor) extractFinishReason(choice map[string]interface{}) string {
	if finishReason, ok := choice["finish_reason"]; ok {
		switch v := finishReason.(type) {
		case string:
			return v
		case nil:
			return "null"
		default:
			return ""
		}
	}
	return ""
}

func (sp *SentenceProcessor) Flush(callback func(string)) {
	sp.mu.Lock()
	defer sp.mu.Unlock()

	// 如果是工具调用完成的响应，跳过文本处理（工具调用已经在 ProcessChunk 中发送）
	if sp.responseComplete && len(sp.toolAccumulators) > 0 {
		log.Printf("[Flush] Tool calls response, skipping text processing")
		sp.fullResponse.Reset()
		sp.reasoningBuffer.Reset()
		sp.responseComplete = false
		return
	}

	if sp.fullResponse.Len() > 0 {
		fullResponse := sp.fullResponse.String()
		// reasoningSentence := sp.reasoningBuffer.String()

		log.Printf("[Flush] Flushing full response: %s", fullResponse)

		// 如果拦截器已关闭（口令校验通过），直接透传原始响应
		if !sp.enableInterceptor {
			log.Printf("[Flush] Interceptor disabled, passing through raw response")
			sp.simulateStreamOutput(fullResponse, "", callback)
		} else {
			if err := sp.processWithAgent(fullResponse, "", callback); err != nil {
				log.Printf("[Flush] Agent processing error: %v, falling back to original", err)
			}
		}

		sp.fullResponse.Reset()
		sp.reasoningBuffer.Reset()
		sp.responseComplete = false
	}
}

func (sp *SentenceProcessor) isCompleteSentence(text string) bool {
	return sp.sentencePattern.MatchString(text)
}

func (sp *SentenceProcessor) processWithAgent(sentence string, reasoningSentence string, callback func(string)) error {
	ctx := context.Background()

	if sp.agent != nil {
		invocation := &blades.Invocation{
			Instruction: blades.SystemMessage(configv1.AgentSystemPrompt),
			Message:     blades.UserMessage(sentence),
		}

		log.Printf("[processWithAgent] Sending to agent: %s", sentence)
		gen := sp.agent.Run(ctx, invocation)

		var finalContent string
		for msg, err := range gen {
			if err != nil {
				log.Printf("[processWithAgent] Agent error: %v", err)
				return err
			}
			if msg == nil {
				continue
			}

			content := sp.extractTextContent(msg)
			if content != "" {
				finalContent = content
			}
		}

		log.Printf("[processWithAgent] Stateless Agent full response: %s", finalContent)

		// 将脱敏内容(SLM处理后的内容)和原始内容存储到Redis
		if sentence != "" && finalContent != "" && sentence != finalContent {
			if err := cache.SetDesensitizedContent(ctx, finalContent, sentence); err != nil {
				log.Printf("[processWithAgent] Failed to store to Redis: %v", err)
			} else {
				log.Printf("[processWithAgent] Stored desensitized content to Redis, hash: %s", cache.ComputeSHA256(finalContent))
			}
		}

		// 检查敏感标签
		processedContent := sp.checkSensitiveTags(finalContent)

		sp.simulateStreamOutput(processedContent, reasoningSentence, callback)
	} else {
		result := sp.formatAsSSE(sentence, reasoningSentence, "")
		log.Printf("[processWithAgent] No agent, passing through: %s", sentence)
		log.Printf("[processWithAgent] Formatted SSE: %s", result)
		callback(result)
	}
	return nil
}

func (sp *SentenceProcessor) checkSensitiveTags(content string) string {
	isSensitive := strings.Contains(content, "[网络地址脱敏]") ||
		strings.Contains(content, "[凭证脱敏]") ||
		strings.Contains(content, "[人员脱敏]")

	if isSensitive {
		log.Printf("[checkSensitiveTags] Detected sensitive information in response")

		// 获取或生成session ID（这里使用固定ID，实际应该从请求中获取）
		sessionID := sp.getSessionID()

		// 增加计数器
		count := sp.incrementSessionCounter(sessionID)
		log.Printf("[checkSensitiveTags] Session %s triggered sensitive info, count: %d", sessionID, count)

		// 判断是否超过3次，且当前Session没有被解锁
		if count >= 3 && !sp.isSessionUnlocked(sessionID) {
			// 核心动作：向前端追加索要Token的提示
			warningMsg := "\n\n⚠️ [系统警告] 连续高频触碰敏感数据。如需查看明文，请回复口令（如：Token: Mengran123）。"

			// 第3次：在内容末尾追加警告信息
			if count == 3 {
				content += warningMsg
			} else {
				// 第4次及以上：直接返回警告信息，不返回原始内容
				content = "⚠️ [系统警告] 连续高频触碰敏感数据。如需查看明文，请回复口令（如：Token: Mengran123）。"
			}
		}
	}

	return content
}

func (sp *SentenceProcessor) getSessionID() string {
	if sp.currentSessionID != "" {
		return sp.currentSessionID
	}
	return "default-session"
}

func (sp *SentenceProcessor) incrementSessionCounter(sessionID string) int {
	if sp.sessionCounters == nil {
		sp.sessionCounters = make(map[string]int)
	}

	count := sp.sessionCounters[sessionID]
	count++
	sp.sessionCounters[sessionID] = count

	return count
}

func (sp *SentenceProcessor) isSessionUnlocked(sessionID string) bool {
	if sp.sessionUnlocks == nil {
		sp.sessionUnlocks = make(map[string]bool)
	}

	// 检查是否已解锁
	if !sp.sessionUnlocks[sessionID] {
		return false
	}

	// 检查解锁时间是否过期
	if unlockTime, exists := sp.sessionUnlockTime[sessionID]; exists {
		if time.Now().After(unlockTime) {
			log.Printf("[isSessionUnlocked] Session %s unlock time expired", sessionID)
			sp.sessionUnlocks[sessionID] = false
			delete(sp.sessionUnlockTime, sessionID)
			return false
		}
	}

	return true
}

func (sp *SentenceProcessor) checkAndUnlockSession(input string, sessionID string) bool {
	if sp.token == "" {
		log.Printf("[checkAndUnlockSession] No token configured")
		return false
	}

	// 检查输入是否包含Token
	tokenPattern := "口令: " + sp.token
	if strings.Contains(input, tokenPattern) {
		log.Printf("[checkAndUnlockSession] Token matched, unlocking session")
		// 解锁Session，按配置时长有效
		if sp.sessionUnlocks == nil {
			sp.sessionUnlocks = make(map[string]bool)
		}
		if sp.sessionUnlockTime == nil {
			sp.sessionUnlockTime = make(map[string]time.Time)
		}
		sp.sessionUnlocks[sessionID] = true
		sp.sessionUnlockTime[sessionID] = time.Now().Add(sp.unlockDuration)

		// 重置计数器
		if sp.sessionCounters != nil {
			sp.sessionCounters[sessionID] = 0
		}

		// 关闭拦截器，按配置时长后自动恢复
		sp.enableInterceptor = false
		log.Printf("[checkAndUnlockSession] Interceptor disabled, will re-enable after %s", sp.unlockDuration)

		// 启动定时器，按配置时长后恢复拦截器
		go func() {
			<-time.After(sp.unlockDuration)
			sp.mu.Lock()
			defer sp.mu.Unlock()
			sp.enableInterceptor = sp.originalEnableInterceptor
			log.Printf("[checkAndUnlockSession] Interceptor re-enabled after %s", sp.unlockDuration)
		}()

		return true
	}

	return false
}

func (sp *SentenceProcessor) CheckAndUnlockSession(input string, sessionID string) {
	sp.checkAndUnlockSession(input, sessionID)
}

func (sp *SentenceProcessor) SetCurrentSessionID(sessionID string) {
	sp.mu.Lock()
	defer sp.mu.Unlock()
	if strings.TrimSpace(sessionID) == "" {
		sp.currentSessionID = "default-session"
		return
	}
	sp.currentSessionID = sessionID
}

func (sp *SentenceProcessor) simulateStreamOutput(content string, reasoningSentence string, callback func(string)) {
	// // 直接发送完整内容，不再切分（避免碎片化）
	// result := sp.formatAsSSE(content, reasoningSentence, "")
	// log.Printf("[simulateStreamOutput] Sending complete response: %s", content)
	// callback(result)

	// // 1. 先发思考过程（如果有）
	// if reasoningSentence != "" {
	// 	reasoningResult := sp.formatAsSSE("", reasoningSentence, "")
	// 	callback(reasoningResult)
	// }

	// 2. 再发正式内容（如果有）
	if content != "" {
		contentResult := sp.formatAsSSE(content, "", "")
		callback(contentResult)
	}

	// 发送 finish_reason 标记结束
	finishResult := sp.formatAsSSE("", "", "")
	finishMap := make(map[string]interface{})
	if err := json.Unmarshal([]byte(finishResult), &finishMap); err == nil {
		if choices, ok := finishMap["choices"].([]interface{}); ok && len(choices) > 0 {
			if firstChoice, ok := choices[0].(map[string]interface{}); ok {
				firstChoice["finish_reason"] = "stop"
			}
		}
		if jsonData, err := json.Marshal(finishMap); err == nil {
			callback(string(jsonData))
		}
	}
}

func (sp *SentenceProcessor) extractTextContent(msg *blades.Message) string {
	if msg == nil {
		return ""
	}

	for _, part := range msg.Parts {
		if text, ok := part.(blades.TextPart); ok {
			return text.Text
		}
	}
	return ""
}

func (sp *SentenceProcessor) formatAsSSE(content string, reasoningContent string, toolCalls string) string {
	delta := map[string]interface{}{}

	if content != "" {
		delta["content"] = content
	}

	if reasoningContent != "" {
		delta["reasoning_content"] = reasoningContent
	}

	if toolCalls != "" {
		var toolCallsArray []interface{}
		if err := json.Unmarshal([]byte(toolCalls), &toolCallsArray); err == nil && len(toolCallsArray) > 0 {
			delta["tool_calls"] = toolCallsArray
		}
	}

	response := map[string]interface{}{
		"id":      "chatcmpl-" + generateID(),
		"object":  "chat.completion.chunk",
		"created": getCurrentTimestamp(),
		"model":   "gpt-3.5-turbo",
		"choices": []map[string]interface{}{
			{
				"index":         0,
				"delta":         delta,
				"finish_reason": nil,
			},
		},
	}

	jsonData, err := json.Marshal(response)
	if err != nil {
		return ""
	}
	return string(jsonData)
}

func generateID() string {
	return "proxy-" + randomString(16)
}

func getCurrentTimestamp() int64 {
	return time.Now().Unix()
}

func randomString(length int) string {
	const charset = "abcdefghijklmnopqrstuvwxyz0123456789"
	b := make([]byte, length)
	for i := range b {
		b[i] = charset[rand.Intn(len(charset))]
	}
	return string(b)
}

func (sp *SentenceProcessor) SetAgent(agent blades.Agent) {
	sp.mu.Lock()
	defer sp.mu.Unlock()
	sp.agent = agent
}
