package websocket

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync"

	"angrymiao-ai-server/src/configs"
	"angrymiao-ai-server/src/core/auth"
	"angrymiao-ai-server/src/core/transport"
	"angrymiao-ai-server/src/core/utils"
	"angrymiao-ai-server/src/models"
	"angrymiao-ai-server/src/services"

	"github.com/gorilla/websocket"
)

// WebSocketTransport WebSocket传输层实现
type WebSocketTransport struct {
	config            *configs.Config
	server            *http.Server
	logger            *utils.Logger
	connHandler       transport.ConnectionHandlerFactory
	activeConnections sync.Map
	upgrader          *websocket.Upgrader
	authToken         *auth.AuthToken // JWT认证工具
	userConfigService services.UserAIConfigService
}

// NewWebSocketTransport 创建WebSocket传输层
func NewWebSocketTransport(config *configs.Config, logger *utils.Logger, userConfigService services.UserAIConfigService) *WebSocketTransport {
	return &WebSocketTransport{
		config: config,
		logger: logger,
		upgrader: &websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool {
				return true // 允许所有来源，生产环境应该更严格
			},
		},
		authToken:         auth.NewAuthToken(config.Server.Token), // 初始化JWT认证工具
		userConfigService: userConfigService,
	}
}

// Start 启动WebSocket传输层
func (t *WebSocketTransport) Start(ctx context.Context) error {
	addr := fmt.Sprintf("%s:%d", t.config.Transport.WebSocket.IP, t.config.Transport.WebSocket.Port)

	mux := http.NewServeMux()
	mux.HandleFunc("/", t.handleWebSocket)

	t.server = &http.Server{
		Addr:    addr,
		Handler: mux,
	}

	t.logger.Info("启动WebSocket传输层 ws://%s", addr)

	// 监听关闭信号
	go func() {
		<-ctx.Done()
		t.Stop()
	}()

	if err := t.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return fmt.Errorf("WebSocket传输层启动失败: %v", err)
	}

	return nil
}

// Stop 停止WebSocket传输层
func (t *WebSocketTransport) Stop() error {
	if t.server != nil {
		t.logger.Info("WebSocket传输层...")

		// 关闭所有活动连接
		t.activeConnections.Range(func(key, value interface{}) bool {
			if handler, ok := value.(transport.ConnectionHandler); ok {
				handler.Close()
			}
			t.activeConnections.Delete(key)
			return true
		})

		return t.server.Close()
	}
	return nil
}

// SetConnectionHandler 设置连接处理器工厂
func (t *WebSocketTransport) SetConnectionHandler(handler transport.ConnectionHandlerFactory) {
	t.connHandler = handler
}

// GetActiveConnectionCount 获取活跃连接数
func (t *WebSocketTransport) GetActiveConnectionCount() int {
	count := 0
	t.activeConnections.Range(func(key, value interface{}) bool {
		count++
		return true
	})
	return count
}

// GetType 获取传输类型
func (t *WebSocketTransport) GetType() string {
	return "websocket"
}

// verifyJWTAuth 验证JWT认证并返回用户ID
func (t *WebSocketTransport) verifyJWTAuth(r *http.Request) (uint, error) {
	// 获取Authorization头
	authHeader := r.Header.Get("Authorization")
	if !strings.HasPrefix(authHeader, "Bearer ") {
		return 0, fmt.Errorf("缺少或无效的Authorization头")
	}

	token := authHeader[7:] // 移除"Bearer "前缀

	// 验证JWT token
	isValid, deviceID, userID, err := t.authToken.VerifyToken(token)
	if err != nil || !isValid {
		return 0, fmt.Errorf("JWT token验证失败: %v", err)
	}

	// 检查设备ID匹配
	requestDeviceID := r.Header.Get("Device-Id")
	if requestDeviceID != deviceID {
		return 0, fmt.Errorf("设备ID与token不匹配: 请求=%s, token=%s", requestDeviceID, deviceID)
	}

	t.logger.Info("用户认证成功: userID=%d, deviceID=%s", userID, deviceID)

	return userID, nil
}

// handleWebSocket 处理WebSocket连接
func (t *WebSocketTransport) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	// 从URL参数中获取header信息（用于支持WebSocket连接时传递自定义header）
	if t.config.Transport.WebSocket.Browser {
		query := r.URL.Query()
		if deviceId := query.Get("device-id"); deviceId != "" {
			r.Header.Set("Device-Id", deviceId)
		}
		if clientId := query.Get("client-id"); clientId != "" {
			r.Header.Set("Client-Id", clientId)
		}
		if sessionId := query.Get("session-id"); sessionId != "" {
			r.Header.Set("Session-Id", sessionId)
		}
		if transportType := query.Get("transport-type"); transportType != "" {
			r.Header.Set("Transport-Type", transportType)
		}
		if token := query.Get("token"); token != "" {
			r.Header.Set("Authorization", "Bearer "+token)
			r.Header.Set("Token", token)
		}
	}

	// 验证JWT认证并获取用户ID
	userID, err := t.verifyJWTAuth(r)
	if err != nil {
		t.logger.Warn("WebSocket认证失败: %v device-id: %s", err, r.Header.Get("Device-Id"))
		http.Error(w, "Unauthorized: "+err.Error(), http.StatusUnauthorized)
		return
	}

	// 将用户ID添加到请求头中，供连接处理器使用
	r.Header.Set("User-Id", fmt.Sprintf("%d", userID))
	t.logger.Info("WebSocket认证成功: device-id=%s, user-id=%d", r.Header.Get("Device-Id"), userID)

	// 预加载用户配置，避免在ConnectionHandler中重复查询数据库
	userConfigs, err := t.preloadUserConfigs(fmt.Sprintf("%d", userID))
	if err != nil {
		t.logger.Warn("预加载用户配置失败: %v", err)
		// 不阻断连接，继续处理
	} else {
		t.logger.Info("成功预加载用户 %d 的配置，共 %d 个", userID, len(userConfigs))
	}

	// 将用户配置存储到请求上下文中
	ctx := context.WithValue(r.Context(), "user_configs", userConfigs)
	r = r.WithContext(ctx)

	conn, err := t.upgrader.Upgrade(w, r, nil)
	if err != nil {
		t.logger.Error("WebSocket升级失败: %v", err)
		return
	}

	clientID := fmt.Sprintf("%p", conn)
	t.logger.Info("收到WebSocket连接请求: %s", r.Header.Get("Device-Id"))
	wsConn := NewWebSocketConnection(clientID, conn)

	if t.connHandler == nil {
		t.logger.Error("连接处理器工厂未设置")
		conn.Close()
		return
	}

	handler := t.connHandler.CreateHandler(wsConn, r)
	if handler == nil {
		t.logger.Error("创建连接处理器失败")
		conn.Close()
		return
	}

	t.activeConnections.Store(clientID, handler)
	t.logger.Info("WebSocket客户端 %s 连接已建立，资源已分配", clientID)

	// 启动连接处理，并在结束时清理资源
	go func() {
		defer func() {
			// 连接结束时清理
			t.activeConnections.Delete(clientID)
			handler.Close()
		}()

		handler.Handle()
	}()
}

// preloadUserConfigs 预加载用户配置
func (t *WebSocketTransport) preloadUserConfigs(userID string) ([]*models.UserAIConfig, error) {
	// 获取用户的活跃Function Call配置
	configs, err := t.userConfigService.GetActiveConfigs(context.Background(), userID)
	if err != nil {
		return nil, fmt.Errorf("获取用户配置失败: %v", err)
	}

	return configs, nil
}
