package lobby

import (
	"errors"
	"fmt"
	"sync"
)

// Manager 管理大厅里的玩家状态：谁在线、谁准备了、谁在排队、谁进了房间。
// 可以把它想成和平精英大厅背后的“玩家状态台账”：Login、SetReady、EnterMatching 这些方法就是在这本台账上改状态。
type Manager struct {
	mu      sync.RWMutex     // 保护 players 的并发读写
	players map[string]State // 玩家 ID 到当前状态的对应表，没登录过的人不在表里
	teams   *TeamManager     // 队伍管理，负责建队、邀请、准备
}

// NewManager 创建大厅管理器；teamSize 非法时直接报错，不让服务带错配置启动。
func NewManager(teamSize int) (*Manager, error) {
	teams, err := NewTeamManager(teamSize)
	if err != nil {
		return nil, err
	}
	return &Manager{
		players: make(map[string]State),
		teams:   teams,
	}, nil
}

// Teams 把队伍管理器交出去，方便 handler 直接调用建队、邀请等方法。
func (m *Manager) Teams() *TeamManager { return m.teams }

// StateOf 返回玩家当前状态；从未登录的玩家视为 Offline。
func (m *Manager) StateOf(playerID string) State {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if state, ok := m.players[playerID]; ok {
		return state
	}
	return Offline
}

// Login 把玩家从 Offline 放入 Lobby。ws 升级已完成 token 鉴权，
// 这里的 playerID 来自连接而非消息体，避免冒充。
func (m *Manager) Login(playerID string) error {
	if playerID == "" {
		return errors.New("lobby: player id must not be empty")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	current := m.players[playerID]
	if current != Offline {
		return fmt.Errorf("lobby: player %q already logged in (state %s)", playerID, current)
	}
	m.players[playerID] = Lobby
	return nil
}

// changeState 是状态推进的统一入口：校验合法性后才写入，失败不改变状态。
func (m *Manager) changeState(playerID string, target State) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	current, ok := m.players[playerID]
	if !ok {
		return fmt.Errorf("lobby: unknown player %q", playerID)
	}
	if !CanTransition(current, target) {
		return fmt.Errorf("lobby: invalid state transition %s -> %s for player %q", current, target, playerID)
	}
	m.players[playerID] = target
	return nil
}

// SetReady 同时更新玩家状态和队伍内的准备标记。
// 目标状态与当前一致时视为幂等成功（客户端可能重发准备/取消准备），
// 但没有登录过的玩家不能直接进入大厅。
func (m *Manager) SetReady(playerID string, ready bool) error {
	target := Lobby
	if ready {
		target = Ready
	}
	m.mu.Lock()
	current, ok := m.players[playerID]
	if !ok {
		m.mu.Unlock()
		return fmt.Errorf("lobby: unknown player %q", playerID)
	}
	if current != target {
		if !CanTransition(current, target) {
			m.mu.Unlock()
			return fmt.Errorf("lobby: invalid state transition %s -> %s for player %q", current, target, playerID)
		}
		m.players[playerID] = target
	}
	m.mu.Unlock()
	// 单人匹配不需要队伍；只在玩家确实属于某支队伍时同步准备标记。
	if m.teams.TeamOf(playerID) == nil {
		return nil
	}
	return m.teams.SetReady(playerID, ready)
}

// EnterMatching 把玩家状态改成“匹配中”，表示他开始排队。
func (m *Manager) EnterMatching(playerID string) error {
	return m.changeState(playerID, Matching)
}

// AssignRoom 表示匹配成功，玩家进入房间，状态改成“战斗中”。
func (m *Manager) AssignRoom(playerID string) error {
	return m.changeState(playerID, InRoom)
}

// Settle 表示对局结束，进入结算。
func (m *Manager) Settle(playerID string) error {
	return m.changeState(playerID, Settling)
}

// FinishSettle 表示结算完成，玩家回到大厅。
func (m *Manager) FinishSettle(playerID string) error {
	return m.changeState(playerID, Lobby)
}

// Disconnect 把玩家置回 Offline 并从队伍移除。重复断线视为幂等成功。
func (m *Manager) Disconnect(playerID string) error {
	m.mu.Lock()
	current, ok := m.players[playerID]
	if !ok {
		m.mu.Unlock()
		return fmt.Errorf("lobby: unknown player %q", playerID)
	}
	if current == Offline {
		m.mu.Unlock()
		return nil
	}
	if !CanTransition(current, Offline) {
		m.mu.Unlock()
		return fmt.Errorf("lobby: invalid state transition %s -> Offline for player %q", current, playerID)
	}
	m.players[playerID] = Offline
	m.mu.Unlock()

	// 玩家可能不在任何队伍；没有队伍不算错误。
	_ = m.teams.Leave(playerID)
	return nil
}

// CreateTeam 委托给组队管理器建队。
func (m *Manager) CreateTeam(playerID string) (*Team, error) {
	return m.teams.Create(playerID)
}

// Invite 委托给组队管理器发出邀请。
func (m *Manager) Invite(from, to string) error {
	return m.teams.Invite(from, to)
}

// Accept 委托给组队管理器接受邀请。
func (m *Manager) Accept(playerID, teamID string) error {
	return m.teams.Accept(playerID, teamID)
}
