package port

import "context"

// Chat roles of an OpenAI-style conversation.
type ChatRole string

const (
	RoleSystem    ChatRole = "system"
	RoleUser      ChatRole = "user"
	RoleAssistant ChatRole = "assistant"
	RoleTool      ChatRole = "tool"
)

// ToolCall is one function the model asked to run.
type ToolCall struct {
	ID       string
	Name     string
	ArgsJSON string // arguments as a JSON object string, parsed by the caller
}

// ChatMessage is one turn of the conversation. ToolCalls is set on
// assistant messages that request tool runs; ToolCallID links a tool
// reply to the call it answers.
type ChatMessage struct {
	Role       ChatRole
	Content    string
	ToolCalls  []ToolCall
	ToolCallID string
}

// Tool describes a function the model may call. ParamsJSON is the JSON
// Schema of the arguments.
type Tool struct {
	Name        string
	Description string
	ParamsJSON  string
}

// ChatRequest is a provider-agnostic chat completion request. The model
// itself is a client setting, not a per-request choice.
type ChatRequest struct {
	Messages []ChatMessage
	Tools    []Tool
}

// ChatResponse is the model's answer: either text or tool calls.
type ChatResponse struct {
	Content   string
	ToolCalls []ToolCall
}

// LLM is the chat completion boundary. Adapters translate these types to
// the provider wire format.
type LLM interface {
	ChatComplete(ctx context.Context, req ChatRequest) (ChatResponse, error)
}
