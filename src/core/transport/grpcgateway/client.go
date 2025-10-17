package grpcgateway

import (
	"context"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// IMGatewayClient 采用 gRPC 双向流，使用 JSON 编码
type IMGatewayClient struct {
	conn   *grpc.ClientConn
	stream grpc.ClientStream
}

// NewIMGatewayClient 创建到 im-server 的客户端，路径与服务名需与服务端一致
func NewIMGatewayClient(ctx context.Context, addr string) (*IMGatewayClient, error) {
	// 强制使用 json 编码，避免 proto 依赖
	opts := []grpc.DialOption{
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithDefaultCallOptions(grpc.ForceCodec(jsonCodec{}), grpc.CallContentSubtype("json")),
	}
	cc, err := grpc.DialContext(ctx, addr, opts...)
	if err != nil {
		return nil, err
	}
	// 与服务端 service.go 中的 ServiceDesc 保持一致
	sd := &grpc.StreamDesc{ServerStreams: true, ClientStreams: true}
	// 方法名与服务端注册的 Method 需一致："/im.IMGateway/MessageGateway"
	st, err := cc.NewStream(ctx, sd, "/im.IMGateway/MessageGateway")
	if err != nil {
		cc.Close()
		return nil, err
	}
	return &IMGatewayClient{conn: cc, stream: st}, nil
}

func (c *IMGatewayClient) Send(msg *ImMessage) error {
	return c.stream.SendMsg(msg)
}

func (c *IMGatewayClient) Recv() (*ImMessage, error) {
	m := new(ImMessage)
	if err := c.stream.RecvMsg(m); err != nil {
		return nil, err
	}
	return m, nil
}

func (c *IMGatewayClient) Close() error {
	if c.stream != nil {
		_ = c.stream.CloseSend()
	}
	if c.conn != nil {
		return c.conn.Close()
	}
	return nil
}
