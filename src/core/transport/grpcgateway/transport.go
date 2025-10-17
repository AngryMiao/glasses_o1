package grpcgateway

import (
	"context"
	"fmt"
	"net/http"
	"sync"

	"angrymiao-ai-server/src/configs"
	"angrymiao-ai-server/src/core/transport"
	"angrymiao-ai-server/src/core/utils"
)

// GrpcGatewayTransport 将 im-server 的 gRPC Gateway 作为传输层，对上复用现有 ConnectionHandlerFactory
type GrpcGatewayTransport struct {
	cfg         *configs.Config
	logger      *utils.Logger
	factory     transport.ConnectionHandlerFactory
	client      *IMGatewayClient
	cancel      context.CancelFunc
	wg          sync.WaitGroup
	handlers    sync.Map // sessionID -> transport.ConnectionHandler
	connections sync.Map // sessionID -> *GrpcConnection
}

func NewGrpcGatewayTransport(cfg *configs.Config, logger *utils.Logger, factory transport.ConnectionHandlerFactory) *GrpcGatewayTransport {
	return &GrpcGatewayTransport{cfg: cfg, logger: logger, factory: factory}
}

func (t *GrpcGatewayTransport) GetType() string { return "grpcgateway" }

func (t *GrpcGatewayTransport) SetConnectionHandler(factory transport.ConnectionHandlerFactory) {
	t.factory = factory
}

func (t *GrpcGatewayTransport) Start(ctx context.Context) error {
	if t.factory == nil {
		return fmt.Errorf("connection handler factory not set")
	}
	addr := fmt.Sprintf("%s:%d", t.cfg.Transport.GrpcGateway.IP, t.cfg.Transport.GrpcGateway.Port)
	cctx, cancel := context.WithCancel(ctx)
	t.cancel = cancel
	t.logger.Info("GrpcGatewayTransport connecting to %s", addr)
	client, err := NewIMGatewayClient(cctx, addr)
	if err != nil {
		return err
	}
	t.client = client

	t.wg.Add(1)
	go func() {
		defer t.wg.Done()
		for {
			msg, err := t.client.Recv()
			if err != nil {
				t.logger.Error("grpcgateway recv error: %v", err)
				return
			}
			t.handleIncoming(msg)
		}
	}()

	t.logger.Info("GrpcGatewayTransport connected to %s", addr)
	return nil
}

func (t *GrpcGatewayTransport) Stop() error {
	if t.cancel != nil {
		t.cancel()
	}
	if t.client != nil {
		_ = t.client.Close()
	}
	t.wg.Wait()
	// 关闭所有连接
	t.connections.Range(func(key, value any) bool {
		if c, ok := value.(*GrpcConnection); ok {
			_ = c.Close()
		}
		return true
	})
	return nil
}

func (t *GrpcGatewayTransport) GetActiveConnectionCount() int {
	count := 0
	t.connections.Range(func(_, _ any) bool { count++; return true })
	return count
}

func (t *GrpcGatewayTransport) handleIncoming(msg *ImMessage) {
	switch msg.Event {
	case EventSessionOpen:
		// 构造伪 HTTP 请求，传递头信息到处理器
		req := &http.Request{Header: http.Header{}}
		for k, v := range msg.Headers {
			req.Header.Set(k, v)
		}
		conn := NewGrpcConnection(msg.SessionID, req, t.client)
		handler := t.factory.CreateHandler(conn, req)
		t.connections.Store(msg.SessionID, conn)
		t.handlers.Store(msg.SessionID, handler)
		t.wg.Add(1)
		go func() {
			defer t.wg.Done()
			handler.Handle()
			t.connections.Delete(msg.SessionID)
			t.handlers.Delete(msg.SessionID)
		}()
	case EventData:
		if v, ok := t.connections.Load(msg.SessionID); ok {
			if c, ok := v.(*GrpcConnection); ok {
				c.PushIncoming(msg)
			}
		}
	case EventSessionClose:
		if v, ok := t.handlers.Load(msg.SessionID); ok {
			if h, ok := v.(transport.ConnectionHandler); ok {
				h.Close()
			}
		}
		if v, ok := t.connections.Load(msg.SessionID); ok {
			if c, ok := v.(*GrpcConnection); ok {
				_ = c.Close()
			}
		}
	default:
		// 其他事件直接作为数据透传
		if v, ok := t.connections.Load(msg.SessionID); ok {
			if c, ok := v.(*GrpcConnection); ok {
				c.PushIncoming(msg)
			}
		}
	}
}

// 编译期校验满足 Transport 接口
var _ transport.Transport = (*GrpcGatewayTransport)(nil)
