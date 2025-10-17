package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"angrymiao-ai-server/src/configs"
	"angrymiao-ai-server/src/core/auth/am_token"
	"angrymiao-ai-server/src/core/providers/llm"
	"angrymiao-ai-server/src/core/types"
	"angrymiao-ai-server/src/core/utils"
	"angrymiao-ai-server/src/models"
	"angrymiao-ai-server/src/services"

	"github.com/gin-gonic/gin"
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

// AIConfigHandler AI配置处理器
type AIConfigHandler struct {
	configService services.UserAIConfigService
	logger        *utils.Logger
}

// NewAIConfigHandler 创建AI配置处理器
func NewAIConfigHandler(db *gorm.DB, logger *utils.Logger) *AIConfigHandler {
	return &AIConfigHandler{
		configService: services.NewUserAIConfigService(db, logger),
		logger:        logger,
	}
}

// RegisterRoutes 注册路由
func (h *AIConfigHandler) RegisterRoutes(apiGroup *gin.RouterGroup) {
	configGroup := apiGroup.Group("/ai-configs")
	// 添加JWT认证中间件
	configGroup.Use(h.authMiddleware())
	{
		configGroup.GET("", h.GetUserConfigs)
		configGroup.POST("", h.CreateConfig)
		configGroup.GET("/:id", h.GetConfigByID)
		configGroup.PUT("/:id", h.UpdateConfig)
		configGroup.DELETE("/:id", h.DeleteConfig)
		configGroup.PATCH("/:id/toggle", h.ToggleConfigStatus)
		configGroup.PATCH("/:id/priority", h.SetConfigPriority)
	}
}

// GetUserConfigs 获取用户配置列表
// @Summary 获取用户AI配置列表
// @Description 获取当前用户的所有AI配置
// @Tags AI配置管理
// @Accept json
// @Produce json
// @Success 200 {object} map[string]interface{} "成功"
// @Failure 400 {object} map[string]interface{} "请求参数错误"
// @Failure 500 {object} map[string]interface{} "服务器内部错误"
// @Router /api/ai-configs [get]
func (h *AIConfigHandler) GetUserConfigs(c *gin.Context) {
	userID := h.getUserID(c)
	configs, err := h.configService.GetUserConfigs(c.Request.Context(), userID)
	if err != nil {
		h.respondError(c, http.StatusInternalServerError, "获取配置失败", err)
		return
	}

	// 转换为响应格式
	var responses []*models.AIConfigResponse
	for _, config := range configs {
		responses = append(responses, config.ToResponse())
	}

	h.respondSuccess(c, gin.H{
		"configs": responses,
		"total":   len(responses),
	})
}

// CreateConfig 创建AI配置
// @Summary 创建AI配置
// @Description 创建新的AI配置
// @Tags AI配置管理
// @Accept json
// @Produce json
// @Param config body models.CreateAIConfigRequest true "配置信息"
// @Success 201 {object} map[string]interface{} "创建成功"
// @Failure 400 {object} map[string]interface{} "请求参数错误"
// @Failure 500 {object} map[string]interface{} "服务器内部错误"
// @Router /api/ai-configs [post]
func (h *AIConfigHandler) CreateConfig(c *gin.Context) {
	userID := h.getUserID(c)

	var req models.CreateAIConfigRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		h.respondError(c, http.StatusBadRequest, "请求参数格式错误", err)
		return
	}

	// 检查function name唯一性
	if req.FunctionName != "" {
		if err := h.configService.CheckFunctionNameUnique(c.Request.Context(), userID, req.FunctionName, 0); err != nil {
			h.respondError(c, http.StatusBadRequest, "Function Name唯一性校验失败", err)
			return
		}
	}

	// 构建配置对象
	config := &models.UserAIConfig{
		UserID:       userID,
		ConfigName:   req.ConfigName,
		LLMType:      req.LLMType,
		ModelName:    req.ModelName,
		APIKey:       req.APIKey,
		BaseURL:      req.BaseURL,
		MaxTokens:    req.MaxTokens,
		Temperature:  req.Temperature,
		FunctionName: req.FunctionName,
		Description:  req.Description,
		MCPServerURL: req.MCPServerURL,
		Priority:     req.Priority,
		IsActive:     true,
	}

	// 验证配置
	if err := h.configService.ValidateLLMConfig(c.Request.Context(), config); err != nil {
		h.respondError(c, http.StatusBadRequest, "LLM配置验证失败", err)
		return
	}

	// 处理参数JSON
	if req.Parameters != nil {
		parametersJSON, err := json.Marshal(req.Parameters)
		if err != nil {
			h.respondError(c, http.StatusBadRequest, "参数格式错误", err)
			return
		}
		config.Parameters = datatypes.JSON(parametersJSON)
	}

	if err := h.configService.CreateConfig(c.Request.Context(), config); err != nil {
		h.respondError(c, http.StatusInternalServerError, "创建配置失败", err)
		return
	}

	if req.Parameters == nil {
		go h.generateLLMFunctionParameters(config, config.ConfigName, config.Description)
	}

	h.logger.Info("用户 %s 创建AI配置成功: %s (ID: %d)", userID, config.ConfigName, config.ID)
	c.JSON(http.StatusCreated, gin.H{
		"code":    201,
		"message": "配置创建成功",
		"data":    config.ToResponse(),
	})
}

func (h *AIConfigHandler) generateLLMFunctionParameters(config *models.UserAIConfig, configName, description string) {
	h.logger.Info("为Function Call配置自动生成Parameters: %s", configName)

	generatedParams, err := h.generateParametersWithLLM(configName, description)
	if err != nil {
		h.logger.Warn("自动生成Parameters失败: %v", err)
	}
	parametersJSON, err := json.Marshal(generatedParams)
	if err != nil {
		h.logger.Info("生成的参数格式错误: %s", configName)
		return
	}
	config.Parameters = datatypes.JSON(parametersJSON)
	h.logger.Info("成功为Function Call配置生成Parameters: %s,%s", configName, string(parametersJSON))

	// 更新配置
	if err := h.configService.UpdateConfig(context.Background(), config); err != nil {
		h.logger.Error("更新配置参数失败: %v", err)
	}
	return
}

// generateParametersWithLLM 使用LLM生成Function Call的Parameters JSON Schema
func (h *AIConfigHandler) generateParametersWithLLM(configName, description string) (map[string]interface{}, error) {
	// 获取全局配置
	cfg := configs.Cfg
	if cfg == nil {
		return nil, fmt.Errorf("无法获取系统配置")
	}

	// 获取选定的LLM类型
	selectedLLM := cfg.SelectedModule["LLM"]
	if selectedLLM == "" {
		return nil, fmt.Errorf("未配置选定的LLM")
	}

	// 获取LLM配置
	llmConfig, exists := cfg.LLM[selectedLLM]
	if !exists {
		return nil, fmt.Errorf("找不到LLM配置: %s", selectedLLM)
	}

	// 创建LLM配置
	providerConfig := &llm.Config{
		Name:        selectedLLM,
		Type:        llmConfig.Type,
		ModelName:   llmConfig.ModelName,
		BaseURL:     llmConfig.BaseURL,
		APIKey:      llmConfig.APIKey,
		Temperature: llmConfig.Temperature,
		MaxTokens:   llmConfig.MaxTokens,
		TopP:        llmConfig.TopP,
		Extra:       llmConfig.Extra,
	}

	// 创建LLM提供者实例
	provider, err := llm.Create(llmConfig.Type, providerConfig)
	if err != nil {
		return nil, fmt.Errorf("创建LLM提供者失败: %v", err)
	}
	defer provider.Cleanup()

	// 构建提示词
	prompt := fmt.Sprintf(`请为以下Function Call生成合适的JSON Schema参数定义：

Function名称: %s
Function描述: %s

要求：
1. 返回标准的JSON Schema格式
2. 包含type、properties、required字段
3. 根据Function的名称和描述推断合理的参数
4. 只返回JSON格式，不要包含其他文字说明
5. 确保JSON格式正确且可解析

示例格式：
{
  "type": "object",
  "properties": {
    "param1": {
      "type": "string",
      "description": "参数1描述"
    },
    "param2": {
      "type": "integer",
      "description": "参数2描述"
    }
  },
  "required": ["param1"]
}`, configName, description)

	// 构建消息
	messages := []types.Message{
		{
			Role:    "user",
			Content: prompt,
		},
	}

	// 调用LLM
	ctx, cancel := context.WithTimeout(context.Background(), 30*1000000000) // 30秒超时
	defer cancel()

	responseChan, err := provider.Response(ctx, "param-gen", messages)
	if err != nil {
		return nil, fmt.Errorf("调用LLM失败: %v", err)
	}

	// 收集响应
	var responseText strings.Builder
	for chunk := range responseChan {
		responseText.WriteString(chunk)
	}

	response := strings.TrimSpace(responseText.String())
	fmt.Println("response", response)
	if response == "" {
		return nil, fmt.Errorf("LLM返回空响应")
	}

	// 尝试解析JSON
	var params map[string]interface{}
	if err := json.Unmarshal([]byte(response), &params); err != nil {
		// 如果直接解析失败，尝试提取JSON部分
		response = h.extractJSONFromResponse(response)
		if err := json.Unmarshal([]byte(response), &params); err != nil {
			return nil, fmt.Errorf("解析LLM响应JSON失败: %v, 响应内容: %s", err, response)
		}
	}

	// 验证JSON Schema基本结构
	if err := h.validateJSONSchema(params); err != nil {
		return nil, fmt.Errorf("生成的JSON Schema格式不正确: %v", err)
	}

	return params, nil
}

// extractJSONFromResponse 从响应中提取JSON部分
func (h *AIConfigHandler) extractJSONFromResponse(response string) string {
	// 查找第一个 { 和最后一个 }
	start := strings.Index(response, "{")
	end := strings.LastIndex(response, "}")

	if start != -1 && end != -1 && end > start {
		return response[start : end+1]
	}

	return response
}

// validateJSONSchema 验证JSON Schema基本结构
func (h *AIConfigHandler) validateJSONSchema(schema map[string]interface{}) error {
	// 检查必需的字段
	if _, ok := schema["type"]; !ok {
		return fmt.Errorf("缺少type字段")
	}

	if schemaType, ok := schema["type"].(string); !ok || schemaType != "object" {
		return fmt.Errorf("type字段必须为object")
	}

	// properties字段是可选的，但如果存在必须是对象
	if properties, exists := schema["properties"]; exists {
		if _, ok := properties.(map[string]interface{}); !ok {
			return fmt.Errorf("properties字段必须是对象")
		}
	}

	// required字段是可选的，但如果存在必须是数组
	if required, exists := schema["required"]; exists {
		if _, ok := required.([]interface{}); !ok {
			return fmt.Errorf("required字段必须是数组")
		}
	}

	return nil
}

// GetConfigByID 根据ID获取配置
// @Summary 获取配置详情
// @Description 根据配置ID获取配置详情
// @Tags AI配置管理
// @Accept json
// @Produce json
// @Param id path int true "配置ID"
// @Success 200 {object} map[string]interface{} "成功"
// @Failure 400 {object} map[string]interface{} "请求参数错误"
// @Failure 404 {object} map[string]interface{} "配置不存在"
// @Failure 500 {object} map[string]interface{} "服务器内部错误"
// @Router /api/ai-configs/{id} [get]
func (h *AIConfigHandler) GetConfigByID(c *gin.Context) {
	configID, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		h.respondError(c, http.StatusBadRequest, "无效的配置ID", err)
		return
	}

	config, err := h.configService.GetConfigByID(c.Request.Context(), uint(configID))
	if err != nil {
		if err.Error() == "配置不存在" {
			h.respondError(c, http.StatusNotFound, "配置不存在", err)
		} else {
			h.respondError(c, http.StatusInternalServerError, "获取配置失败", err)
		}
		return
	}

	h.respondSuccess(c, gin.H{
		"config": config.ToResponse(),
	})
}

// UpdateConfig 更新配置
// @Summary 更新AI配置
// @Description 更新指定的AI配置
// @Tags AI配置管理
// @Accept json
// @Produce json
// @Param id path int true "配置ID"
// @Param config body models.UpdateAIConfigRequest true "更新信息"
// @Success 200 {object} map[string]interface{} "更新成功"
// @Failure 400 {object} map[string]interface{} "请求参数错误"
// @Failure 404 {object} map[string]interface{} "配置不存在"
// @Failure 500 {object} map[string]interface{} "服务器内部错误"
// @Router /api/ai-configs/{id} [put]
func (h *AIConfigHandler) UpdateConfig(c *gin.Context) {
	configID, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		h.respondError(c, http.StatusBadRequest, "无效的配置ID", err)
		return
	}

	var req models.UpdateAIConfigRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		h.respondError(c, http.StatusBadRequest, "请求参数格式错误", err)
		return
	}

	// 获取现有配置
	config, err := h.configService.GetConfigByID(c.Request.Context(), uint(configID))
	if err != nil {
		if err.Error() == "配置不存在" {
			h.respondError(c, http.StatusNotFound, "配置不存在", err)
		} else {
			h.respondError(c, http.StatusInternalServerError, "获取配置失败", err)
		}
		return
	}

	// 更新字段
	if req.ConfigName != nil {
		config.ConfigName = *req.ConfigName
	}
	if req.LLMType != nil {
		config.LLMType = *req.LLMType
	}
	if req.ModelName != nil {
		config.ModelName = *req.ModelName
	}
	if req.APIKey != nil {
		config.APIKey = *req.APIKey
	}
	if req.BaseURL != nil {
		config.BaseURL = *req.BaseURL
	}
	if req.MaxTokens != nil {
		config.MaxTokens = *req.MaxTokens
	}
	if req.Temperature != nil {
		config.Temperature = *req.Temperature
	}
	if req.FunctionName != nil {
		// 检查function name唯一性（排除当前配置ID）
		if err := h.configService.CheckFunctionNameUnique(c.Request.Context(), config.UserID, *req.FunctionName, uint(configID)); err != nil {
			h.respondError(c, http.StatusBadRequest, "Function name已存在", err)
			return
		}
		config.FunctionName = *req.FunctionName
	}
	if req.Description != nil {
		config.Description = *req.Description
	}
	if req.MCPServerURL != nil {
		config.MCPServerURL = *req.MCPServerURL
	}
	if req.Priority != nil {
		config.Priority = *req.Priority
	}
	if req.IsActive != nil {
		config.IsActive = *req.IsActive
	}

	// 处理参数JSON
	if req.Parameters != nil {
		parametersJSON, err := json.Marshal(req.Parameters)
		if err != nil {
			h.respondError(c, http.StatusBadRequest, "参数格式错误", err)
			return
		}
		config.Parameters = datatypes.JSON(parametersJSON)
	}

	if err := h.configService.UpdateConfig(c.Request.Context(), config); err != nil {
		h.respondError(c, http.StatusInternalServerError, "更新配置失败", err)
		return
	}

	h.logger.Info("用户 %s 更新AI配置成功: %s (ID: %d)", config.UserID, config.ConfigName, config.ID)
	if req.Parameters == nil {
		go h.generateLLMFunctionParameters(config, config.ConfigName, config.Description)
	}
	h.respondSuccess(c, gin.H{
		"config": config.ToResponse(),
	})
}

// DeleteConfig 删除配置
// @Summary 删除AI配置
// @Description 删除指定的AI配置
// @Tags AI配置管理
// @Accept json
// @Produce json
// @Param id path int true "配置ID"
// @Success 200 {object} map[string]interface{} "删除成功"
// @Failure 400 {object} map[string]interface{} "请求参数错误"
// @Failure 404 {object} map[string]interface{} "配置不存在"
// @Failure 500 {object} map[string]interface{} "服务器内部错误"
// @Router /api/ai-configs/{id} [delete]
func (h *AIConfigHandler) DeleteConfig(c *gin.Context) {
	configID, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		h.respondError(c, http.StatusBadRequest, "无效的配置ID", err)
		return
	}

	if err := h.configService.DeleteConfig(c.Request.Context(), uint(configID)); err != nil {
		if err.Error() == "配置不存在" {
			h.respondError(c, http.StatusNotFound, "配置不存在", err)
		} else {
			h.respondError(c, http.StatusInternalServerError, "删除配置失败", err)
		}
		return
	}

	h.logger.Info("删除AI配置成功 (ID: %d)", configID)

	h.respondSuccess(c, gin.H{
		"message": "配置删除成功",
	})
}

// ToggleConfigStatus 切换配置状态
// @Summary 切换配置状态
// @Description 启用或禁用指定的AI配置
// @Tags AI配置管理
// @Accept json
// @Produce json
// @Param id path int true "配置ID"
// @Param status body map[string]bool true "状态信息"
// @Success 200 {object} map[string]interface{} "操作成功"
// @Failure 400 {object} map[string]interface{} "请求参数错误"
// @Failure 404 {object} map[string]interface{} "配置不存在"
// @Failure 500 {object} map[string]interface{} "服务器内部错误"
// @Router /api/ai-configs/{id}/toggle [patch]
func (h *AIConfigHandler) ToggleConfigStatus(c *gin.Context) {
	configID, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		h.respondError(c, http.StatusBadRequest, "无效的配置ID", err)
		return
	}

	var req struct {
		IsActive bool `json:"is_active" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		h.respondError(c, http.StatusBadRequest, "请求参数格式错误", err)
		return
	}

	if err := h.configService.ToggleConfigStatus(c.Request.Context(), uint(configID), req.IsActive); err != nil {
		if err.Error() == "配置不存在" {
			h.respondError(c, http.StatusNotFound, "配置不存在", err)
		} else {
			h.respondError(c, http.StatusInternalServerError, "切换配置状态失败", err)
		}
		return
	}

	status := "禁用"
	if req.IsActive {
		status = "启用"
	}

	h.logger.Info("配置状态切换成功 (ID: %d, 状态: %s)", configID, status)

	h.respondSuccess(c, gin.H{
		"message":   "配置状态切换成功",
		"is_active": req.IsActive,
	})
}

// SetConfigPriority 设置配置优先级
// @Summary 设置配置优先级
// @Description 设置指定AI配置的优先级
// @Tags AI配置管理
// @Accept json
// @Produce json
// @Param id path int true "配置ID"
// @Param priority body map[string]int true "优先级信息"
// @Success 200 {object} map[string]interface{} "操作成功"
// @Failure 400 {object} map[string]interface{} "请求参数错误"
// @Failure 404 {object} map[string]interface{} "配置不存在"
// @Failure 500 {object} map[string]interface{} "服务器内部错误"
// @Router /api/ai-configs/{id}/priority [patch]
func (h *AIConfigHandler) SetConfigPriority(c *gin.Context) {
	configID, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		h.respondError(c, http.StatusBadRequest, "无效的配置ID", err)
		return
	}

	var req struct {
		Priority int `json:"priority" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		h.respondError(c, http.StatusBadRequest, "请求参数格式错误", err)
		return
	}

	if err := h.configService.SetConfigPriority(c.Request.Context(), uint(configID), req.Priority); err != nil {
		if err.Error() == "配置不存在" {
			h.respondError(c, http.StatusNotFound, "配置不存在", err)
		} else {
			h.respondError(c, http.StatusInternalServerError, "设置优先级失败", err)
		}
		return
	}

	h.logger.Info("配置优先级设置成功 (ID: %d, 优先级: %d)", configID, req.Priority)

	h.respondSuccess(c, gin.H{
		"message":  "优先级设置成功",
		"priority": req.Priority,
	})
}

// 辅助方法

// authMiddleware JWT认证中间件
func (h *AIConfigHandler) authMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		// 验证认证
		authHeader := c.GetHeader("Authorization")
		if !strings.HasPrefix(authHeader, "Bearer ") {
			h.respondError(c, http.StatusUnauthorized, "无效的认证token或token已过期", nil)
			c.Abort()
			return
		}

		token := authHeader[7:] // 移除"Bearer "前缀

		// 使用Casbin进行JWT token验证
		claims, err := am_token.ParseToken(token)
		if err != nil {
			h.respondError(c, http.StatusUnauthorized, "token验证失败: "+err.Error(), err)
			c.Abort()
			return
		}

		// 将用户ID存储到上下文中
		c.Set("user_id", uint(claims.UserID))
		c.Set("jwt_claims", claims)

		c.Next()
	}
}

// getUserID 从上下文获取用户ID
func (h *AIConfigHandler) getUserID(c *gin.Context) string {
	// 从JWT认证中间件设置的上下文中获取用户ID
	if userID, exists := c.Get("user_id"); exists {
		if uid, ok := userID.(uint); ok {
			return strconv.FormatUint(uint64(uid), 10)
		}
	}

	// 如果没有找到用户ID，返回空字符串（这种情况不应该发生，因为有认证中间件）
	h.logger.Error("无法从上下文中获取用户ID")
	return ""
}

// respondSuccess 返回成功响应
func (h *AIConfigHandler) respondSuccess(c *gin.Context, data interface{}) {
	c.JSON(http.StatusOK, gin.H{
		"code":    200,
		"message": "操作成功",
		"data":    data,
	})
}

// respondError 返回错误响应
func (h *AIConfigHandler) respondError(c *gin.Context, statusCode int, message string, err error) {
	h.logger.Error("%s: %v", message, err)

	response := gin.H{
		"code":    statusCode,
		"message": message,
	}

	if err != nil {
		response["error"] = err.Error()
	}

	c.JSON(statusCode, response)
}
