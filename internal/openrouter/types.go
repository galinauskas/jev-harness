package openrouter

import (
	"encoding/json"
	"time"
)

// Message is a chat-completions message.
type Message struct {
	Role       string     `json:"role"` // system|user|assistant|tool
	Content    string     `json:"content"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
}

// ToolCall is a completed function call requested by the model.
type ToolCall struct {
	ID       string `json:"id"`
	Type     string `json:"type"` // "function"
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"` // JSON string
	} `json:"function"`
}

// ToolDef declares a tool for a chat request.
type ToolDef struct {
	Type     string `json:"type"` // "function"
	Function struct {
		Name        string          `json:"name"`
		Description string          `json:"description"`
		Parameters  json.RawMessage `json:"parameters"`
	} `json:"function"`
}

// Usage is token accounting from the final stream chunk.
type Usage struct {
	PromptTokens     int     `json:"prompt_tokens"`
	CompletionTokens int     `json:"completion_tokens"`
	TotalTokens      int     `json:"total_tokens"`
	Cost             float64 `json:"cost,omitempty"`
}

// ChatRequest is POST /api/v1/chat/completions.
type ChatRequest struct {
	Model    string    `json:"model"`
	Messages []Message `json:"messages"`
	Tools    []ToolDef `json:"tools,omitempty"`
	Stream   bool      `json:"stream"`
}

// StreamEvent is one item on the channel returned by ChatStream.
type StreamEvent struct {
	TextDelta string
	ToolCalls []ToolCall // complete, only on Done
	Finish    string     // finish_reason on Done: "stop" | "tool_calls" | "length" | "error"
	Usage     *Usage     // on Done when present
	Err       error
	Done      bool
	// Started is when the stream's HTTP request was sent; set on Done so the
	// caller can compute response duration / tokens-per-second.
	Started time.Time
}

// ChoiceQuestion is the choice primitive of the Decisions API.
type ChoiceQuestion struct {
	Type         string            `json:"type"` // "choice"
	Instructions string            `json:"instructions"`
	Criteria     map[string]string `json:"criteria"`
}

// DecisionsRequest is POST /api/alpha/decisions.
type DecisionsRequest struct {
	Model     string                    `json:"model"`
	State     any                       `json:"state"`
	Questions map[string]ChoiceQuestion `json:"questions"`
}

// ChoiceAnswer is one answer in a DecisionsResponse.
type ChoiceAnswer struct {
	Type          string             `json:"type"`
	Choice        string             `json:"choice"`
	Confidence    *float64           `json:"confidence"` // optional per schema
	Probabilities map[string]float64 `json:"probabilities"`
}

// DecisionsResponse is the body of POST /api/alpha/decisions.
type DecisionsResponse struct {
	ID      string                  `json:"id"`
	Model   string                  `json:"model"`
	Answers map[string]ChoiceAnswer `json:"answers"`
	Usage   struct {
		InputTokens int     `json:"input_tokens"`
		Cost        float64 `json:"cost"`
	} `json:"usage"`
}

// apiError is the {"error":{"code","message"}} envelope on non-2xx and
// mid-stream failures.
type apiError struct {
	Code    any    `json:"code"` // number or string depending on error kind
	Message string `json:"message"`
}

// streamChunk is one `data:` payload of a chat completion stream.
type streamChunk struct {
	Choices []struct {
		Delta struct {
			Content   string `json:"content"`
			ToolCalls []struct {
				Index    int    `json:"index"`
				ID       string `json:"id"`
				Type     string `json:"type"`
				Function struct {
					Name      string `json:"name"`
					Arguments string `json:"arguments"`
				} `json:"function"`
			} `json:"tool_calls"`
		} `json:"delta"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Usage *Usage    `json:"usage"`
	Err   *apiError `json:"error"`
}
