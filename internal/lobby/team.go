package lobby

import (
	"crypto/rand" // 生成不可预测的队伍 ID
	"encoding/hex"
	"errors"
	"fmt"
	"sync"
)

// teamIDBytes 是队伍 ID 的随机字节数；前缀便于日志里区分 teamID 与 playerID。
const teamIDBytes = 8

// Team 是一支队伍。Members[0] 是队长（队长退出后自动由下一位接任）；
// Ready 只记录已准备的成员；Invited 记录已邀请但尚未接受的玩家。
type Team struct {
	ID      string          // 队伍唯一标识，由 newTeamID 生成
	Members []string        // 成员列表，Members[0] 为队长
	Ready   map[string]bool // 已准备的成员集合，只保留 true
	Invited map[string]bool // 已邀请但尚未接受的玩家集合
}

// TeamManager 用同一把锁保护队伍表和 Map<playerID, teamID> 索引，
// 保证"一个人在同一时刻只属于一支队伍"这个不变式。
type TeamManager struct {
	mu       sync.RWMutex      // 保护 teams 与 byPlayer 的并发读写
	teamSize int               // 队伍满员人数，超过则拒绝加入
	teams    map[string]*Team  // Map<teamID, 队伍>
	byPlayer map[string]string // Map<playerID, teamID>，保证一人一队
}

// NewTeamManager 构造组队管理器，teamSize 为满员人数。
func NewTeamManager(teamSize int) (*TeamManager, error) {
	if teamSize <= 0 {
		return nil, errors.New("lobby: team size must be positive")
	}
	return &TeamManager{
		teamSize: teamSize,
		teams:    make(map[string]*Team),
		byPlayer: make(map[string]string),
	}, nil
}

func newTeamID() (string, error) {
	buf := make([]byte, teamIDBytes)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("lobby: generate team id: %w", err)
	}
	return "t_" + hex.EncodeToString(buf), nil
}

// Len 返回当前队伍数量。
func (tm *TeamManager) Len() int {
	tm.mu.RLock()
	defer tm.mu.RUnlock()
	return len(tm.teams)
}

// TeamOf 返回玩家所在队伍；玩家不在任何队伍时返回 nil。
func (tm *TeamManager) TeamOf(playerID string) *Team {
	tm.mu.RLock()
	defer tm.mu.RUnlock()
	teamID, ok := tm.byPlayer[playerID]
	if !ok {
		return nil
	}
	return tm.teams[teamID]
}

// Create 让玩家建队并自动成为队长。已在其他队伍的玩家不能重复建队。
func (tm *TeamManager) Create(leaderID string) (*Team, error) {
	if leaderID == "" {
		return nil, errors.New("lobby: leader id must not be empty")
	}
	tm.mu.Lock()
	defer tm.mu.Unlock()
	if _, exists := tm.byPlayer[leaderID]; exists { // 检查当前玩家是否已在队伍中
		return nil, fmt.Errorf("lobby: player %q already in a team", leaderID)
	}
	id, err := newTeamID()
	if err != nil {
		return nil, err
	}
	team := &Team{
		ID:      id,
		Members: []string{leaderID},
		Ready:   make(map[string]bool),
		Invited: make(map[string]bool),
	}
	tm.teams[id] = team        // 绑定队伍到队伍管理器中
	tm.byPlayer[leaderID] = id // 标记当前玩家已有队伍 对应队伍号为 id
	return team, nil
}

// Invite 由队伍成员邀请玩家。重复邀请同一人保持幂等，
// 这样客户端超时重试不会因为"已邀请过"而收到错误。
func (tm *TeamManager) Invite(from, to string) error {
	if to == "" {
		return errors.New("lobby: invitee must not be empty")
	}
	tm.mu.Lock()
	defer tm.mu.Unlock()
	teamID, ok := tm.byPlayer[from]
	if !ok {
		return fmt.Errorf("lobby: inviter %q has no team", from)
	}
	if _, exists := tm.byPlayer[to]; exists {
		return fmt.Errorf("lobby: player %q already in a team", to)
	}
	team := tm.teams[teamID]
	if len(team.Members) >= tm.teamSize {
		return fmt.Errorf("lobby: team %s is full", teamID)
	}
	team.Invited[to] = true
	return nil
}

// Accept 让玩家接受某支队伍的邀请后加入。邀请校验和满员校验都在锁内完成，
// 避免两个玩家同时接受最后一个名额导致超员。
func (tm *TeamManager) Accept(playerID, teamID string) error {
	if playerID == "" {
		return errors.New("lobby: player id must not be empty")
	}
	tm.mu.Lock()
	defer tm.mu.Unlock()
	team, ok := tm.teams[teamID]
	if !ok {
		return fmt.Errorf("lobby: unknown team %q", teamID)
	}
	if !team.Invited[playerID] {
		return fmt.Errorf("lobby: player %q has no invitation to team %s", playerID, teamID)
	}
	if _, exists := tm.byPlayer[playerID]; exists {
		return fmt.Errorf("lobby: player %q already in a team", playerID)
	}
	if len(team.Members) >= tm.teamSize {
		return fmt.Errorf("lobby: team %s is full", teamID)
	}
	team.Members = append(team.Members, playerID)
	tm.byPlayer[playerID] = teamID
	delete(team.Invited, playerID)
	return nil
}

// Leave 让玩家退出队伍，清理索引、准备状态和邀请记录。
// 最后一名成员离开时删除整支队伍，避免空队伍堆积。
func (tm *TeamManager) Leave(playerID string) error {
	tm.mu.Lock()
	defer tm.mu.Unlock()
	teamID, ok := tm.byPlayer[playerID]
	if !ok {
		return fmt.Errorf("lobby: player %q has no team", playerID)
	}
	team := tm.teams[teamID]
	delete(tm.byPlayer, playerID)
	delete(team.Ready, playerID)

	remaining := make([]string, 0, len(team.Members)-1)
	for _, id := range team.Members {
		if id != playerID {
			remaining = append(remaining, id)
		}
	}
	team.Members = remaining
	if len(team.Members) == 0 {
		delete(tm.teams, teamID)
	}
	return nil
}

// SetReady 更新队伍内的准备状态；取消准备直接删除键，
// 让 Ready 映射只保留 true，读方无需再判断 false。
func (tm *TeamManager) SetReady(playerID string, ready bool) error {
	tm.mu.Lock()
	defer tm.mu.Unlock()
	teamID, ok := tm.byPlayer[playerID]
	if !ok {
		return fmt.Errorf("lobby: player %q has no team", playerID)
	}
	team := tm.teams[teamID]
	if ready {
		team.Ready[playerID] = true
	} else {
		delete(team.Ready, playerID)
	}
	return nil
}
