package grpcgateway

import (
	"fmt"
	"net/http"
	"sync/atomic"
	"time"
)

// GrpcConnection 适配 core.Connection，供上层连接处理器使用
type GrpcConnection struct {
	id         string
	req        *http.Request
	client     *IMGatewayClient
	incoming   chan *ImMessage
	closed     int32
	lastActive int64
}

func NewGrpcConnection(id string, req *http.Request, client *IMGatewayClient) *GrpcConnection {
	c := &GrpcConnection{
		id:       id,
		req:      req,
		client:   client,
		incoming: make(chan *ImMessage, 64),
	}
	c.touch()
	return c
}

func (c *GrpcConnection) touch() {
	atomic.StoreInt64(&c.lastActive, time.Now().Unix())
}

func (c *GrpcConnection) GetID() string {
	return c.id
}

func (c *GrpcConnection) GetType() string {
	return "grpcgateway"
}

func (c *GrpcConnection) WriteMessage(messageType int, data []byte) error {
	if atomic.LoadInt32(&c.closed) == 1 {
		return ErrClosed
	}
	c.touch()
	return c.client.Send(&ImMessage{Event: EventData, SessionID: c.id, MessageType: messageType, Payload: data})
}

func (c *GrpcConnection) ReadMessage(stopChan <-chan struct{}) (int, []byte, error) {
	select {
	case <-stopChan:
		return 0, nil, ErrClosed
	case msg, ok := <-c.incoming:
		if !ok {
			return 0, nil, ErrClosed
		}
		c.touch()
		return msg.MessageType, msg.Payload, nil
	}
}

// PushIncoming 由传输层调用，将 gateway 的消息喂给连接
func (c *GrpcConnection) PushIncoming(msg *ImMessage) {
	if atomic.LoadInt32(&c.closed) == 1 {
		return
	}
	select {
	case c.incoming <- msg:
	default:
		// 背压时丢弃最旧消息，避免阻塞
		select {
		case <-c.incoming:
		default:
		}
		c.incoming <- msg
	}
}

func (c *GrpcConnection) Close() error {
	if atomic.CompareAndSwapInt32(&c.closed, 0, 1) {
		close(c.incoming)
		_ = c.client.Send(&ImMessage{Event: EventSessionClose, SessionID: c.id})
	}
	return nil
}

func (c *GrpcConnection) IsClosed() bool {
	return atomic.LoadInt32(&c.closed) == 1
}

func (c *GrpcConnection) GetLastActiveTime() time.Time {
	return time.Unix(atomic.LoadInt64(&c.lastActive), 0)
}

func (c *GrpcConnection) IsStale(timeout time.Duration) bool {
	return time.Since(c.GetLastActiveTime()) > timeout
}

// 报错消息
var ErrClosed = fmt.Errorf("connection closed")
