package models

import (
	//"gorm.io/gorm"
	"time"

	"gorm.io/datatypes"
)

// 系统全局配置（只保存一条记录）
type SystemConfig struct {
	ID               uint `gorm:"primaryKey"`
	SelectedASR      string
	SelectedTTS      string
	SelectedLLM      string
	SelectedVLLLM    string
	Prompt           string         `gorm:"type:text"`
	QuickReplyWords  datatypes.JSON // 存储为 JSON 数组
	DeleteAudio      bool
	UsePrivateConfig bool
}

// 用户
type User struct {
	ID       uint   `gorm:"primaryKey"`
	Username string `gorm:"uniqueIndex;not null"`
	Password string // 建议加密
	Role     string // 可选值：admin/user
	Setting  UserSetting
}

// 用户设置
type UserSetting struct {
	ID              uint `gorm:"primaryKey"`
	UserID          uint `gorm:"uniqueIndex"` // 一对一
	SelectedASR     string
	SelectedTTS     string
	SelectedLLM     string
	SelectedVLLLM   string
	PromptOverride  string `gorm:"type:text"`
	QuickReplyWords datatypes.JSON
}

// 模块配置（可选）
type ModuleConfig struct {
	ID          uint   `gorm:"primaryKey"`
	Name        string `gorm:"uniqueIndex;not null"` // 模块名
	Type        string
	ConfigJSON  datatypes.JSON
	Public      bool
	Description string
	Enabled     bool
}

type ServerConfig struct {
	ID     uint   `gorm:"primaryKey"`
	CfgStr string `gorm:"type:text"`
}

type DeviceBind struct {
	ID       uint      `gorm:"primaryKey;autoIncrement" json:"id"`
	DeviceID string    `gorm:"type:varchar(64);not null;uniqueIndex:uniq_device_binding" json:"device_id"`
	UserID   uint      `gorm:"not null;index" json:"user_id"`
	BindKey  string    `gorm:"type:varchar(512);not null" json:"binding_key"`
	IsActive bool      `gorm:"not null;default:true" json:"is_active"`
	CreateAt time.Time `gorm:"not null;default:CURRENT_TIMESTAMP" json:"created_at"`
	UpdateAt time.Time `gorm:"not null;default:CURRENT_TIMESTAMP" json:"updated_at"`
}
