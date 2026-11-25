package lobby

import (
	"context"
	"log/slog"

	"game-backend-practice/internal/protocol"
	"game-backend-practice/internal/ws"
)

// Handler 实现 ws.Handler，把大厅相关消息转给 Manager。
// 玩家身份一律取 Conn.ID()（来自 token 鉴权），不信任消息体里的 PlayerID/From 字段，
// 否则客户端可以伪造他人身份准备或邀请。
type Handler struct {
	manager *Manager // 大厅状态机与组队的组合入口
}

// NewHandler 创建大厅消息处理器。
func NewHandler(manager *Manager) *Handler {
	return &Handler{manager: manager}
}

// Handle 按消息类型分发：LoginReq 驱动状态机进入 Lobby，
// ReadyReq 切换准备状态，TeamInvite 根据 Accept 区分邀请与接受。
// MatchStart/RoomAssign/LoginResp 是服务端下发的消息，客户端发来时忽略而不是报错，
// 因为 ws 读泵会把 handler 的错误当作致命错误并关闭连接。
func (h *Handler) Handle(_ context.Context, c *ws.Conn, msg protocol.Message) error {
	return h.handle(c.ID(), msg)
}

// handle 接受已经鉴权过的 playerID，便于单元测试绕开无法 mock 的 *ws.Conn。
func (h *Handler) handle(playerID string, msg protocol.Message) error {
	if msg == nil {
		return nil
	}
	switch m := msg.(type) {
	case *protocol.LoginReq:
		return h.manager.Login(playerID)
	case *protocol.ReadyReq:
		return h.manager.SetReady(playerID, m.Ready)
	case *protocol.TeamInvite:
		if m.Accept {
			return h.manager.Accept(playerID, m.TeamID)
		}
		return h.manager.Invite(playerID, m.To)
	default:
		slog.Debug("lobby: ignore non-lobby message", "player", playerID, "type", msg.Type())
		return nil
	}
}
