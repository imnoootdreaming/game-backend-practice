package lobby

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"game-backend-practice/internal/match"
	"game-backend-practice/internal/protocol"
	"game-backend-practice/internal/ws"
)

// Handler 负责处理大厅相关的网络消息，收到后转给 Manager 去改状态或组队。
// 比如客户端说“我准备了”“我邀请谁”，都从这里进。
// 玩家身份只认连接里的 ID（登录时验过 token），不信消息里自己填的 PlayerID，防止冒充别人。
type Handler struct {
	manager *Manager     // 大厅状态和组队的入口
	queue   *match.Queue // 匹配队列；nil 表示旧调用方，不做入队
}

// NewHandler 创建大厅消息处理器；不接入匹配队列，保持旧调用方行为不变。
func NewHandler(manager *Manager) *Handler {
	return &Handler{manager: manager}
}

// NewHandlerWithMatch 创建同时接入匹配队列的大厅处理器。
func NewHandlerWithMatch(manager *Manager, queue *match.Queue) *Handler {
	return &Handler{manager: manager, queue: queue}
}

// Handle 是网络层调用的统一入口：按消息类型做不同的事。
// 服务端下发的消息（如 MatchStart）如果客户端发回来就忽略，避免一点业务小错把连接关掉。
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
		return h.handleReady(playerID, m.Ready, m.Rank)
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

// handleReady 更新准备状态；接入队列时按整队入队/出队，并推进全队状态。
func (h *Handler) handleReady(playerID string, ready bool, rank int) error {
	if err := h.manager.SetReady(playerID, ready); err != nil {
		return err
	}
	if h.queue == nil {
		return nil
	}
	team, members := h.partyOf(playerID)
	partyID := playerID
	if team != nil {
		partyID = team.ID
	}
	if !ready {
		// 取消准备时整队一起退出队列，并让所有成员回到大厅。
		for _, memberID := range members {
			if err := h.manager.SetReady(memberID, false); err != nil {
				return err
			}
		}
		h.queue.RemoveAll(members)
		return nil
	}
	if team != nil && !h.allReady(team, members) {
		return fmt.Errorf("lobby: team %s not all ready", team.ID)
	}
	// 先整队入队再改状态；任一步失败都回滚，避免留下半支队伍。
	now := time.Now()
	for _, memberID := range members {
		if _, err := h.queue.Enqueue(match.Entry{
			PlayerID:   memberID,
			PartyID:    partyID,
			PartySize:  len(members),
			Rank:       rank,
			EnqueuedAt: now,
		}); err != nil {
			h.queue.RemoveAll(members)
			return err
		}
	}
	for _, memberID := range members {
		if err := h.manager.EnterMatching(memberID); err != nil {
			h.queue.RemoveAll(members)
			return err
		}
	}
	return nil
}

// partyOf 返回玩家所属队伍与成员列表；散队时把自己当作一支单人队。
func (h *Handler) partyOf(playerID string) (*Team, []string) {
	team := h.manager.Teams().TeamOf(playerID)
	if team == nil {
		return nil, []string{playerID}
	}
	return team, append([]string(nil), team.Members...)
}

// allReady 检查队伍里每个人是否都已准备，避免只来一个人就把整队送进匹配。
func (h *Handler) allReady(team *Team, members []string) bool {
	for _, memberID := range members {
		if !team.Ready[memberID] {
			return false
		}
	}
	return true
}
