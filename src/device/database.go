package device

import (
	"angrymiao-ai-server/src/configs/database"
	"angrymiao-ai-server/src/models"
	"time"

	"gorm.io/gorm"
)

// DeviceBindDB 设备绑定数据库操作结构体
type DeviceBindDB struct {
	db *gorm.DB
}

// NewDeviceBindDB 创建设备绑定数据库操作实例
func NewDeviceBindDB() *DeviceBindDB {
	return &DeviceBindDB{
		db: database.GetDB(),
	}
}

// SaveDeviceBind 保存设备绑定信息
func (d *DeviceBindDB) SaveDeviceBind(deviceID string, userID uint, bindKey string) error {
	// 检查设备是否已经绑定
	var existingBind models.DeviceBind
	err := d.db.Where("device_id = ? ", deviceID).First(&existingBind).Error
	if err == nil {
		// 设备已绑定，更新绑定信息
		existingBind.BindKey = bindKey
		existingBind.IsActive = true
		existingBind.UpdateAt = time.Now()
		return d.db.Save(&existingBind).Error
	} else if err != gorm.ErrRecordNotFound {
		return err
	}

	// 创建新的绑定记录
	newBind := models.DeviceBind{
		DeviceID: deviceID,
		UserID:   userID,
		BindKey:  bindKey,
		IsActive: true,
		CreateAt: time.Now(),
		UpdateAt: time.Now(),
	}

	return d.db.Create(&newBind).Error
}

// GetDeviceBind 根据设备ID获取绑定信息
func (d *DeviceBindDB) GetDeviceBind(deviceID string) (*models.DeviceBind, error) {
	var bind models.DeviceBind
	err := d.db.Where("device_id = ? AND is_active = ?", deviceID, true).First(&bind).Error
	if err != nil {
		return nil, err
	}
	return &bind, nil
}

// IsDeviceBound 检查设备是否已绑定
func (d *DeviceBindDB) IsDeviceBound(deviceID string) (bool, error) {
	var count int64
	err := d.db.Model(&models.DeviceBind{}).Where("device_id = ? AND is_active = ?", deviceID, true).Count(&count).Error
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

// UnbindDevice 解绑设备
func (d *DeviceBindDB) UnbindDevice(deviceID string, bindKey string) error {
	return d.db.Model(&models.DeviceBind{}).Where("device_id = ? AND bind_key = ?", deviceID, bindKey).Update("is_active", false).Error
}

// GetUserDevices 获取用户绑定的所有设备
func (d *DeviceBindDB) GetUserDevices(userID uint) ([]models.DeviceBind, error) {
	var devices []models.DeviceBind
	err := d.db.Where("user_id = ? AND is_active = ?", userID, true).Find(&devices).Error
	return devices, err
}
