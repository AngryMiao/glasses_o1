package gateway

import (
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"im-server/auth"
	imcfg "im-server/configs"
	"im-server/utils"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"google.golang.org/grpc"
)

// ImMessage IM消息结构体，用于双向流通信（JSON格式）
// 定义了客户端与AI服务之间的消息格式
// EventType 明确的事件类型定义，避免使用裸字符串
type EventType string

const (
	EventSessionOpen  EventType = "session_open"
	EventSessionClose EventType = "session_close"
	EventData         EventType = "data"
)

type ImMessage struct {
	Event       EventType         `json:"event"`                  // 事件类型（session_open, session_close, data等）
	SessionID   string            `json:"session_id"`             // 会话ID，用于标识唯一会话
	Headers     map[string]string `json:"headers,omitempty"`      // 请求头信息
	MessageType int               `json:"message_type,omitempty"` // WebSocket消息类型
	Payload     []byte            `json:"payload,omitempty"`      // 消息载荷数据
}

// IMGatewayService gRPC服务接口，用于服务注册时的类型校验
// 这是一个空接口，仅用于类型定义
type IMGatewayService interface{}

// IMGatewayServer IM网关服务器
// 集成了gRPC服务器和WebSocket服务器，作为客户端与AI服务之间的桥梁
type IMGatewayServer struct {
	config    *imcfg.Config       // 服务配置
	logger    *utils.Logger       // 日志记录器
	server    *grpc.Server        // gRPC服务器实例
	upgrader  *websocket.Upgrader // WebSocket升级器
	authToken *auth.AuthToken     // JWT认证处理器

	// 会话连接映射表：sessionID -> WebSocket连接
	// 使用sync.Map保证并发安全
	conns sync.Map

	// AI总线连接管理
	gatewayMu     sync.Mutex        // 保护AI总线连接的互斥锁
	gatewayStream grpc.ServerStream // AI总线的gRPC流连接
}

// NewIMGatewayServer 创建新的IM网关服务器实例
// cfg: 服务配置
// logger: 日志记录器
// 返回: 初始化完成的IMGatewayServer实例
func NewIMGatewayServer(cfg *imcfg.Config, logger *utils.Logger) *IMGatewayServer {
	return &IMGatewayServer{
		config: cfg,
		logger: logger,
		// 创建WebSocket升级器，允许所有来源的连接
		upgrader:  &websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }},
		authToken: auth.NewAuthToken(cfg.Server.Token), // 初始化JWT认证
	}
}

// Start 启动gRPC网关服务
// lis: 网络监听器
// 返回: 启动过程中的错误信息
func (s *IMGatewayServer) Start(lis net.Listener) error {
	// 创建gRPC服务器，使用默认编解码器
	// 依赖客户端传递 content-subtype="json"，并通过 encoding.RegisterCodec 注册 JSON
	s.server = grpc.NewServer()
	s.registerService() // 注册服务描述

	// 在单独的goroutine中启动gRPC服务器
	go func() {
		if err := s.server.Serve(lis); err != nil {
			s.logger.Error("gRPC 总线服务终止: %v", err)
		}
	}()
	return nil
}

// Stop 停止gRPC服务器
// 优雅地关闭服务器，等待所有连接完成
func (s *IMGatewayServer) Stop() {
	if s.server != nil {
		s.server.GracefulStop()
	}
}

// IsGatewayConnected 返回 AI 总线连接状态
func (s *IMGatewayServer) IsGatewayConnected() bool {
	s.gatewayMu.Lock()
	defer s.gatewayMu.Unlock()
	return s.gatewayStream != nil
}

// ActiveSessions 返回当前 WebSocket 会话数量
func (s *IMGatewayServer) ActiveSessions() int {
	count := 0
	s.conns.Range(func(_, _ interface{}) bool {
		count++
		return true
	})
	return count
}

// registerService 动态注册gRPC服务描述
// 定义了MessageGateway双向流服务
func (s *IMGatewayServer) registerService() {
	svc := &grpc.ServiceDesc{
		ServiceName: "im.IMGateway",           // 服务名称
		HandlerType: (*IMGatewayService)(nil), // 服务接口类型
		Streams: []grpc.StreamDesc{
			{
				StreamName:    "MessageGateway",        // 流方法名称
				ServerStreams: true,                    // 支持服务端流
				ClientStreams: true,                    // 支持客户端流
				Handler:       s.messageGatewayHandler, // 流处理函数
			},
		},
	}
	s.server.RegisterService(svc, s) // 注册服务到gRPC服务器
}

// messageGatewayHandler 处理AI总线的双向流连接
// srv: 服务实例（未使用）
// stream: gRPC双向流
// 返回: 处理过程中的错误信息
func (s *IMGatewayServer) messageGatewayHandler(srv interface{}, stream grpc.ServerStream) error {
	// 保存AI总线流连接（线程安全）
	s.gatewayMu.Lock()
	s.gatewayStream = stream
	s.gatewayMu.Unlock()
	s.logger.Info("AI 总线连接已建立")

	// 持续读取AI发送的下行消息并转发给客户端
	for {
		msg := &ImMessage{}
		if err := stream.RecvMsg(msg); err != nil {
			if err == io.EOF {
				s.logger.Warn("AI 总线连接关闭")
				return nil
			}
			s.logger.Error("读取 AI 总线消息失败: %v", err)
			return err
		}
		// 将AI消息转发给对应的WebSocket客户端
		s.forwardToClient(msg)
	}
}

// forwardToClient 将AI下行消息转发给对应的WebSocket客户端
// msg: 要转发的消息
func (s *IMGatewayServer) forwardToClient(msg *ImMessage) {
	// 验证消息有效性
	if msg == nil || msg.SessionID == "" {
		return
	}

	// 根据会话ID查找对应的WebSocket连接
	val, ok := s.conns.Load(msg.SessionID)
	if !ok {
		s.logger.Warn("收到下行消息但会话不存在: %s", msg.SessionID)
		return
	}

	conn := val.(*websocket.Conn)

	// 处理会话关闭事件
	if msg.Event == EventSessionClose {
		_ = conn.Close()
		s.conns.Delete(msg.SessionID) // 从连接表中移除
		return
	}

	// 确定WebSocket消息类型（默认为文本消息）
	mtype := websocket.TextMessage
	if msg.MessageType != 0 {
		mtype = msg.MessageType
	}

	// 向WebSocket客户端发送消息
	if err := conn.WriteMessage(mtype, msg.Payload); err != nil {
		s.logger.Warn("向会话 %s 写消息失败: %v", msg.SessionID, err)
	}
}

// HandleWebSocket WebSocket连接处理入口（HTTP升级）
// w: HTTP响应写入器
// r: HTTP请求
func (s *IMGatewayServer) HandleWebSocket(w http.ResponseWriter, r *http.Request) {
	// 兼容浏览器环境：支持通过URL查询参数传递Header信息
	if s.config.Transport.GrpcGateway.Browser {
		q := r.URL.Query()
		// 将查询参数转换为HTTP头
		if v := q.Get("device-id"); v != "" {
			r.Header.Set("Device-Id", v)
		}
		if v := q.Get("client-id"); v != "" {
			r.Header.Set("Client-Id", v)
		}
		if v := q.Get("session-id"); v != "" {
			r.Header.Set("Session-Id", v)
		}
		if v := q.Get("transport-type"); v != "" {
			r.Header.Set("Transport-Type", v)
		}
		if v := q.Get("token"); v != "" {
			r.Header.Set("Authorization", "Bearer "+v)
			r.Header.Set("Token", v)
		}
	}

	// JWT认证验证
	userID, err := s.verifyJWTAuth(r)
	if err != nil {
		s.logger.Warn("WebSocket 认证失败: %v device-id: %s", err, r.Header.Get("Device-Id"))
		http.Error(w, "Unauthorized: "+err.Error(), http.StatusUnauthorized)
		return
	}
	r.Header.Set("User-Id", fmt.Sprintf("%d", userID))

	// 确保AI总线已连接，否则拒绝WebSocket升级
	if !s.waitGatewayConnected(2 * time.Second) {
		s.logger.Warn("AI 总线未连接，拒绝 WS 升级: device=%s", r.Header.Get("Device-Id"))
		http.Error(w, "Service Unavailable: AI gateway not connected", http.StatusServiceUnavailable)
		return
	}

	// 升级HTTP连接为WebSocket连接
	conn, err := s.upgrader.Upgrade(w, r, nil)
	if err != nil {
		s.logger.Error("WebSocket 升级失败: %v", err)
		return
	}

	// 获取或生成会话ID
	sessionID := r.Header.Get("Session-Id")
	if sessionID == "" {
		sessionID = uuid.NewString() // 生成新的UUID作为会话ID
	}
	s.conns.Store(sessionID, conn) // 保存会话连接映射

	s.logger.Info("WS 会话建立: session=%s device=%s", sessionID, r.Header.Get("Device-Id"))

	// 向AI总线发送会话开启通知
	s.gatewaySend(&ImMessage{
		Event:     EventSessionOpen,
		SessionID: sessionID,
		Headers: map[string]string{
			"Device-Id":      r.Header.Get("Device-Id"),
			"Client-Id":      r.Header.Get("Client-Id"),
			"Session-Id":     sessionID,
			"Transport-Type": r.Header.Get("Transport-Type"),
			"Authorization":  r.Header.Get("Authorization"),
			"User-Id":        r.Header.Get("User-Id"),
		},
	})

	// 启动消息读取循环：客户端 -> AI总线
	go func() {
		// 确保连接关闭时清理资源
		defer func() {
			s.conns.Delete(sessionID) // 从连接表中移除
			// 向AI总线发送会话关闭通知
			s.gatewaySend(&ImMessage{Event: EventSessionClose, SessionID: sessionID})
			_ = conn.Close() // 关闭WebSocket连接
		}()

		// 持续读取客户端消息并转发给AI总线
		for {
			mtype, data, err := conn.ReadMessage()
			if err != nil {
				// 检查是否为异常关闭
				if websocket.IsUnexpectedCloseError(err) {
					s.logger.Warn("WS 连接异常关闭: %v", err)
				}
				return
			}
			// 将客户端消息转发给AI总线
			s.gatewaySend(&ImMessage{
				Event:       EventData,
				SessionID:   sessionID,
				MessageType: mtype,
				Payload:     data,
			})
		}
	}()
}

// verifyJWTAuth 验证JWT令牌并返回用户ID
// r: HTTP请求
// 返回: 用户ID和可能的错误
func (s *IMGatewayServer) verifyJWTAuth(r *http.Request) (uint, error) {
	// 获取Authorization头
	authHeader := r.Header.Get("Authorization")
	if !strings.HasPrefix(authHeader, "Bearer ") {
		return 0, fmt.Errorf("缺少或无效的Authorization头")
	}

	// 提取JWT令牌
	token := authHeader[7:] // 去掉"Bearer "前缀

	// 验证JWT令牌
	isValid, deviceID, userID, err := s.authToken.VerifyToken(token)
	if err != nil || !isValid {
		return 0, fmt.Errorf("JWT token验证失败: %v", err)
	}

	// 验证设备ID是否匹配
	reqDevice := r.Header.Get("Device-Id")
	if reqDevice != deviceID {
		return 0, fmt.Errorf("设备ID与token不匹配: 请求=%s, token=%s", reqDevice, deviceID)
	}

	s.logger.Info("用户认证成功: userID=%d, deviceID=%s", userID, deviceID)
	return userID, nil
}

// gatewaySend 安全地向AI总线发送消息
// msg: 要发送的消息
func (s *IMGatewayServer) gatewaySend(msg *ImMessage) {
	s.gatewayMu.Lock()
	defer s.gatewayMu.Unlock()

	// 检查AI总线连接是否存在
	if s.gatewayStream == nil {
		s.logger.Warn("AI 总线未连接，消息丢弃: %s", msg.Event)
		return
	}

	// 发送消息到AI总线
	if err := s.gatewayStream.SendMsg(msg); err != nil {
		s.logger.Warn("向 AI 总线发送失败: %v", err)
	}
}

// waitGatewayConnected 在指定超时时间内等待AI总线连接建立
// timeout: 等待超时时间
// 返回: 是否在超时前建立了连接
func (s *IMGatewayServer) waitGatewayConnected(timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)

	// 轮询检查连接状态直到超时
	for time.Now().Before(deadline) {
		s.gatewayMu.Lock()
		connected := s.gatewayStream != nil
		s.gatewayMu.Unlock()

		if connected {
			return true
		}

		time.Sleep(100 * time.Millisecond) // 短暂休眠后重试
	}
	return false
}
