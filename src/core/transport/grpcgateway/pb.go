package grpcgateway

// ImMessage 与 im-server 一致的消息结构
type ImMessage struct {
	Event       string            `json:"event"`
	SessionID   string            `json:"session_id"`
	Headers     map[string]string `json:"headers,omitempty"`
	MessageType int               `json:"message_type,omitempty"`
	Payload     []byte            `json:"payload,omitempty"`
}

// 常量事件名（可选）
const (
	EventSessionOpen  = "session_open"
	EventSessionClose = "session_close"
	EventData         = "data"
)
