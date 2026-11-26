package room

import (
	"context"
	"errors"
	"fmt"

	"game-backend-practice/internal/protocol"
	"game-backend-practice/internal/ws"
)

// Handler 实现 ws.Handler，把战斗类消息路由到玩家所在房间。
type Handler struct {
	rooms *Manager // 房间注册表，按连接身份定位对局
}

// NewHandler 创建房间消息处理器。
func NewHandler(rooms *Manager) *Handler {
	return &Handler{rooms: rooms}
}

// Handle 实现 ws.Handler；连接身份来自已鉴权的 ws.Conn，消息体身份不可信。
func (h *Handler) Handle(_ context.Context, c *ws.Conn, msg protocol.Message) error {
	if c == nil {
		return errors.New("room: nil connection")
	}
	return h.handle(c.ID(), msg)
}

// handle 按连接身份把 InputCmd 应用到对应房间；其他消息暂时忽略。
func (h *Handler) handle(playerID string, msg protocol.Message) error {
	if msg == nil {
		return errors.New("room: nil message")
	}
	cmd, ok := msg.(*protocol.InputCmd)
	if !ok {
		// Snapshot / GameResult 是服务端下发或结算消息，客户端发来时先安全忽略。
		return nil
	}
	r, ok := h.rooms.RoomOf(playerID)
	if !ok {
		return fmt.Errorf("room: player %q not in any room", playerID)
	}
	return r.ApplyInput(playerID, *cmd)
}
