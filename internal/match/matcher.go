package match

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"
)

const (
	baseRankTolerance = 100              // 初始段位容差
	rankWidenStep     = 100              // 每放宽一次增加的容差
	rankWidenInterval = 30 * time.Second // 放宽一次的等待时长
)

// Config 是匹配器的可调参数。
type Config struct {
	TeamSize          int           // 一局需要的人数
	BaseRankTolerance int           // 初始段位容差
	RankWidenStep     int           // 每放宽一次增加的容差
	RankWidenInterval time.Duration // 放宽一次的等待时长
}

// DefaultConfig 返回与原先硬编码值一致的一组默认参数。
func DefaultConfig(teamSize int) Config {
	return Config{
		TeamSize:          teamSize,
		BaseRankTolerance: baseRankTolerance,
		RankWidenStep:     rankWidenStep,
		RankWidenInterval: rankWidenInterval,
	}
}

// Result 是一次匹配成功的结果。
type Result struct {
	MatchID   string   // 本局匹配 ID
	PlayerIDs []string // 本局玩家列表
}

// Matcher 扫描 Queue，把凑够人数的玩家组成一局。
// 每次扫描最多成局一次，段位参数由 Config 提供。
type Matcher struct {
	mu                sync.Mutex         // 保护 ID 自增的锁
	queue             *Queue             // 等待匹配的队列
	teamSize          int                // 一局需要的人数
	baseRankTolerance int                // 初始段位容差
	rankWidenStep     int                // 每放宽一次增加的容差
	rankWidenInterval time.Duration      // 放宽一次的等待时长
	onMatched         func(Result) error // 成局后的通知回调
	nextMatchID       int                // 下一局的序号
}

// NewMatcher 用默认参数创建匹配器；旧调用方无需感知 Config。
func NewMatcher(queue *Queue, teamSize int, onMatched func(Result) error) (*Matcher, error) {
	return NewMatcherWithConfig(queue, DefaultConfig(teamSize), onMatched)
}

// NewMatcherWithConfig 创建匹配器；配置不合法时拒绝启动，避免服务带着错误配置运行。
func NewMatcherWithConfig(queue *Queue, cfg Config, onMatched func(Result) error) (*Matcher, error) {
	if queue == nil {
		return nil, errors.New("match: queue must not be nil")
	}
	if cfg.TeamSize <= 0 {
		return nil, errors.New("match: team size must be positive")
	}
	if cfg.BaseRankTolerance < 0 {
		return nil, errors.New("match: base rank tolerance must not be negative")
	}
	if cfg.RankWidenStep < 0 {
		return nil, errors.New("match: rank widen step must not be negative")
	}
	if cfg.RankWidenInterval <= 0 {
		return nil, errors.New("match: rank widen interval must be positive")
	}
	if onMatched == nil {
		return nil, errors.New("match: onMatched callback must not be nil")
	}
	return &Matcher{
		queue:             queue,
		teamSize:          cfg.TeamSize,
		baseRankTolerance: cfg.BaseRankTolerance,
		rankWidenStep:     cfg.RankWidenStep,
		rankWidenInterval: cfg.RankWidenInterval,
		onMatched:         onMatched,
		nextMatchID:       1,
	}, nil
}

// ScanOnce 扫描一次队列。人数不足或段位超出容许范围时什么都不做；凑满 teamSize 后生成结果并通知上层。
func (m *Matcher) ScanOnce(now time.Time) ([]Result, error) {
	ids := m.selectPlayerIDs(now)
	if len(ids) == 0 {
		return nil, nil
	}

	m.mu.Lock()
	id := fmt.Sprintf("match-%d", m.nextMatchID)
	m.nextMatchID++
	m.mu.Unlock()

	result := Result{MatchID: id, PlayerIDs: ids}
	if err := m.onMatched(result); err != nil {
		return nil, fmt.Errorf("match: notify match %s: %w", id, err)
	}
	m.queue.RemoveAll(ids)
	return []Result{result}, nil
}

// Run 按 interval 周期扫描队列，直到 ctx 取消或扫描出错。
// ctx 是上下文：调用方取消它（例如服务关闭）时，Run 应当立即停止。
func (m *Matcher) Run(ctx context.Context, interval time.Duration) error {
	if interval <= 0 {
		return errors.New("match: scan interval must be positive")
	}
	ticker := time.NewTicker(interval)
	// 用 defer 保证无论从哪个分支退出，定时器都会被释放。
	defer ticker.Stop()
	// select 会阻塞，直到某个 case 就绪；哪个先到就执行哪个。
	// 这里同时等待“定时信号”和“取消信号”，取消时能立刻退出，不用等下一个 tick。
	for {
		select {
		case <-ctx.Done(): // 外部创建 go 协程轮询，如果外部结束协程也需要结束，所以需要用 ctx.Done() 信号
			// ctx.Done() 在上下文被取消时关闭，收到这个信号就正常退出循环。
			return nil
		case now := <-ticker.C:
			// ticker.C 每隔 interval 送来一个时间信号，用它触发一次队列扫描。
			if _, err := m.ScanOnce(now); err != nil {
				return err
			}
		}
	}
}

// selectPlayerIDs 按入队时间挑整队，直到凑满 teamSize。
// 段位区间以最早入队队伍为基准，容差随它的等待时间放宽；同队玩家必须一起进房间。
func (m *Matcher) selectPlayerIDs(now time.Time) []string {
	allRanks := int(^uint(0) >> 1) // 先取全段位候选，段位窗口在下面统一套用。
	byParty := make(map[string]Entry)
	for size := 1; size <= m.teamSize; size++ {
		for _, e := range m.queue.Candidates(size, 0, allRanks) {
			if _, ok := byParty[e.PartyID]; !ok {
				byParty[e.PartyID] = e
			}
		}
	}
	parties := make([]Entry, 0, len(byParty))
	for _, e := range byParty {
		parties = append(parties, e)
	}
	// 按入队时间排序，时间相同用 PartyID 兜底，保证挑选结果稳定。
	sort.Slice(parties, func(i, j int) bool {
		if parties[i].EnqueuedAt.Equal(parties[j].EnqueuedAt) {
			return parties[i].PartyID < parties[j].PartyID
		}
		return parties[i].EnqueuedAt.Before(parties[j].EnqueuedAt)
	})
	if len(parties) == 0 {
		return nil
	}

	// 用最早排队的玩家当段位基准；他等得越久，允许的段位范围越宽。
	anchor := parties[0]
	tolerance := m.rankTolerance(now, anchor.EnqueuedAt)
	minRank := anchor.Rank - tolerance
	maxRank := anchor.Rank + tolerance

	remaining := m.teamSize
	ids := make([]string, 0, m.teamSize)
	for _, party := range parties {
		// 段位窗口外的队伍先跳过，避免段位差太大时过早同局。
		if party.Rank < minRank || party.Rank > maxRank {
			continue
		}
		// 放不下整队就跳过，避免把一队人拆到两局。
		if party.PartySize <= 0 || party.PartySize > remaining {
			continue
		}
		members := make([]string, 0, party.PartySize)
		for _, e := range m.queue.Candidates(party.PartySize, minRank, maxRank) {
			// 只取同一 PartyID 的成员，保证整队进出。
			if e.PartyID == party.PartyID {
				members = append(members, e.PlayerID)
			}
		}
		if len(members) != party.PartySize {
			continue
		}
		ids = append(ids, members...)
		remaining -= len(members)
		if remaining == 0 {
			break
		}
	}
	if remaining != 0 {
		return nil
	}
	return ids
}

// rankTolerance 按等待时长计算允许的段位偏差；等待越久，可接受的段位范围越宽。
func (m *Matcher) rankTolerance(now, enqueuedAt time.Time) int {
	waited := now.Sub(enqueuedAt)
	if waited < 0 {
		waited = 0
	}
	// 每等满一个间隔放宽一档，用配置里的步长累加。
	steps := int(waited / m.rankWidenInterval)
	return m.baseRankTolerance + steps*m.rankWidenStep
}
