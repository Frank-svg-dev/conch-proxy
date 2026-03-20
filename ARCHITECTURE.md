# 流式拦截器架构设计文档

## 概述

本项目集成了 `go-kratos/blades` 框架，实现了一个智能的流式数据拦截和处理系统。该系统能够拦截OpenAI API返回的流式数据，将完整句子提取后通过Blades Agent进行处理，并以流式方式返回给用户。

## 架构设计

### 核心组件

```
┌─────────────────┐
│   Client Request │
└────────┬────────┘
         │
         ▼
┌─────────────────┐
│  Gin Router     │
│  /v1/chat/      │
│  completions    │
└────────┬────────┘
         │
         ▼
┌─────────────────┐
│ OpenAI Handler  │
│ - 检查stream参数 │
│ - 转发请求      │
└────────┬────────┘
         │
         ▼
┌─────────────────┐
│ Stream Interceptor│
│ - 拦截SSE数据   │
│ - 逐行处理      │
└────────┬────────┘
         │
         ▼
┌─────────────────┐
│ Sentence Processor│
│ - 缓冲数据      │
│ - 句子检测      │
│ - Agent调用     │
└────────┬────────┘
         │
         ▼
┌─────────────────┐
│   Blades Agent  │
│ - 处理完整句子  │
│ - 生成响应      │
└────────┬────────┘
         │
         ▼
┌─────────────────┐
│  Stream Response│
│  (SSE格式)      │
└─────────────────┘
```

### 组件说明

#### 1. StreamInterceptor (流式拦截器)
**文件**: `internal/interceptor/stream_interceptor.go`

**职责**:
- 拦截OpenAI API返回的SSE (Server-Sent Events) 流式数据
- 逐行解析SSE事件
- 将数据传递给Processor处理
- 将处理结果以SSE格式返回给客户端

**核心方法**:
```go
InterceptStream(c *gin.Context, reader io.Reader, flusher http.Flusher)
```

#### 2. SentenceProcessor (句子处理器)
**文件**: `internal/processor/sentence_processor.go`

**职责**:
- 缓冲流式数据片段
- 检测完整句子（基于标点符号）
- 将完整句子发送给Blades Agent处理
- 将Agent响应格式化为SSE格式

**核心方法**:
```go
ProcessChunk(chunk string, callback func(string))
Flush(callback func(string))
```

**句子检测规则**:
- 英文句号、问号、感叹号: `[.!?]+\s*$`
- 中文句号、问号、感叹号: `[。！？]+\s*$`

#### 3. SimpleAgent (简单Agent)
**文件**: `internal/agent/simple_agent.go`

**职责**:
- 实现Blades Agent接口
- 使用OpenAI ModelProvider处理输入
- 支持流式输出

**核心方法**:
```go
Run(ctx context.Context, invocation *blades.Invocation) blades.Generator[*blades.Message, error]
```

#### 4. AgentManager (Agent管理器)
**文件**: `internal/agent/manager.go`

**职责**:
- 管理多个Agent实例
- 提供Agent注册和查询功能
- 创建默认Agent

**核心方法**:
```go
RegisterAgent(name string, agent blades.Agent)
GetAgent(name string) (blades.Agent, bool)
CreateDefaultAgent(openaiKey, openaiURL string) error
```

## 工作流程

### 1. 标准输出流程
```
Client → Handler → OpenAI API → Client
```

### 2. 流式输出流程 (无拦截器)
```
Client → Handler → OpenAI API → Stream Response → Client
```

### 3. 流式输出流程 (启用拦截器)
```
Client → Handler → OpenAI API 
                          ↓
                    Stream Interceptor
                          ↓
                    Sentence Processor
                          ↓
                    Blades Agent
                          ↓
                    Stream Response → Client
```

## 配置说明

### 环境变量

| 变量名 | 说明 | 默认值 |
|--------|------|--------|
| `PORT` | 服务器端口 | `8080` |
| `OPENAI_API_KEY` | OpenAI API密钥 | (必需) |
| `OPENAI_API_URL` | OpenAI API地址 | `https://api.openai.com/v1` |
| `PROXY_URL` | 代理服务器地址 | (可选) |
| `ENABLE_INTERCEPTOR` | 是否启用流式拦截器 | `false` |
| `AGENT_SYSTEM_PROMPT` | Agent系统提示词 | `You are a helpful assistant...` |

### 启用拦截器

在 `.env` 文件中设置:
```bash
ENABLE_INTERCEPTOR=true
```

## 使用示例

### 1. 标准请求
```bash
curl -X POST http://localhost:8080/v1/chat/completions \
  -H "Content-Type: application/json" \
  -d '{
    "model": "gpt-3.5-turbo",
    "messages": [{"role": "user", "content": "Hello"}],
    "stream": false
  }'
```

### 2. 流式请求 (无拦截器)
```bash
curl -X POST http://localhost:8080/v1/chat/completions \
  -H "Content-Type: application/json" \
  -d '{
    "model": "gpt-3.5-turbo",
    "messages": [{"role": "user", "content": "Hello"}],
    "stream": true
  }'
```

### 3. 流式请求 (启用拦截器)
```bash
# 设置 ENABLE_INTERCEPTOR=true 后
curl -X POST http://localhost:8080/v1/chat/completions \
  -H "Content-Type: application/json" \
  -d '{
    "model": "gpt-3.5-turbo",
    "messages": [{"role": "user", "content": "Hello"}],
    "stream": true
  }'
```

## 扩展性

### 自定义Processor

实现 `Processor` 接口:
```go
type Processor interface {
    ProcessChunk(chunk string, callback func(string))
    Flush(callback func(string))
}
```

### 自定义Agent

实现 `blades.Agent` 接口:
```go
type Agent interface {
    Name() string
    Description() string
    Run(context.Context, *blades.Invocation) Generator[*Message, error]
}
```

### 注册自定义Agent

```go
agentManager := agent.NewAgentManager()
customAgent := NewCustomAgent(...)
agentManager.RegisterAgent("custom", customAgent)
```

## 技术特点

1. **非阻塞处理**: 流式数据实时处理，不阻塞客户端接收
2. **句子智能检测**: 支持中英文标点符号识别
3. **Agent灵活性**: 可插拔的Agent架构，支持多种处理逻辑
4. **向后兼容**: 可通过配置开关拦截器，不影响现有功能
5. **流式响应**: Agent处理结果也以流式方式返回

## 性能考虑

1. **缓冲区管理**: SentenceProcessor使用缓冲区累积数据，避免频繁的Agent调用
2. **并发安全**: 使用互斥锁保护共享状态
3. **资源清理**: 确保HTTP响应体正确关闭
4. **错误处理**: 完善的错误处理机制，确保系统稳定性

## 未来扩展

1. **多Agent支持**: 根据不同场景选择不同的Agent
2. **上下文管理**: 支持多轮对话的上下文传递
3. **性能监控**: 添加拦截器性能指标监控
4. **配置热更新**: 支持运行时动态配置Agent
5. **自定义句子检测**: 支持自定义句子结束符规则
