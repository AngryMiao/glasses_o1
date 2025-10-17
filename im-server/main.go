package main

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	imcfg "im-server/configs"
	"im-server/gateway"
	"im-server/utils"
)

// Application IM服务器应用程序结构体
// 包含配置、日志、gRPC服务器和HTTP服务器等核心组件
type Application struct {
	config *imcfg.Config            // 配置信息
	logger *utils.Logger            // 日志记录器
	grpc   *gateway.IMGatewayServer // gRPC网关服务器
	http   *http.Server             // HTTP服务器（用于WebSocket）
}

// init 初始化应用程序
// 加载配置文件并初始化日志系统
func (app *Application) init() error {
	// 加载 im-server 独立配置
	cfg, cfgPath, err := imcfg.LoadConfig()
	if err != nil {
		return fmt.Errorf("加载配置失败: %w", err)
	}
	app.config = cfg

	// 初始化日志系统
	logger, err := utils.NewLogger((*utils.LogCfg)(&cfg.Log))
	if err != nil {
		return fmt.Errorf("初始化日志失败: %w", err)
	}
	app.logger = logger
	utils.DefaultLogger = logger
	app.logger.Info("im-server 配置初始化完成: %s", cfgPath)

	return nil
}

// start 启动应用程序服务
// 启动gRPC网关服务器和WebSocket服务器
func (app *Application) start() error {
	grpcPort := app.config.Transport.GrpcGateway.Port
	grpcAddr := fmt.Sprintf("%s:%d", app.config.Transport.GrpcGateway.IP, grpcPort)
	lis, err := net.Listen("tcp", grpcAddr)
	if err != nil {
		return fmt.Errorf("监听 gRPC 地址失败: %w", err)
	}

	// 创建并启动gRPC网关服务器
	app.grpc = gateway.NewIMGatewayServer(app.config, app.logger)
	if err := app.grpc.Start(lis); err != nil {
		return fmt.Errorf("启动 gRPC 总线失败: %w", err)
	}
	app.logger.Info("im-server gRPC 总线已启动: %s", grpcAddr)

	// 启动WebSocket服务器
	wsAddr := fmt.Sprintf("%s:%d", app.config.Transport.WebSocket.IP, app.config.Transport.WebSocket.Port)
	mux := http.NewServeMux()
	mux.HandleFunc("/", app.grpc.HandleWebSocket) // 将WebSocket请求路由到gRPC网关
	mux.HandleFunc("/health", app.handleHealth)   // 健康检查端点
	app.http = &http.Server{Addr: wsAddr, Handler: mux}

	// 在单独的goroutine中启动HTTP服务器
	go func() {
		if err := app.http.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			app.logger.Error("im-server WebSocket 服务启动失败: %v", err)
		}
	}()
	app.logger.Info("im-server WebSocket 服务已启动: %s", wsAddr)

	return nil
}

// handleHealth 健康检查路由，返回IM服务状态
func (app *Application) handleHealth(w http.ResponseWriter, r *http.Request) {
	status := "ok"
	if app.grpc != nil && !app.grpc.IsGatewayConnected() {
		status = "degraded" // AI总线未连接，但IM服务仍可接收WS
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(fmt.Sprintf("{\"status\":\"%s\",\"sessions\":%d}", status, app.grpc.ActiveSessions())))
}

// stop 停止应用程序服务
// 优雅地关闭HTTP服务器和gRPC服务器
func (app *Application) stop() {
	// 关闭HTTP服务器
	if app.http != nil {
		_ = app.http.Shutdown(context.Background())
	}
	// 停止gRPC服务器
	if app.grpc != nil {
		app.grpc.Stop()
	}
}

// main 程序入口点
// 初始化并启动IM服务器，处理优雅退出
func main() {
	app := &Application{}

	// 初始化应用程序
	if err := app.init(); err != nil {
		fmt.Println("im-server 初始化失败:", err)
		os.Exit(1)
	}

	// 启动应用程序服务
	if err := app.start(); err != nil {
		fmt.Println("im-server 启动失败:", err)
		os.Exit(1)
	}

	// 优雅退出处理
	// 监听系统信号（SIGINT, SIGTERM）
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh // 阻塞等待退出信号

	app.logger.Info("im-server 收到退出信号，正在关闭...")
	app.stop() // 停止所有服务
	app.logger.Info("im-server 已退出")
}
