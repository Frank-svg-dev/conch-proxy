package agent

import (
	"context"

	"github.com/go-kratos/blades"
)

type ProcessingAgent struct {
	name          string
	description   string
	systemPrompt  string
	modelProvider blades.ModelProvider
}

func NewProcessingAgent(name, description, systemPrompt string, modelProvider blades.ModelProvider) *ProcessingAgent {
	return &ProcessingAgent{
		name:          name,
		description:   description,
		systemPrompt:  systemPrompt,
		modelProvider: modelProvider,
	}
}

func (pa *ProcessingAgent) Name() string {
	return pa.name
}

func (pa *ProcessingAgent) Description() string {
	return pa.description
}

func (pa *ProcessingAgent) Run(ctx context.Context, invocation *blades.Invocation) blades.Generator[*blades.Message, error] {
	messages := []*blades.Message{}

	if pa.systemPrompt != "" {
		messages = append(messages, blades.SystemMessage(pa.systemPrompt))
	}

	if invocation.Message != nil {
		messages = append(messages, invocation.Message)
	}

	if invocation.History != nil {
		messages = append(messages, invocation.History...)
	}

	req := &blades.ModelRequest{
		Messages: messages,
	}

	respGen := pa.modelProvider.NewStreaming(ctx, req)

	return func(yield func(*blades.Message, error) bool) {
		for resp, err := range respGen {
			if err != nil {
				yield(nil, err)
				return
			}
			if resp == nil || resp.Message == nil {
				return
			}

			// if len(resp.Message.Text()) > 100 {
			// 	log.Printf("[ProcessingAgent] Skipping long chunk: %d chars", len(resp.Message.Text()))
			// 	continue
			// }

			if !yield(resp.Message, nil) {
				return
			}
		}
	}
}
