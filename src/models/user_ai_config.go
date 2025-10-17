package models

import (
	"encoding/json"
	"time"

	"gorm.io/datatypes"
)

// UserAIConfig 用户AI配置表
type UserAIConfig struct {
	ID         uint   `json:"id" gorm:"primaryKey"`
	UserID     string `json:"user_id" gorm:"index;not null"`
	ConfigName string `json:"config_name" gorm:"not null"`

	// LLM配置
	LLMType     string  `json:"llm_type,omitempty"`    // "qwen", "chatglm", "ollama", "coze"
	ModelName   string  `json:"model_name,omitempty"`  // 模型名称
	APIKey      string  `json:"api_key,omitempty"`     // API密钥
	BaseURL     string  `json:"base_url,omitempty"`    // API基础URL
	MaxTokens   int     `json:"max_tokens,omitempty"`  // 最大token数
	Temperature float32 `json:"temperature,omitempty"` // 温度参数

	// Function Call配置
	FunctionName string         `json:"function_name,omitempty"`  // 函数名称
	Description  string         `json:"description,omitempty"`    // 函数描述
	Parameters   datatypes.JSON `json:"parameters,omitempty"`     // JSON格式的参数定义
	MCPServerURL string         `json:"mcp_server_url,omitempty"` // MCP服务器URL

	IsActive  bool      `json:"is_active" gorm:"default:true"`
	Priority  int       `json:"priority" gorm:"default:0"` // 优先级，数字越大优先级越高
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// UserSessionConfig 用户会话配置表
type UserSessionConfig struct {
	ID               uint           `json:"id" gorm:"primaryKey"`
	UserID           string         `json:"user_id" gorm:"index;not null"`
	SessionID        string         `json:"session_id" gorm:"index;not null"`
	DefaultLLMID     uint           `json:"default_llm_id,omitempty"`    // 默认LLM配置ID
	EnabledFunctions datatypes.JSON `json:"enabled_functions,omitempty"` // JSON数组，启用的函数列表
	SessionData      datatypes.JSON `json:"session_data,omitempty"`      // JSON格式的会话数据
	ExpiresAt        time.Time      `json:"expires_at"`
	CreatedAt        time.Time      `json:"created_at"`
	UpdatedAt        time.Time      `json:"updated_at"`
}

// TableName 指定UserAIConfig表名
func (UserAIConfig) TableName() string {
	return "user_ai_configs"
}

// TableName 指定UserSessionConfig表名
func (UserSessionConfig) TableName() string {
	return "user_session_configs"
}

// CreateAIConfigRequest 创建AI配置请求结构
type CreateAIConfigRequest struct {
	ConfigName   string                 `json:"config_name" binding:"required"`
	LLMType      string                 `json:"llm_type,omitempty"`
	ModelName    string                 `json:"model_name,omitempty"`
	APIKey       string                 `json:"api_key,omitempty"`
	BaseURL      string                 `json:"base_url,omitempty"`
	MaxTokens    int                    `json:"max_tokens,omitempty"`
	Temperature  float32                `json:"temperature,omitempty"`
	FunctionName string                 `json:"function_name" binding:"required,min=1"` // 不能为空，且长度必须大于0
	Description  string                 `json:"description,omitempty"`
	Parameters   map[string]interface{} `json:"parameters,omitempty"`
	MCPServerURL string                 `json:"mcp_server_url,omitempty"`
	Priority     int                    `json:"priority,omitempty"`
}

// UpdateAIConfigRequest 更新AI配置请求结构
type UpdateAIConfigRequest struct {
	ConfigName   *string                `json:"config_name,omitempty"`
	LLMType      *string                `json:"llm_type,omitempty"`
	ModelName    *string                `json:"model_name,omitempty"`
	APIKey       *string                `json:"api_key,omitempty"`
	BaseURL      *string                `json:"base_url,omitempty"`
	MaxTokens    *int                   `json:"max_tokens,omitempty"`
	Temperature  *float32               `json:"temperature,omitempty"`
	FunctionName *string                `json:"function_name,omitempty"`
	Description  *string                `json:"description,omitempty"`
	Parameters   map[string]interface{} `json:"parameters,omitempty"`
	MCPServerURL *string                `json:"mcp_server_url,omitempty"`
	Priority     *int                   `json:"priority,omitempty"`
	IsActive     *bool                  `json:"is_active,omitempty"`
}

// AIConfigResponse AI配置响应结构
type AIConfigResponse struct {
	ID           uint                   `json:"id"`
	UserID       string                 `json:"user_id"`
	ConfigName   string                 `json:"config_name"`
	LLMType      string                 `json:"llm_type,omitempty"`
	ModelName    string                 `json:"model_name,omitempty"`
	BaseURL      string                 `json:"base_url,omitempty"`
	MaxTokens    int                    `json:"max_tokens,omitempty"`
	Temperature  float32                `json:"temperature,omitempty"`
	FunctionName string                 `json:"function_name,omitempty"`
	Description  string                 `json:"description,omitempty"`
	Parameters   map[string]interface{} `json:"parameters,omitempty"`
	MCPServerURL string                 `json:"mcp_server_url,omitempty"`
	IsActive     bool                   `json:"is_active"`
	Priority     int                    `json:"priority"`
	CreatedAt    time.Time              `json:"created_at"`
	UpdatedAt    time.Time              `json:"updated_at"`
}

// ToResponse 将UserAIConfig转换为响应结构
func (c *UserAIConfig) ToResponse() *AIConfigResponse {
	resp := &AIConfigResponse{
		ID:           c.ID,
		UserID:       c.UserID,
		ConfigName:   c.ConfigName,
		LLMType:      c.LLMType,
		ModelName:    c.ModelName,
		BaseURL:      c.BaseURL,
		MaxTokens:    c.MaxTokens,
		Temperature:  c.Temperature,
		FunctionName: c.FunctionName,
		Description:  c.Description,
		MCPServerURL: c.MCPServerURL,
		IsActive:     c.IsActive,
		Priority:     c.Priority,
		CreatedAt:    c.CreatedAt,
		UpdatedAt:    c.UpdatedAt,
	}

	// 解析Parameters JSON
	if c.Parameters != nil {
		var params map[string]interface{}
		if err := json.Unmarshal(c.Parameters, &params); err == nil {
			resp.Parameters = params
		}
	}

	return resp
}
