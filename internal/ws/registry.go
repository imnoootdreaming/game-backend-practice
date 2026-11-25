// package ws 负责 WebSocket 接入层：连接升级、注册表、心跳、读写泵与消息路由。
package ws

import (
	"fmt"  // 格式化重复注册的错误
	"sync" // 保护并发读写的连接表
)

// Peer 是注册表里的一条连接所需的最小能力。
// 用接口而不是直接依赖 *Conn，让注册表可以独立做单元测试，不碰真实网络。
type Peer interface {
	ID() string
	Close() error
}

// Registry 是 playerID → 连接的并发安全注册表。
// 同一 playerID 只允许一条连接在线，避免两条连接同时改权威状态时互相覆盖。
type Registry struct {
	mu    sync.RWMutex
	peers map[string]Peer
}

// NewRegistry 创建空注册表。
func NewRegistry() *Registry {
	return &Registry{peers: make(map[string]Peer)}
}

// Add 注册一条连接；ID 为空或已注册都返回错误，不覆盖原有连接。
func (r *Registry) Add(p Peer) error {
	if p == nil || p.ID() == "" {
		return fmt.Errorf("ws: invalid peer")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.peers[p.ID()]; ok {
		return fmt.Errorf("ws: peer %q already registered", p.ID())
	}
	r.peers[p.ID()] = p
	return nil
}

// Remove 注销一条连接，返回是否真的删掉了记录。
func (r *Registry) Remove(id string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.peers[id]; !ok {
		return false
	}
	delete(r.peers, id)
	return true
}

// Get 按 playerID 查连接。
func (r *Registry) Get(id string) (Peer, bool) {
	r.mu.RLock()
	p, ok := r.peers[id]
	r.mu.RUnlock()
	return p, ok
}

// Len 返回当前在线连接数。
func (r *Registry) Len() int {
	r.mu.RLock()
	n := len(r.peers)
	r.mu.RUnlock()
	return n
}

// IDs 返回当前在线玩家的 ID 列表（顺序不保证），用于日志和测试断言。
func (r *Registry) IDs() []string {
	r.mu.RLock()
	ids := make([]string, 0, len(r.peers))
	for id := range r.peers {
		ids = append(ids, id)
	}
	r.mu.RUnlock()
	return ids
}
