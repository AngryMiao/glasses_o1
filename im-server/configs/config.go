package configs

import (
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// Config 定义 im-server 独立配置结构
type Config struct {
	// 仅保留 im-server 需要的配置项
	Server struct {
		Token string `yaml:"token" json:"token"`
	} `yaml:"server" json:"server"`

	Transport struct {
		WebSocket struct {
			Enabled bool   `yaml:"enabled" json:"enabled"`
			IP      string `yaml:"ip" json:"ip"`
			Port    int    `yaml:"port" json:"port"`
		} `yaml:"websocket" json:"websocket"`
		GrpcGateway struct {
			Browser bool   `yaml:"browser" json:"browser"`
			Enabled bool   `yaml:"enabled" json:"enabled"`
			IP      string `yaml:"ip" json:"ip"`
			Port    int    `yaml:"port" json:"port"`
		} `yaml:"grpcgateway" json:"grpcgateway"`
	} `yaml:"transport" json:"transport"`

	Log struct {
		LogFormat string `yaml:"log_format" json:"log_format"`
		LogLevel  string `yaml:"log_level" json:"log_level"`
		LogDir    string `yaml:"log_dir" json:"log_dir"`
		LogFile   string `yaml:"log_file" json:"log_file"`
	} `yaml:"log" json:"log"`
}

var (
	Cfg *Config
)

func (cfg *Config) ToString() string {
	data, _ := yaml.Marshal(cfg)
	return string(data)
}

func (cfg *Config) FromString(data string) error {
	return yaml.Unmarshal([]byte(data), cfg)
}

func (cfg *Config) setDefaults() {
	// 默认 WebSocket 与 gRPC 网关地址
	cfg.Transport.WebSocket.Enabled = true
	cfg.Transport.WebSocket.IP = "0.0.0.0"
	cfg.Transport.WebSocket.Port = 9000

	cfg.Transport.GrpcGateway.Enabled = true
	cfg.Transport.GrpcGateway.Browser = true
	cfg.Transport.GrpcGateway.IP = "0.0.0.0"
	cfg.Transport.GrpcGateway.Port = 9001

	// 默认 token
	cfg.Server.Token = "your_token"

	// 默认日志配置（兼容 utils.LogCfg）
	cfg.Log.LogDir = "logs"
	cfg.Log.LogLevel = "INFO"
	cfg.Log.LogFormat = "{time:YYYY-MM-DD HH:mm:ss} - {level} - {message}"
	cfg.Log.LogFile = "im-server.log"
}

// LoadConfig 从 im-server 专用路径加载配置
// 为了兼容当前 main.go 用法，保留一个无意义的参数位。
func LoadConfig() (*Config, string, error) {
	config := &Config{}

	// 相对仓库根路径的配置文件位置
	path := filepath.Join("config.yaml")

	data, err := os.ReadFile(path)
	if err != nil {
		// 文件不存在或读取失败时，使用默认配置
		config.setDefaults()
		data, _ = yaml.Marshal(config)
	} else {
		if err := yaml.Unmarshal(data, config); err != nil {
			// 解析失败则回退默认配置
			config.setDefaults()
		}
	}

	Cfg = config
	return config, path, nil
}
