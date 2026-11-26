package room

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"

	"game-backend-practice/internal/protocol"
	"game-backend-practice/internal/sim"
)

const (
	initialHP       int32         = 100                    // 玩家进入房间时的初始血量
	maxMovePerInput float32       = 1                      // 单条输入允许的最大位移，防止客户端一帧瞬移
	fireCooldown    time.Duration = 200 * time.Millisecond // 同一玩家两次开火的最小间隔
	hitRange        float32       = 2                      // 开火命中判定的最大距离，按服务器坐标计算
	hitDamage       int32         = 25                     // 单次命中造成的血量伤害
)

// Player 是房间内一名玩家的权威状态，客户端不能直接改写这些字段。
type Player struct {
	ID         string    // 玩家 ID
	X          float32   // 服务器计算出的 X 坐标
	Y          float32   // 服务器计算出的 Y 坐标
	HP         int32     // 当前血量
	LastSeq    uint64    // 服务端已应用的最后一条输入序列号
	LastFireAt time.Time // 服务器记录的上次开火时间，用于射速冷却
}

// Room 是一局对战的权威状态容器，Tick 单调递增。
type Room struct {
	mu           sync.RWMutex             // 保护 Players、Tick 与 snapshotSink 的并发读写
	ID           string                   // 房间 ID
	Players      map[string]*Player       // 房间内玩家，按玩家 ID 索引
	Tick         uint64                   // 当前逻辑帧号
	now          func() time.Time         // 服务器时钟，测试可替换为固定时间
	snapshotSink func(*protocol.Snapshot) // 每个 tick 生成快照后的回调，nil 表示不广播
}

// NewRoom 创建房间并为每位玩家初始化权威状态。
func NewRoom(id string, playerIDs []string) (*Room, error) {
	if id == "" {
		return nil, errors.New("room: id must not be empty")
	}
	if len(playerIDs) == 0 {
		return nil, errors.New("room: at least one player required")
	}
	r := &Room{
		ID:      id,
		Players: make(map[string]*Player, len(playerIDs)),
		now:     time.Now,
	}
	// 用 seen 去重，避免同一个玩家在权威状态里被初始化两次。
	seen := make(map[string]struct{}, len(playerIDs))
	for _, playerID := range playerIDs {
		if playerID == "" {
			return nil, errors.New("room: player id must not be empty")
		}
		if _, ok := seen[playerID]; ok {
			return nil, fmt.Errorf("room: duplicate player %q", playerID)
		}
		seen[playerID] = struct{}{}
		r.Players[playerID] = &Player{ID: playerID, HP: initialHP}
	}
	return r, nil
}

// AddPlayer 加入一名新玩家；空 ID 或已在房间内都拒绝。
func (r *Room) AddPlayer(playerID string) error {
	if playerID == "" {
		return errors.New("room: player id must not be empty")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.Players[playerID]; ok {
		return fmt.Errorf("room: player %q already in room", playerID)
	}
	r.Players[playerID] = &Player{ID: playerID, HP: initialHP}
	return nil
}

// RemovePlayer 移除玩家；不存在的玩家视为错误，方便上层发现状态不一致。
func (r *Room) RemovePlayer(playerID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.Players[playerID]; !ok {
		return fmt.Errorf("room: player %q not in room", playerID)
	}
	delete(r.Players, playerID)
	return nil
}

// Advance 推进一个逻辑帧。
func (r *Room) Advance() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.Tick++
}

// CurrentTick 返回当前逻辑帧号；读锁避免与 ticker 写入竞争。
func (r *Room) CurrentTick() uint64 {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.Tick
}

// ApplyInput 用服务器权威状态应用一条输入。
// playerID 来自已鉴权连接，cmd.PlayerID 不可信，因此这里直接忽略消息体里的身份字段。
func (r *Room) ApplyInput(playerID string, cmd protocol.InputCmd) error {
	if playerID == "" {
		return errors.New("room: player id must not be empty")
	}
	now := r.now()
	r.mu.Lock()
	defer r.mu.Unlock()
	p, ok := r.Players[playerID]
	if !ok {
		return fmt.Errorf("room: player %q not in room", playerID)
	}
	// 旧 seq 和重复 seq 直接忽略，避免客户端重放导致重复移动。
	if cmd.Seq <= p.LastSeq {
		return nil
	}
	// 先校验位移幅度再写状态，拒绝一帧内瞬移的输入。
	if !sim.MoveAllowed(cmd.MoveX, cmd.MoveY, maxMovePerInput) {
		return fmt.Errorf("room: input move out of range: x=%f y=%f", cmd.MoveX, cmd.MoveY)
	}
	// 开火冷却不通过时整条输入都不生效，避免客户端靠高频点击绕过射速。
	if cmd.Fire && !p.LastFireAt.IsZero() && now.Sub(p.LastFireAt) < fireCooldown {
		return fmt.Errorf("room: fire cooldown active for %q", playerID)
	}
	p.X += cmd.MoveX
	p.Y += cmd.MoveY
	if cmd.Fire {
		p.LastFireAt = now
		r.applyHit(playerID, p)
	}
	p.LastSeq = cmd.Seq
	return nil
}

// applyHit 在已持锁前提下按服务器坐标结算命中，跳过射手和已阵亡目标。
func (r *Room) applyHit(shooterID string, shooter *Player) {
	for targetID, target := range r.Players {
		// 射手不会打到自己，已阵亡目标也不重复扣血。
		if targetID == shooterID || target.HP <= 0 {
			continue
		}
		if sim.InRange(shooter.X, shooter.Y, target.X, target.Y, hitRange) {
			target.HP = sim.ApplyDamage(target.HP, hitDamage)
		}
	}
}

// Snapshot 生成当前权威状态的快照；玩家按 ID 排序，保证同一 tick 的广播顺序稳定。
func (r *Room) Snapshot() *protocol.Snapshot {
	r.mu.RLock()
	defer r.mu.RUnlock()
	ids := make([]string, 0, len(r.Players))
	for id := range r.Players {
		ids = append(ids, id)
	}
	// 排序后再填玩家列表，避免 map 遍历顺序造成同帧快照内容不一致。
	sort.Strings(ids)
	players := make([]protocol.PlayerState, 0, len(ids))
	for _, id := range ids {
		p := r.Players[id]
		players = append(players, protocol.PlayerState{
			ID:      id,
			X:       p.X,
			Y:       p.Y,
			HP:      p.HP,
			LastSeq: p.LastSeq,
		})
	}
	return &protocol.Snapshot{RoomID: r.ID, Tick: r.Tick, Players: players}
}

// SetSnapshotSink 设置每个 tick 生成快照后的回调；传 nil 可关闭广播。
func (r *Room) SetSnapshotSink(fn func(*protocol.Snapshot)) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.snapshotSink = fn
}

// Run 按 interval 周期推进逻辑帧，直到 ctx 取消。
func (r *Room) Run(ctx context.Context, interval time.Duration) error {
	if interval <= 0 {
		return errors.New("room: tick interval must be positive")
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	// 同一个 select 同时等待 tick 与取消信号，保证房间销毁时能立刻退出。
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			r.Advance()
			// 回调在锁外执行，避免广播阻塞时反过来卡住房间状态读写。
			r.mu.RLock()
			sink := r.snapshotSink
			r.mu.RUnlock()
			if sink != nil {
				sink(r.Snapshot())
			}
		}
	}
}
