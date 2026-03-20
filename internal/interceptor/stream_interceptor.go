package interceptor

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

type StreamInterceptor struct {
	processor Processor
}

type Processor interface {
	ProcessChunk(chunk string, callback func(string))
	Flush(callback func(string))
}

type SSEEvent struct {
	ID    string `json:"id,omitempty"`
	Event string `json:"event,omitempty"`
	Data  string `json:"data"`
	Retry int    `json:"retry,omitempty"`
}

func NewStreamInterceptor(processor Processor) *StreamInterceptor {
	return &StreamInterceptor{
		processor: processor,
	}
}

func (si *StreamInterceptor) InterceptStream(c *gin.Context, reader io.Reader, flusher http.Flusher) {
	scanner := bufio.NewScanner(reader)
	scanner.Split(bufio.ScanLines)

	for scanner.Scan() {
		line := scanner.Text()
		line = strings.TrimSpace(line)

		if line == "" {
			continue
		}

		if strings.HasPrefix(line, "data: ") {
			data := strings.TrimPrefix(line, "data: ")

			if data == "[DONE]" {
				log.Printf("[InterceptStream] Received [DONE], flushing processor")
				si.processor.Flush(func(result string) {
					log.Printf("[InterceptStream] Flush result: %s", result)
					if result != "" {
						c.Writer.WriteString("data: " + result + "\n\n")
						flusher.Flush()
					}
				})
				c.Writer.WriteString("data: [DONE]\n\n")
				flusher.Flush()
				return
			}

			si.processor.ProcessChunk(data, func(result string) {
				c.Writer.WriteString("data: " + result + "\n\n")
				flusher.Flush()
			})
		} else if line == "[DONE]" {
			log.Printf("[InterceptStream] Received standalone [DONE], flushing processor")
			si.processor.Flush(func(result string) {
				log.Printf("[InterceptStream] Flush result: %s", result)
				if result != "" {
					c.Writer.WriteString("data: " + result + "\n\n")
					flusher.Flush()
				}
			})
			c.Writer.WriteString("data: [DONE]\n\n")
			flusher.Flush()
			return
		}
	}

	if err := scanner.Err(); err != nil {
		log.Printf("[InterceptStream] Scanner error: %v", err)
		si.processor.Flush(func(result string) {
			log.Printf("[InterceptStream] Flush result: %s", result)
			if result != "" {
				c.Writer.WriteString("data: " + result + "\n\n")
				flusher.Flush()
			}
		})
	}
}

func ParseSSEEvent(line string) (*SSEEvent, error) {
	if !strings.HasPrefix(line, "data: ") {
		return nil, fmt.Errorf("invalid SSE event format")
	}

	data := strings.TrimPrefix(line, "data: ")
	if data == "[DONE]" {
		return &SSEEvent{Data: "[DONE]"}, nil
	}

	var event map[string]interface{}
	if err := json.Unmarshal([]byte(data), &event); err != nil {
		return nil, err
	}

	return &SSEEvent{Data: data}, nil
}
