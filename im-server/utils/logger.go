package utils

import (
    "context"
    "fmt"
    "io"
    "log/slog"
    "os"
    "path/filepath"
    "strings"
    "sync"
    "time"
)

// LogLevel 日志级别
type LogLevel string

const (
    DebugLevel LogLevel = "debug"
    InfoLevel  LogLevel = "info"
    WarnLevel  LogLevel = "warn"
    ErrorLevel LogLevel = "error"
)

const (
    LogRetentionDays = 7 // 日志保留天数，硬编码7天
)

var DefaultLogger *Logger

type LogCfg struct {
    LogFormat string `yaml:"log_format" json:"log_format"`
    LogLevel  string `yaml:"log_level" json:"log_level"`
    LogDir    string `yaml:"log_dir" json:"log_dir"`
    LogFile   string `yaml:"log_file" json:"log_file"`
}

type colorWriter struct {
    w  io.Writer
    mu sync.Mutex
}

var (
    colorReset = "\x1b[0m"
    colorTime  = "\x1b[93m" // 时间：浅黄色 (Bright Yellow)
    colorDebug = "\x1b[36m" // 青色
    colorInfo  = "\x1b[32m" // 绿色
    colorWarn  = "\x1b[33m" // 黄色
    colorError = "\x1b[31m" // 红色
)

func (cw *colorWriter) Write(p []byte) (int, error) {
    cw.mu.Lock()
    defer cw.mu.Unlock()
    s := string(p)
    if strings.Contains(s, "ERROR") {
        s = colorError + s
    } else {
        s = colorTime + s
        s = strings.ReplaceAll(s, "DEBUG", colorDebug+"DEBUG"+colorReset)
        s = strings.ReplaceAll(s, "INFO", colorInfo+"INFO"+colorReset)
        s = strings.ReplaceAll(s, "WARN", colorWarn+"WARN"+colorReset)
    }
    return cw.w.Write([]byte(s))
}

// Logger 日志接口实现
type Logger struct {
    config      *LogCfg
    jsonLogger  *slog.Logger // 文件JSON输出
    textLogger  *slog.Logger // 控制台文本输出
    logFile     *os.File
    currentDate string        // 当前日期 YYYY-MM-DD
    mu          sync.RWMutex  // 读写锁保护
    ticker      *time.Ticker  // 定时器
    stopCh      chan struct{} // 停止信号
}

// configLogLevelToSlogLevel 将配置中的日志级别转换为slog.Level
func configLogLevelToSlogLevel(configLevel string) slog.Level {
    switch configLevel {
    case "DEBUG":
        return slog.LevelDebug
    case "INFO":
        return slog.LevelInfo
    case "WARN":
        return slog.LevelWarn
    case "ERROR":
        return slog.LevelError
    default:
        return slog.LevelInfo
    }
}

// NewLogger 创建新的日志记录器
func NewLogger(config *LogCfg) (*Logger, error) {
    if err := os.MkdirAll(config.LogDir, 0o755); err != nil {
        return nil, fmt.Errorf("创建日志目录失败: %v", err)
    }
    logPath := filepath.Join(config.LogDir, config.LogFile)
    file, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
    if err != nil {
        return nil, fmt.Errorf("打开日志文件失败: %v", err)
    }
    slogLevel := configLogLevelToSlogLevel(config.LogLevel)
    jsonHandler := slog.NewJSONHandler(file, &slog.HandlerOptions{Level: slogLevel})
    textHandler := slog.NewTextHandler(&colorWriter{w: os.Stdout}, &slog.HandlerOptions{Level: slogLevel})
    jsonLogger := slog.New(jsonHandler)
    textLogger := slog.New(textHandler)
    logger := &Logger{
        config:      config,
        jsonLogger:  jsonLogger,
        textLogger:  textLogger,
        logFile:     file,
        currentDate: time.Now().Format("2006-01-02"),
        stopCh:      make(chan struct{}),
    }
    logger.startRotationChecker()
    if DefaultLogger == nil {
        DefaultLogger = logger
    }
    return logger, nil
}

// startRotationChecker 启动定时检查器
func (l *Logger) startRotationChecker() {
    l.ticker = time.NewTicker(1 * time.Minute)
    go func() {
        for {
            select {
            case <-l.ticker.C:
                l.checkAndRotate()
            case <-l.stopCh:
                return
            }
        }
    }()
}

// checkAndRotate 检查并执行轮转
func (l *Logger) checkAndRotate() {
    today := time.Now().Format("2006-01-02")
    if today != l.currentDate {
        l.rotateLogFile(today)
        l.cleanOldLogs()
    }
}

// rotateLogFile 执行日志轮转
func (l *Logger) rotateLogFile(newDate string) {
    l.mu.Lock()
    defer l.mu.Unlock()
    if l.logFile != nil {
        l.logFile.Close()
    }
    logDir := l.config.LogDir
    currentLogPath := filepath.Join(logDir, l.config.LogFile)
    baseFileName := strings.TrimSuffix(l.config.LogFile, filepath.Ext(l.config.LogFile))
    ext := filepath.Ext(l.config.LogFile)
    archivedLogPath := filepath.Join(logDir, fmt.Sprintf("%s-%s%s", baseFileName, l.currentDate, ext))
    if _, err := os.Stat(currentLogPath); err == nil {
        if err := os.Rename(currentLogPath, archivedLogPath); err != nil {
            l.textLogger.Error("重命名日志文件失败", slog.String("error", err.Error()))
        }
    }
    file, err := os.OpenFile(currentLogPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
    if err != nil {
        l.textLogger.Error("创建新日志文件失败", slog.String("error", err.Error()))
        return
    }
    l.logFile = file
    l.currentDate = newDate
    jsonHandler := slog.NewJSONHandler(file, &slog.HandlerOptions{Level: configLogLevelToSlogLevel(l.config.LogLevel)})
    l.jsonLogger = slog.New(jsonHandler)
}

// cleanOldLogs 清理过期日志
func (l *Logger) cleanOldLogs() {
    l.mu.Lock()
    defer l.mu.Unlock()
    logDir := l.config.LogDir
    cutoff := time.Now().AddDate(0, 0, -LogRetentionDays)
    entries, err := os.ReadDir(logDir)
    if err != nil {
        l.textLogger.Error("读取日志目录失败", slog.String("error", err.Error()))
        return
    }
    baseFileName := strings.TrimSuffix(l.config.LogFile, filepath.Ext(l.config.LogFile))
    ext := filepath.Ext(l.config.LogFile)
    for _, e := range entries {
        name := e.Name()
        if strings.HasPrefix(name, baseFileName+"-") && strings.HasSuffix(name, ext) {
            parts := strings.Split(strings.TrimSuffix(strings.TrimPrefix(name, baseFileName+"-"), ext), "-")
            if len(parts) == 3 {
                dateStr := fmt.Sprintf("%s-%s-%s", parts[0], parts[1], parts[2])
                if t, err := time.Parse("2006-01-02", dateStr); err == nil && t.Before(cutoff) {
                    _ = os.Remove(filepath.Join(logDir, name))
                }
            }
        }
    }
}

// Close 关闭日志记录器
func (l *Logger) Close() error {
    l.mu.Lock()
    defer l.mu.Unlock()
    if l.ticker != nil {
        l.ticker.Stop()
    }
    close(l.stopCh)
    if l.logFile != nil {
        return l.logFile.Close()
    }
    return nil
}

// log 基础日志方法
func (l *Logger) log(level slog.Level, msg string, fields ...interface{}) {
    // 使用读锁保护并发访问
    l.mu.RLock()
    defer l.mu.RUnlock()

    // 构建slog属性（支持成对传参或map传参）
    var attrs []slog.Attr
    if len(fields) > 0 && fields[0] != nil {
        if fieldsMap, ok := fields[0].(map[string]interface{}); ok {
            for k, v := range fieldsMap {
                attrs = append(attrs, slog.Any(k, v))
            }
        } else {
            // 处理成对传参 key, value
            for i := 0; i < len(fields)-1; i += 2 {
                if key, ok := fields[i].(string); ok {
                    attrs = append(attrs, slog.Any(key, fields[i+1]))
                }
            }
        }
    }

    // 同时写入文件（JSON）和控制台（文本）
    ctx := context.Background()
    l.jsonLogger.LogAttrs(ctx, level, msg, attrs...)
    l.textLogger.LogAttrs(ctx, level, msg, attrs...)
}

func (l *Logger) Debug(msg string, args ...interface{}) {
    if containsFormatPlaceholders(msg) {
        l.log(slog.LevelDebug, fmt.Sprintf(msg, args...))
    } else {
        l.log(slog.LevelDebug, msg, args...)
    }
}

func containsFormatPlaceholders(s string) bool {
    return strings.Contains(s, "%")
}

func (l *Logger) Info(msg string, args ...interface{}) {
    if containsFormatPlaceholders(msg) {
        l.log(slog.LevelInfo, fmt.Sprintf(msg, args...))
    } else {
        l.log(slog.LevelInfo, msg, args...)
    }
}

func (l *Logger) Warn(msg string, args ...interface{}) {
    if containsFormatPlaceholders(msg) {
        l.log(slog.LevelWarn, fmt.Sprintf(msg, args...))
    } else {
        l.log(slog.LevelWarn, msg, args...)
    }
}

func (l *Logger) Error(msg string, args ...interface{}) {
    if containsFormatPlaceholders(msg) {
        l.log(slog.LevelError, fmt.Sprintf(msg, args...))
    } else {
        l.log(slog.LevelError, msg, args...)
    }
}