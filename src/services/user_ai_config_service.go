package services

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"angrymiao-ai-server/src/core/utils"
	"angrymiao-ai-server/src/models"

	"gorm.io/datatypes"
	"gorm.io/gorm"
)

// UserAIConfigService 用户AI配置服务接口
type UserAIConfigService interface {
	// CRUD操作
	CreateConfig(ctx context.Context, config *models.UserAIConfig) error
	GetConfigByID(ctx context.Context, id uint) (*models.UserAIConfig, error)
	GetUserConfigs(ctx context.Context, userID string) ([]*models.UserAIConfig, error)
	UpdateConfig(ctx context.Context, config *models.UserAIConfig) error
	DeleteConfig(ctx context.Context, id uint) error

	// 业务逻辑
	GetActiveConfigs(ctx context.Context, userID string) ([]*models.UserAIConfig, error)
	SetConfigPriority(ctx context.Context, id uint, priority int) error
	ToggleConfigStatus(ctx context.Context, id uint, isActive bool) error

	// 配置验证
	ValidateLLMConfig(ctx context.Context, config *models.UserAIConfig) error
	CheckFunctionNameUnique(ctx context.Context, userID string, functionName string, excludeID uint) error

	// 缓存用户配置到会话
	CacheUserConfigsToSession(userID string, sessionID string) error

	// 从缓存获取用户配置
	GetCachedUserConfigs(userID string) ([]*models.UserAIConfig, error)
}

// DefaultUserAIConfigService 默认用户AI配置服务实现
type DefaultUserAIConfigService struct {
	db     *gorm.DB
	logger *utils.Logger
}

// NewUserAIConfigService 创建用户AI配置服务实例
func NewUserAIConfigService(db *gorm.DB, logger *utils.Logger) UserAIConfigService {
	return &DefaultUserAIConfigService{
		db:     db,
		logger: logger,
	}
}

// CreateConfig 创建用户AI配置
func (s *DefaultUserAIConfigService) CreateConfig(ctx context.Context, config *models.UserAIConfig) error {
	// 设置创建时间
	config.CreatedAt = time.Now()
	config.UpdatedAt = time.Now()

	if err := s.db.WithContext(ctx).Create(config).Error; err != nil {
		s.logger.Error("创建AI配置失败: %v", err)
		return err
	}

	s.logger.Info("用户 %s 创建AI配置成功: %s (ID: %d)", config.UserID, config.ConfigName, config.ID)
	return nil
}

// GetConfigByID 根据ID获取配置
func (s *DefaultUserAIConfigService) GetConfigByID(ctx context.Context, id uint) (*models.UserAIConfig, error) {
	var config models.UserAIConfig
	err := s.db.WithContext(ctx).First(&config, id).Error
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, fmt.Errorf("配置不存在")
		}
		return nil, err
	}
	return &config, nil
}

// GetUserConfigs 获取用户的所有配置
func (s *DefaultUserAIConfigService) GetUserConfigs(ctx context.Context, userID string) ([]*models.UserAIConfig, error) {
	var configs []*models.UserAIConfig
	query := s.db.WithContext(ctx).Where("user_id = ?", userID)

	err := query.Order("priority DESC, created_at DESC").Find(&configs).Error
	return configs, err
}

// UpdateConfig 更新配置
func (s *DefaultUserAIConfigService) UpdateConfig(ctx context.Context, config *models.UserAIConfig) error {
	// 验证配置
	if err := s.ValidateLLMConfig(ctx, config); err != nil {
		return fmt.Errorf("LLM配置验证失败: %v", err)
	}

	config.UpdatedAt = time.Now()

	if err := s.db.WithContext(ctx).Save(config).Error; err != nil {
		s.logger.Error("更新AI配置失败: %v", err)
		return err
	}

	s.logger.Info("用户 %s 更新AI配置成功: %s (ID: %d)", config.UserID, config.ConfigName, config.ID)
	return nil
}

// DeleteConfig 删除配置
func (s *DefaultUserAIConfigService) DeleteConfig(ctx context.Context, id uint) error {
	result := s.db.WithContext(ctx).Delete(&models.UserAIConfig{}, id)
	if result.Error != nil {
		s.logger.Error("删除AI配置失败: %v", result.Error)
		return result.Error
	}

	if result.RowsAffected == 0 {
		return fmt.Errorf("配置不存在")
	}

	s.logger.Info("删除AI配置成功 (ID: %d)", id)
	return nil
}

// GetActiveConfigs 获取用户的活跃配置
func (s *DefaultUserAIConfigService) GetActiveConfigs(ctx context.Context, userID string) ([]*models.UserAIConfig, error) {
	var configs []*models.UserAIConfig
	query := s.db.WithContext(ctx).Where("user_id = ? AND is_active = ?", userID, true)

	err := query.Order("priority DESC, created_at DESC").Find(&configs).Error
	return configs, err
}

// SetConfigPriority 设置配置优先级
func (s *DefaultUserAIConfigService) SetConfigPriority(ctx context.Context, id uint, priority int) error {
	result := s.db.WithContext(ctx).Model(&models.UserAIConfig{}).
		Where("id = ?", id).
		Updates(map[string]interface{}{
			"priority":   priority,
			"updated_at": time.Now(),
		})

	if result.Error != nil {
		return result.Error
	}

	if result.RowsAffected == 0 {
		return fmt.Errorf("配置不存在")
	}

	return nil
}

// ToggleConfigStatus 切换配置状态
func (s *DefaultUserAIConfigService) ToggleConfigStatus(ctx context.Context, id uint, isActive bool) error {
	result := s.db.WithContext(ctx).Model(&models.UserAIConfig{}).
		Where("id = ?", id).
		Updates(map[string]interface{}{
			"is_active":  isActive,
			"updated_at": time.Now(),
		})

	if result.Error != nil {
		return result.Error
	}

	if result.RowsAffected == 0 {
		return fmt.Errorf("配置不存在")
	}

	return nil
}

// ValidateLLMConfig 验证LLM配置
func (s *DefaultUserAIConfigService) ValidateLLMConfig(ctx context.Context, config *models.UserAIConfig) error {
	if config.FunctionName == "" {
		return fmt.Errorf("函数名称不能为空")
	}

	if config.Description == "" {
		return fmt.Errorf("函数描述不能为空")
	}

	if config.LLMType == "" {
		return fmt.Errorf("LLM类型不能为空")
	}

	if config.ModelName == "" {
		return fmt.Errorf("模型名称不能为空")
	}

	// 验证支持的LLM类型
	supportedTypes := []string{"qwen", "chatglm", "ollama", "coze", "openai", "claude"}
	isSupported := false
	for _, t := range supportedTypes {
		if strings.ToLower(config.LLMType) == t {
			isSupported = true
			break
		}
	}

	if !isSupported {
		return fmt.Errorf("不支持的LLM类型: %s", config.LLMType)
	}

	// 验证温度参数
	if config.Temperature < 0 || config.Temperature > 2 {
		return fmt.Errorf("温度参数必须在0-2之间")
	}

	// 验证最大token数
	if config.MaxTokens < 0 || config.MaxTokens > 32000 {
		return fmt.Errorf("最大token数必须在0-32000之间")
	}

	return nil
}

// CacheUserConfigsToSession 缓存用户配置到会话
func (s *DefaultUserAIConfigService) CacheUserConfigsToSession(userID string, sessionID string) error {
	configs, err := s.GetActiveConfigs(context.Background(), userID)
	if err != nil {
		return err
	}

	configData, err := json.Marshal(configs)
	if err != nil {
		return err
	}

	sessionConfig := &models.UserSessionConfig{
		UserID:      userID,
		SessionID:   sessionID,
		SessionData: datatypes.JSON(configData),
		ExpiresAt:   time.Now().Add(30 * time.Minute),
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}

	// 使用UPSERT操作，如果存在则更新，不存在则创建
	err = s.db.Where("user_id = ? AND session_id = ?", userID, sessionID).
		Assign(map[string]interface{}{
			"session_data": sessionConfig.SessionData,
			"expires_at":   sessionConfig.ExpiresAt,
			"updated_at":   sessionConfig.UpdatedAt,
		}).
		FirstOrCreate(sessionConfig).Error

	if err != nil {
		s.logger.Error("缓存用户配置到会话失败: %v", err)
		return err
	}

	s.logger.Debug("用户 %s 的配置已缓存到会话 %s", userID, sessionID)
	return nil
}

// GetCachedUserConfigs 从缓存获取用户配置
func (s *DefaultUserAIConfigService) GetCachedUserConfigs(userID string) ([]*models.UserAIConfig, error) {
	var sessionConfig models.UserSessionConfig
	err := s.db.Where("user_id = ? AND expires_at > ?", userID, time.Now()).
		Order("updated_at DESC").
		First(&sessionConfig).Error

	if err != nil {
		if err == gorm.ErrRecordNotFound {
			// 如果缓存不存在，直接从数据库获取
			return s.GetActiveConfigs(context.Background(), userID)
		}
		return nil, err
	}

	var configs []*models.UserAIConfig
	err = json.Unmarshal(sessionConfig.SessionData, &configs)
	if err != nil {
		s.logger.Warn("解析缓存的用户配置失败: %v", err)
		// 如果解析失败，从数据库重新获取
		return s.GetActiveConfigs(context.Background(), userID)
	}

	return configs, nil
}

// CheckFunctionNameUnique 检查function name在用户范围内是否唯一
func (s *DefaultUserAIConfigService) CheckFunctionNameUnique(ctx context.Context, userID string, functionName string, excludeID uint) error {
	if functionName == "" {
		return nil // 如果function name为空，不需要检查唯一性
	}

	var count int64
	query := s.db.WithContext(ctx).Model(&models.UserAIConfig{}).
		Where("user_id = ? AND function_name = ?", userID, functionName)

	// 如果是更新操作，排除当前记录
	if excludeID > 0 {
		query = query.Where("id != ?", excludeID)
	}

	if err := query.Count(&count).Error; err != nil {
		s.logger.Error("检查function name唯一性失败: %v", err)
		return fmt.Errorf("检查function name唯一性失败: %v", err)
	}

	if count > 0 {
		return fmt.Errorf("function name '%s' 已存在，请使用不同的名称", functionName)
	}

	return nil
}
