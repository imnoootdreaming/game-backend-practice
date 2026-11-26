package ws

import (
	"context" // 把取消信号传给后续的大厅/房间处理器
	"errors"  // 空消息校验
	"log/slog"

	"game-backend-practice/internal/protocol"
)

// Handler 处理一条已解码的消息。1.5 的大厅和 1.7 的房间会分别实现它。
// 多态契约：调用方只认这个接口，不关心背后是 *lobby.Handler、未来的 *room.Handler 还是测试桩；
// Go 没有 implements 关键字，方法签名对上就自动满足接口（鸭子类型）。
type Handler interface {
	Handle(context.Context, *Conn, protocol.Message) error
}

// HandlerFunc 让普通函数也能当作 Handler，方便测试里写 mock。
// 它自己也实现了 Handle：把调用转发给底层函数，于是普通函数也成了接口的一种具体实现。
type HandlerFunc func(context.Context, *Conn, protocol.Message) error

func (f HandlerFunc) Handle(ctx context.Context, c *Conn, msg protocol.Message) error {
	return f(ctx, c, msg)
}

// noopHandler 是 1.4 阶段的占位处理器：消息已被正确路由，但业务逻辑要等 1.5/1.7 才实现。
// 这里把 HandlerFunc 值装进 Handler 接口变量：接口变量保存具体值，调用时走它的 Handle。
var noopHandler Handler = HandlerFunc(func(_ context.Context, _ *Conn, msg protocol.Message) error {
	slog.Debug("ws: message routed to placeholder handler", "type", msg.Type())
	return nil
})

// Router 按消息类型把流量分到 lobby / room / fallback 三条路径。
// 连接层只依赖这个接口，后面加新消息不需要改读写泵。
type Router struct {
	// 三个字段都是接口类型：存的是接口值（具体实现的指针/值），
	// 可以在运行期替换实现而不改动 Router 和读泵的代码。
	lobby    Handler // 处理登录/大厅类消息；当前注入 *lobby.Handler
	room     Handler // 处理输入/快照/结算类消息；1.7 接入前由 noopHandler 顶着
	fallback Handler // 处理未识别消息，兜底避免 nil panic
}

// NewRouter 创建路由器，未显式提供的处理器用 noopHandler 兜底，避免 nil panic。
// 参数声明为接口而非具体类型：调用方传来的 *lobby.Handler 在这里被自动当作 Handler 接收。
func NewRouter(lobby, room Handler) *Router {
	return NewRouterWithFallback(lobby, room, nil)
}

// NewRouterWithFallback 可以额外指定未知消息的处理器。
// nil 检查后统一换成 noopHandler，保证三个接口字段永远是可调用的接口值。
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
// 多态的分发点：下面三处调用表面上都是 Handler.Handle，写法完全一样，
// 但接口变量里装的具体实现不同，运行时会执行各自的代码（动态分派）。
// 例如 h.Handle(ctx, conn, msg) 这句，若 h 装的是 *lobby.Handler 就走大厅逻辑，
// 换成 recordingHandler 就记录消息类型——调用方不需要知道背后是谁。
func (r *Router) Route(ctx context.Context, c *Conn, msg protocol.Message) error {
	if msg == nil {
		return errors.New("ws: nil message")
	}
	switch msg.Type() {
	case protocol.TypeLoginReq, protocol.TypeLoginResp,
		protocol.TypeReadyReq, protocol.TypeTeamInvite,
		protocol.TypeMatchStart, protocol.TypeRoomAssign:
		return r.lobby.Handle(ctx, c, msg) // 接口调用：当前实际执行 *lobby.Handler.Handle
	case protocol.TypeInputCmd, protocol.TypeSnapshot, protocol.TypeGameResult:
		return r.room.Handle(ctx, c, msg) // 同一个接口方法：1.7 前实际执行 noopHandler.Handle
	default:
		return r.fallback.Handle(ctx, c, msg) // 兜底实现；测试里可注入 recordingHandler
	}
}
