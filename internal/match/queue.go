// Package match 是匹配模块：玩家点“开始匹配”后，先在这里排队，等人数够了、段位也接近时再一起进房间。
// 它接在大厅后面：lobby.Handler 把玩家放进 Queue，后面的 Matcher 再从 Queue 里挑人。
package match

import (
	"errors"
	"sort"
	"sync"
	"time"
)

// Entry 是队列里的一条排队记录，代表一个正在等匹配的玩家。
// 同一支队伍的几个人各有一条记录，但用同一个 PartyID 绑在一起，匹配时不会把队友拆开。
type Entry struct {
	PlayerID   string    // 玩家 ID，来自登录鉴权，客户端自己说是什么就是什么不算数
	PartyID    string    // 队伍 ID，同队的人这个值相同
	PartySize  int       // 这支队伍几个人，用来按人数分组排队
	Rank       int       // 玩家段位分，用来找段位接近的对手
	EnqueuedAt time.Time // 什么时候开始排的队，等得越久越优先
}

// Queue 是一张“等待匹配的名单”，玩家点匹配后就记在这里。
// 名单以玩家 ID 为键，同一个人重复点匹配只保留一条；名单里只放数据，不放网络连接。
type Queue struct {
	mu      sync.RWMutex     // 保护名单的并发读写，避免多个协程同时改时出错
	entries map[string]Entry // 等待名单：玩家 ID 对应他的排队记录
}

// NewQueue 创建一张空的等待名单。
func NewQueue() *Queue {
	return &Queue{entries: make(map[string]Entry)}
}

// Enqueue 把玩家加进等待名单。
// 已经排过队的玩家不会重复加入，也不会刷新他的排队时间；数据不合法就直接拒绝。
func (q *Queue) Enqueue(e Entry) (bool, error) {
	if e.PlayerID == "" {
		return false, errors.New("match: player id must not be empty")
	}
	if e.PartyID == "" {
		return false, errors.New("match: party id must not be empty")
	}
	if e.PartySize <= 0 {
		return false, errors.New("match: party size must be positive")
	}
	if e.Rank < 0 {
		return false, errors.New("match: rank must not be negative")
	}
	if e.EnqueuedAt.IsZero() {
		return false, errors.New("match: enqueued time must not be zero")
	}

	q.mu.Lock()
	defer q.mu.Unlock()
	if _, exists := q.entries[e.PlayerID]; exists {
		return false, nil
	}
	q.entries[e.PlayerID] = e
	return true, nil
}

// Get 查看某个玩家的排队记录；没在排队就返回找不到。
func (q *Queue) Get(playerID string) (Entry, bool) {
	q.mu.RLock()
	defer q.mu.RUnlock()
	e, ok := q.entries[playerID]
	return e, ok
}

// Remove 把玩家从等待名单里删掉，比如他断线了或者匹配成功了。
// 玩家本来就不在名单里也不算错。
func (q *Queue) Remove(playerID string) bool {
	q.mu.Lock()
	defer q.mu.Unlock()
	if _, ok := q.entries[playerID]; !ok {
		return false
	}
	delete(q.entries, playerID)
	return true
}

// RemoveAll 一次性删掉一批玩家，匹配成功时用它清空这一局的参与者。
// 名单里没有的 ID 直接忽略。
func (q *Queue) RemoveAll(ids []string) {
	q.mu.Lock()
	defer q.mu.Unlock()
	for _, id := range ids {
		delete(q.entries, id)
	}
}

// Candidates 找出“人数符合要求、段位在指定范围内”的玩家，按排队时间从早到晚返回。
// 等得越久排得越前；返回的是记录副本，调用方改不到名单本身。
func (q *Queue) Candidates(partySize, minRank, maxRank int) []Entry {
	if minRank > maxRank {
		return nil
	}
	q.mu.RLock()
	defer q.mu.RUnlock()

	out := make([]Entry, 0, len(q.entries))
	// 先把人数符合要求、段位落在区间内的玩家挑出来
	for _, e := range q.entries {
		if e.PartySize == partySize && e.Rank >= minRank && e.Rank <= maxRank {
			out = append(out, e)
		}
	}
	// 等得久的排前面；同一时间入队的按 ID 排，保证每次结果一样
	sort.Slice(out, func(i, j int) bool {
		if out[i].EnqueuedAt.Equal(out[j].EnqueuedAt) {
			return out[i].PlayerID < out[j].PlayerID
		}
		return out[i].EnqueuedAt.Before(out[j].EnqueuedAt)
	})
	return out
}

// Len 返回当前有多少人在排队。
func (q *Queue) Len() int {
	q.mu.RLock()
	defer q.mu.RUnlock()
	return len(q.entries)
}
