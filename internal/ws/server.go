package ws

import (
	"bytes"   // 把一条 WebSocket 消息当作字节流喂给 protocol.Decoder
	"context" // 每个连接的处理上下文
	"errors"  // 判断 io.EOF 和构造关闭错误
	"io"      // Decoder 读完整帧后返回 io.EOF
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"

	"game-backend-practice/internal/protocol"
	"game-backend-practice/internal/session"
)

const (
	defaultPingInterval = 30 * time.Second // 写泵主动发 ping 的周期
	defaultPongTimeout  = 60 * time.Second // 超过这个时间没收到 pong 就踢掉连接
	readLimit           = 1 << 20          // 单条 WebSocket 消息最大 1 MiB，与 protocol.MaxFrameSize 对齐
	writeWait           = time.Second      // 单次控制帧写出的超时
	sendBuffer          = 16               // 写泵出站队列长度
)

// Server 是挂在 gateway 上的 WebSocket 处理器：
// 先做 token 鉴权，再升级连接，然后启动读写泵并注册到连接表。
type Server struct {
	upgrader     websocket.Upgrader         // 将 HTTP 连接升级为 WebSocket 的升级器，当前允许任意 Origin
	sessions     *session.Manager           // 1.3 的 token 校验，升级前必须先鉴权
	registry     *Registry                  // 在线连接表，按 playerID 去重
	router       *Router                    // 把解码后的消息分发到 lobby/room
	pingInterval time.Duration              // 写泵发 ping 的周期
	pongTimeout  time.Duration              // 超过该时长没收到 pong 就踢连接
	now          func() time.Time           // 可注入时钟，便于测试心跳超时
	newTicker    func(time.Duration) Ticker // 可注入 ticker 工厂，避免测试等真实时间
}

// ServerOption 用来覆盖 Server 的默认行为，测试主要靠它注入假时钟和假 ticker。
type ServerOption func(*Server)

func WithPingInterval(d time.Duration) ServerOption {
	return func(s *Server) { s.pingInterval = d }
}

func WithPongTimeout(d time.Duration) ServerOption {
	return func(s *Server) { s.pongTimeout = d }
}

func WithClock(now func() time.Time) ServerOption {
	return func(s *Server) { s.now = now }
}

func WithTickerFactory(f func(time.Duration) Ticker) ServerOption {
	return func(s *Server) { s.newTicker = f }
}

// NewServer 构造 WebSocket 服务。sessions 由 1.3 提供，router 负责把消息分发到 lobby/room。
func NewServer(sessions *session.Manager, router *Router, opts ...ServerOption) *Server {
	if router == nil {
		router = NewRouter(nil, nil)
	}
	s := &Server{
		// 本地开发用简单 origin 策略；部署时应按域名白名单收紧，避免跨站劫持
		upgrader:     websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }},
		sessions:     sessions,
		registry:     NewRegistry(),
		router:       router,
		pingInterval: defaultPingInterval,
		pongTimeout:  defaultPongTimeout,
		now:          time.Now,
		newTicker:    func(d time.Duration) Ticker { return realTicker{time.NewTicker(d)} },
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// Registry 暴露连接表，供 gateway 监控和测试断言在线人数。
func (s *Server) Registry() *Registry { return s.registry }

// ServeHTTP 处理 /ws 升级请求：token 与 player_id 从查询参数传入，由 1.3 的会话管理器校验。
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	token := r.URL.Query().Get("token")
	playerID := r.URL.Query().Get("player_id")
	if token == "" || playerID == "" {
		http.Error(w, "missing token or player_id", http.StatusUnauthorized)
		return
	}
	if _, err := s.sessions.Authenticate(token, playerID); err != nil {
		slog.Warn("ws authentication failed", "player", playerID, "err", err)
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	// 升级之前先查重：Upgrade 会写死 101 状态码，之后就没法再返回 409 了
	if _, ok := s.registry.Get(playerID); ok {
		http.Error(w, "player already connected", http.StatusConflict)
		return
	}

	wsConn, err := s.upgrader.Upgrade(w, r, nil)
	if err != nil {
		slog.Warn("ws upgrade failed", "err", err)
		return
	}

	c := newConn(wsConn, playerID, s.registry, s.router, s.pongTimeout, s.now, s.newTicker(s.pingInterval))
	if err := s.registry.Add(c); err != nil {
		// 理论上查重已经拦住了，这里兜底防止并发同时升级
		slog.Warn("ws register failed", "player", playerID, "err", err)
		_ = c.Close()
		return
	}

	go c.writePump()
	c.readPump() // 读泵阻塞在当前 goroutine，直到连接关闭
	s.registry.Remove(playerID)
}

// outbound 是写泵统一的出站消息：可以是文本帧、二进制帧，也可以是 ping/pong 控制帧。
type outbound struct {
	kind int    // WebSocket 消息类型：文本帧/二进制帧/pong 等控制帧
	data []byte // 帧负载，控制帧可能为空
}

// Conn 封装一条 WebSocket 连接。读泵只负责读和路由，写泵只负责所有写出，避免并发写冲突。
type Conn struct {
	ws       *websocket.Conn // 底层 WebSocket 连接
	playerID string          // 连接绑定的已鉴权玩家 ID
	registry *Registry       // 所属在线连接表，关闭时由读泵注销
	router   *Router         // 消息分发器
	hb       *Heartbeat      // 本连接的心跳状态
	ticker   Ticker          // 写泵定时发 ping 用的定时器
	send     chan outbound   // 写泵的出站队列，所有写出只由写泵执行
	done     chan struct{}   // 关闭信号，通知读写泵退出
	once     sync.Once       // 保证 Close 只真正执行一次
}

func newConn(ws *websocket.Conn, playerID string, registry *Registry, router *Router,
	timeout time.Duration, now func() time.Time, ticker Ticker) *Conn {
	return &Conn{
		ws:       ws,
		playerID: playerID,
		registry: registry,
		router:   router,
		hb:       NewHeartbeat(timeout, now),
		ticker:   ticker,
		send:     make(chan outbound, sendBuffer),
		done:     make(chan struct{}),
	}
}

// ID 返回连接绑定的 playerID，实现 Peer 接口。
func (c *Conn) ID() string { return c.playerID }

// Close 幂等关闭连接：读写泵都可能触发，只能真正关一次。
func (c *Conn) Close() error {
	var err error
	c.once.Do(func() {
		close(c.done)
		err = c.ws.Close()
	})
	return err
}

// Send 把出站消息交给写泵；连接已关闭时返回错误。
func (c *Conn) Send(kind int, data []byte) error {
	select {
	case c.send <- outbound{kind: kind, data: data}:
		return nil
	case <-c.done:
		return errors.New("ws: connection closed")
	}
}

// readPump 负责读帧、处理控制消息、解码业务消息并交给路由器。
func (c *Conn) readPump() {
	defer c.Close()
	c.ws.SetReadLimit(readLimit)

	for {
		kind, data, err := c.ws.ReadMessage()
		if err != nil {
			return
		}
		switch kind {
		case websocket.PingMessage:
			// 收到客户端 ping 后回 pong，写出统一交给写泵
			_ = c.Send(websocket.PongMessage, data)
		case websocket.PongMessage:
			c.hb.Beat()
		case websocket.TextMessage, websocket.BinaryMessage:
			// 一条 WebSocket 消息里可能塞了多条协议帧，循环读到 EOF 为止
			dec := protocol.NewDecoder(bytes.NewReader(data))
			for {
				msg, err := dec.ReadMessage()
				if errors.Is(err, io.EOF) {
					break
				}
				if err != nil {
					slog.Warn("ws decode failed", "player", c.playerID, "err", err)
					return
				}
				if err := c.router.Route(context.Background(), c, msg); err != nil {
					slog.Error("ws route failed", "player", c.playerID, "type", msg.Type(), "err", err)
					return
				}
			}
		}
	}
}

// writePump 负责所有写出：定时 ping、出站消息，以及心跳超时后主动踢连接。
func (c *Conn) writePump() {
	defer c.ticker.Stop()
	for {
		select {
		case <-c.ticker.C():
			if c.hb.Expired() {
				slog.Info("ws heartbeat timeout", "player", c.playerID)
				_ = c.Close()
				return
			}
			if err := c.ws.WriteControl(websocket.PingMessage, nil, time.Now().Add(writeWait)); err != nil {
				_ = c.Close()
				return
			}
		case out := <-c.send:
			if err := c.ws.WriteControl(out.kind, out.data, time.Now().Add(writeWait)); err != nil {
				_ = c.Close()
				return
			}
		case <-c.done:
			return
		}
	}
}
