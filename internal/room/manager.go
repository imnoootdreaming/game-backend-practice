package room

import (
	"fmt"
	"sync"
)

// Manager 是房间注册表，按 roomID 管理正在进行的对局。
type Manager struct {
	mu       sync.RWMutex     // 保护 rooms 与 byPlayer 的并发读写
	rooms    map[string]*Room // roomID 到房间实例的映射
	byPlayer map[string]*Room // playerID 到所在房间的映射，便于路由输入
}

// NewManager 创建空的房间注册表。
func NewManager() *Manager {
	return &Manager{
		rooms:    make(map[string]*Room),
		byPlayer: make(map[string]*Room),
	}
}

// Create 按玩家列表创建房间并注册；重复 roomID 返回错误，避免覆盖已有对局。
func (m *Manager) Create(id string, playerIDs []string) (*Room, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.rooms[id]; ok {
		return nil, fmt.Errorf("room: %q already exists", id)
	}
	r, err := NewRoom(id, playerIDs)
	if err != nil {
		return nil, err
	}
	m.rooms[id] = r
	// 建立玩家到房间的反向索引，输入消息到达时才能按连接身份找到对局。
	for _, playerID := range playerIDs {
		m.byPlayer[playerID] = r
	}
	return r, nil
}

// Get 按 roomID 查找房间；找不到时返回 false。
func (m *Manager) Get(id string) (*Room, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	r, ok := m.rooms[id]
	return r, ok
}

// RoomOf 按 playerID 查找玩家所在房间；找不到时返回 false。
func (m *Manager) RoomOf(playerID string) (*Room, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	r, ok := m.byPlayer[playerID]
	return r, ok
}

// Remove 按 roomID 移除房间，返回是否真的删掉了记录。
func (m *Manager) Remove(id string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	r, ok := m.rooms[id]
	if !ok {
		return false
	}
	delete(m.rooms, id)
	// 房间销毁时清掉反向索引，避免旧玩家被路由到已结束的对局。
	for playerID := range r.Players {
		delete(m.byPlayer, playerID)
	}
	return true
}
