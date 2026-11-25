// package main 表示这是一个可直接运行的程序入口，而不是被别人引用的库
package main

import (
	"context"   // 带超时地控制"优雅关闭"最多等多久
	"errors"    // 判断错误是不是 http.ErrServerClosed（服务正常关闭）
	"flag"      // 解析命令行参数，例如 -config
	"log/slog"  // Go 1.21+ 内置的结构化日志库，日志会带 key=value 字段
	"net/http"  // HTTP 服务器与路由
	"os"        // 访问标准输出、退出程序
	"os/signal" // 监听操作系统信号（Ctrl+C 等）
	"syscall"   // 定义 SIGINT / SIGTERM 等信号常量

	"game-backend-practice/internal/config"  // 本项目自己的配置加载包
	"game-backend-practice/internal/lobby"   // 1.5 的大厅服务（状态机 + 组队）
	"game-backend-practice/internal/session" // 1.3 的 token 与会话表
	"game-backend-practice/internal/ws"      // 1.4 的 WebSocket 接入层
)

// newHandler 创建 HTTP 请求路由器。
// 单独抽成函数，是为了让测试能直接调用它，而不用真的启动一个服务器。
func newHandler(wsServer *ws.Server) http.Handler {
	mux := http.NewServeMux()
	// Go 1.22+ 支持 "GET /healthz" 这种带 HTTP 方法的路由写法：
	// 只有 GET 请求会命中，POST 等其他方法会自动返回 405（方法不允许）。
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		// 返回一个最简单的健康检查响应：服务还活着
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})
	// WebSocket 升级请求走 1.4 接入层；鉴权、注册表、心跳都封装在 ws.Server 里
	mux.Handle("GET /ws", wsServer)
	return mux
}

// main 是程序入口。启动流程依次为：
// 1. 解析命令行参数；2. 加载配置；3. 设置日志；4. 启动 HTTP 服务；5. 等退出信号并优雅关闭
func main() {
	// -config 允许指定配置文件路径，默认是 configs/config.yaml
	configPath := flag.String("config", "configs/config.yaml", "path to YAML config file")
	flag.Parse()

	// 配置读不出来（文件不存在 / 格式错 / 值非法）就直接报错退出，不带着错误启动
	cfg, err := config.Load(*configPath)
	if err != nil {
		slog.Error("load config failed", "path", *configPath, "err", err)
		os.Exit(1)
	}

	// 把配置里的 info/debug 等字符串转成 slog 能用的日志级别
	var levelVar slog.LevelVar
	if err := levelVar.UnmarshalText([]byte(cfg.Log.Level)); err != nil {
		slog.Error("invalid log level", "level", cfg.Log.Level, "err", err)
		os.Exit(1)
	}
	// 设置为全局默认 logger，后面所有 slog.Info/Error 都按这个级别输出
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: &levelVar})))

	// 会话管理器负责 token 签发与校验
	sessions, err := session.NewManager([]byte(cfg.Session.Secret), cfg.Session.TokenTTL)
	if err != nil {
		slog.Error("create session manager failed", "err", err)
		os.Exit(1)
	}

	// 1.5 大厅服务：玩家状态机 + 组队；房间处理器等 1.7 才接入，暂时用 nil 兜底
	lobbyManager, err := lobby.NewManager(cfg.Game.TeamSize)
	if err != nil {
		slog.Error("create lobby manager failed", "err", err)
		os.Exit(1)
	}
	wsServer := ws.NewServer(sessions, ws.NewRouter(lobby.NewHandler(lobbyManager), nil))

	// 组装 HTTP 服务器：监听地址、路由、请求头超时都来自配置文件
	srv := &http.Server{
		Addr:              cfg.Server.Addr,
		Handler:           newHandler(wsServer),
		ReadHeaderTimeout: cfg.Server.ReadHeaderTimeout,
	}

	// 用 goroutine 启动服务器，主 goroutine 继续往下走，去等待退出信号
	go func() {
		slog.Info("gateway listening", "addr", cfg.Server.Addr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			// ErrServerClosed 是我们主动关闭服务，不算错误；其他错误才记录
			slog.Error("server error", "err", err)
		}
	}()

	// 阻塞在这里，直到收到 Ctrl+C（SIGINT）或终止信号（SIGTERM）
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	// 优雅关闭：给现有请求最多 shutdown_timeout 秒处理完，而不是直接掐断
	slog.Info("shutting down gateway", "timeout", cfg.Server.ShutdownTimeout)
	ctx, cancel := context.WithTimeout(context.Background(), cfg.Server.ShutdownTimeout)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		slog.Error("shutdown error", "err", err)
	}
	slog.Info("gateway stopped")
}
