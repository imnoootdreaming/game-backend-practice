package ws

import (
	"context" // 把取消信号传给后续的大厅/房间处理器
	"errors"  // 空消息校验
	"log/slog"

	"game-backend-practice/internal/protocol"
)

// Handler 处理一条已解码的消息。1.5 的大厅和 1.7 的房间会分别实现它。
type Handler interface {
	Handle(context.Context, *Conn, protocol.Message) error
}

// HandlerFunc 让普通函数也能当作 Handler，方便测试里写 mock。
type HandlerFunc func(context.Context, *Conn, protocol.Message) error

func (f HandlerFunc) Handle(ctx context.Context, c *Conn, msg protocol.Message) error {
	return f(ctx, c, msg)
}

// noopHandler 是 1.4 阶段的占位处理器：消息已被正确路由，但业务逻辑要等 1.5/1.7 才实现。
var noopHandler Handler = HandlerFunc(func(_ context.Context, _ *Conn, msg protocol.Message) error {
	slog.Debug("ws: message routed to placeholder handler", "type", msg.Type())
	return nil
})

// Router 按消息类型把流量分到 lobby / room / fallback 三条路径。
// 连接层只依赖这个接口，后面加新消息不需要改读写泵。
type Router struct {
	lobby    Handler // 处理登录/大厅类消息
	room     Handler // 处理输入/快照/结算类消息
	fallback Handler // 处理未识别消息，兜底避免 nil panic
}

// NewRouter 创建路由器，未显式提供的处理器用 noopHandler 兜底，避免 nil panic。
func NewRouter(lobby, room Handler) *Router {
	return NewRouterWithFallback(lobby, room, nil)
}

// NewRouterWithFallback 可以额外指定未知消息的处理器。
func NewRouterWithFallback(lobby, room, fallback Handler) *Router {
	if lobby == nil {
		lobby = noopHandler
	}
	if room == nil {
		room = noopHandler
	}
	if fallback == nil {
		fallback = noopHandler
	}
	return &Router{lobby: lobby, room: room, fallback: fallback}
}

// Route 把消息交给对应的处理器，并把处理器的错误原样返回给读泵。
func (r *Router) Route(ctx context.Context, c *Conn, msg protocol.Message) error {
	if msg == nil {
		return errors.New("ws: nil message")
	}
	switch msg.Type() {
	case protocol.TypeLoginReq, protocol.TypeLoginResp,
		protocol.TypeReadyReq, protocol.TypeTeamInvite,
		protocol.TypeMatchStart, protocol.TypeRoomAssign:
		return r.lobby.Handle(ctx, c, msg)
	case protocol.TypeInputCmd, protocol.TypeSnapshot, protocol.TypeGameResult:
		return r.room.Handle(ctx, c, msg)
	default:
		return r.fallback.Handle(ctx, c, msg)
	}
}
