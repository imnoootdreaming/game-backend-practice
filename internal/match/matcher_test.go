package match

import (
	"context"
	"reflect"
	"testing"
	"time"
)

func TestNewMatcherRejectsInvalidArgs(t *testing.T) {
	noop := func(Result) error { return nil }
	cases := []struct {
		name      string             // 用例名
		queue     *Queue             // 待校验队列
		teamSize  int                // 目标人数
		onMatched func(Result) error // 成局回调
	}{
		{"nil queue", nil, 4, noop},
		{"zero team size", NewQueue(), 0, noop},
		{"nil callback", NewQueue(), 4, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := NewMatcher(tc.queue, tc.teamSize, tc.onMatched); err == nil {
				t.Fatal("NewMatcher() error = nil, want non-nil")
			}
		})
	}
}

func TestMatcherScanOnceWaitsUntilEnoughPlayers(t *testing.T) {
	base := time.Date(2026, 11, 26, 10, 0, 0, 0, time.UTC)
	q := NewQueue()
	for i, id := range []string{"p1", "p2", "p3"} {
		e := newTestEntry(id, "party-"+id, 1, 1000, base.Add(time.Duration(i)*time.Second))
		if _, err := q.Enqueue(e); err != nil {
			t.Fatalf("Enqueue(%s) error = %v", id, err)
		}
	}

	called := 0
	m, err := NewMatcher(q, 4, func(Result) error { called++; return nil })
	if err != nil {
		t.Fatalf("NewMatcher() error = %v", err)
	}
	results, err := m.ScanOnce(base.Add(time.Minute))
	if err != nil {
		t.Fatalf("ScanOnce() error = %v", err)
	}
	if len(results) != 0 {
		t.Fatalf("ScanOnce() results = %+v, want empty", results)
	}
	if called != 0 {
		t.Fatalf("callback called %d times, want 0", called)
	}
	if q.Len() != 3 {
		t.Fatalf("queue length = %d, want 3", q.Len())
	}
}

func TestMatcherScanOnceCreatesMatch(t *testing.T) {
	base := time.Date(2026, 11, 26, 10, 0, 0, 0, time.UTC)
	q := NewQueue()
	// 故意乱序入队，验证按等待时间选出玩家。
	for _, e := range []Entry{
		newTestEntry("p4", "party-p4", 1, 1000, base.Add(3*time.Second)),
		newTestEntry("p1", "party-p1", 1, 1000, base),
		newTestEntry("p3", "party-p3", 1, 1000, base.Add(2*time.Second)),
		newTestEntry("p2", "party-p2", 1, 1000, base.Add(time.Second)),
	} {
		if _, err := q.Enqueue(e); err != nil {
			t.Fatalf("Enqueue(%s) error = %v", e.PlayerID, err)
		}
	}

	var notified Result
	called := 0
	m, err := NewMatcher(q, 4, func(r Result) error {
		called++
		notified = r
		return nil
	})
	if err != nil {
		t.Fatalf("NewMatcher() error = %v", err)
	}
	results, err := m.ScanOnce(base.Add(time.Minute))
	if err != nil {
		t.Fatalf("ScanOnce() error = %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("ScanOnce() results count = %d, want 1", len(results))
	}
	wantIDs := []string{"p1", "p2", "p3", "p4"}
	if !reflect.DeepEqual(results[0].PlayerIDs, wantIDs) {
		t.Fatalf("matched players = %v, want %v", results[0].PlayerIDs, wantIDs)
	}
	if results[0].MatchID == "" {
		t.Fatal("MatchID is empty, want non-empty")
	}
	if called != 1 {
		t.Fatalf("callback called %d times, want 1", called)
	}
	if !reflect.DeepEqual(notified, results[0]) {
		t.Fatalf("notified = %+v, want %+v", notified, results[0])
	}
	if q.Len() != 0 {
		t.Fatalf("queue length = %d, want 0", q.Len())
	}
}

func TestMatcherScanOnceRejectsRankGapBeyondInitialTolerance(t *testing.T) {
	base := time.Date(2026, 11, 26, 10, 0, 0, 0, time.UTC)
	q := NewQueue()
	for _, e := range []Entry{
		newTestEntry("p1", "party-p1", 1, 1000, base),
		newTestEntry("p2", "party-p2", 1, 1150, base), // 差 150，超过初始容差 100
	} {
		if _, err := q.Enqueue(e); err != nil {
			t.Fatalf("Enqueue(%s) error = %v", e.PlayerID, err)
		}
	}

	called := 0
	m, err := NewMatcher(q, 2, func(Result) error { called++; return nil })
	if err != nil {
		t.Fatalf("NewMatcher() error = %v", err)
	}
	results, err := m.ScanOnce(base)
	if err != nil {
		t.Fatalf("ScanOnce() error = %v", err)
	}
	if len(results) != 0 {
		t.Fatalf("ScanOnce() results = %+v, want empty", results)
	}
	if called != 0 {
		t.Fatalf("callback called %d times, want 0", called)
	}
	if q.Len() != 2 {
		t.Fatalf("queue length = %d, want 2", q.Len())
	}
}

func TestMatcherScanOnceAcceptsRankGapWithinTolerance(t *testing.T) {
	base := time.Date(2026, 11, 26, 10, 0, 0, 0, time.UTC)
	q := NewQueue()
	for _, e := range []Entry{
		newTestEntry("p1", "party-p1", 1, 1000, base),
		newTestEntry("p2", "party-p2", 1, 1080, base), // 差 80，在初始容差 100 内
	} {
		if _, err := q.Enqueue(e); err != nil {
			t.Fatalf("Enqueue(%s) error = %v", e.PlayerID, err)
		}
	}

	m, err := NewMatcher(q, 2, func(Result) error { return nil })
	if err != nil {
		t.Fatalf("NewMatcher() error = %v", err)
	}
	results, err := m.ScanOnce(base)
	if err != nil {
		t.Fatalf("ScanOnce() error = %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("ScanOnce() results count = %d, want 1", len(results))
	}
	wantIDs := []string{"p1", "p2"}
	if !reflect.DeepEqual(results[0].PlayerIDs, wantIDs) {
		t.Fatalf("matched players = %v, want %v", results[0].PlayerIDs, wantIDs)
	}
}

func TestMatcherScanOnceWidensRankToleranceWithWait(t *testing.T) {
	base := time.Date(2026, 11, 26, 10, 0, 0, 0, time.UTC)
	q := NewQueue()
	for _, e := range []Entry{
		newTestEntry("p1", "party-p1", 1, 1000, base),
		newTestEntry("p2", "party-p2", 1, 1150, base), // 差 150：初始不匹配，等过 30s 后容差变 200
	} {
		if _, err := q.Enqueue(e); err != nil {
			t.Fatalf("Enqueue(%s) error = %v", e.PlayerID, err)
		}
	}

	called := 0
	m, err := NewMatcher(q, 2, func(Result) error { called++; return nil })
	if err != nil {
		t.Fatalf("NewMatcher() error = %v", err)
	}
	before, err := m.ScanOnce(base.Add(29 * time.Second))
	if err != nil {
		t.Fatalf("ScanOnce(before) error = %v", err)
	}
	if len(before) != 0 {
		t.Fatalf("ScanOnce(before) results = %+v, want empty", before)
	}
	if called != 0 {
		t.Fatalf("callback called %d times before widen, want 0", called)
	}

	after, err := m.ScanOnce(base.Add(31 * time.Second))
	if err != nil {
		t.Fatalf("ScanOnce(after) error = %v", err)
	}
	if len(after) != 1 {
		t.Fatalf("ScanOnce(after) results count = %d, want 1", len(after))
	}
	wantIDs := []string{"p1", "p2"}
	if !reflect.DeepEqual(after[0].PlayerIDs, wantIDs) {
		t.Fatalf("matched players = %v, want %v", after[0].PlayerIDs, wantIDs)
	}
	if called != 1 {
		t.Fatalf("callback called %d times after widen, want 1", called)
	}
	if q.Len() != 0 {
		t.Fatalf("queue length = %d, want 0", q.Len())
	}
}

func TestNewMatcherWithConfigRejectsInvalidConfig(t *testing.T) {
	noop := func(Result) error { return nil }
	valid := Config{
		TeamSize:          2,
		BaseRankTolerance: 10,
		RankWidenStep:     5,
		RankWidenInterval: 10 * time.Second,
	}
	cases := []struct {
		name      string             // 用例名
		queue     *Queue             // 待校验队列
		cfg       Config             // 匹配配置
		onMatched func(Result) error // 成局回调
	}{
		{"nil queue", nil, valid, noop},
		{"zero team size", NewQueue(), Config{TeamSize: 0, BaseRankTolerance: 10, RankWidenStep: 5, RankWidenInterval: time.Second}, noop},
		{"negative base tolerance", NewQueue(), Config{TeamSize: 2, BaseRankTolerance: -1, RankWidenStep: 5, RankWidenInterval: time.Second}, noop},
		{"negative widen step", NewQueue(), Config{TeamSize: 2, BaseRankTolerance: 10, RankWidenStep: -1, RankWidenInterval: time.Second}, noop},
		{"zero widen interval", NewQueue(), Config{TeamSize: 2, BaseRankTolerance: 10, RankWidenStep: 5, RankWidenInterval: 0}, noop},
		{"nil callback", NewQueue(), valid, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := NewMatcherWithConfig(tc.queue, tc.cfg, tc.onMatched); err == nil {
				t.Fatal("NewMatcherWithConfig() error = nil, want non-nil")
			}
		})
	}
}

func TestDefaultConfigUsesDocumentedValues(t *testing.T) {
	cfg := DefaultConfig(4)
	if cfg.TeamSize != 4 {
		t.Fatalf("TeamSize = %d, want 4", cfg.TeamSize)
	}
	if cfg.BaseRankTolerance != 100 {
		t.Fatalf("BaseRankTolerance = %d, want 100", cfg.BaseRankTolerance)
	}
	if cfg.RankWidenStep != 100 {
		t.Fatalf("RankWidenStep = %d, want 100", cfg.RankWidenStep)
	}
	if cfg.RankWidenInterval != 30*time.Second {
		t.Fatalf("RankWidenInterval = %s, want 30s", cfg.RankWidenInterval)
	}
}

func TestMatcherWithConfigUsesCustomRankTolerance(t *testing.T) {
	base := time.Date(2026, 11, 26, 10, 0, 0, 0, time.UTC)
	q := NewQueue()
	for _, e := range []Entry{
		newTestEntry("p1", "party-p1", 1, 1000, base),
		newTestEntry("p2", "party-p2", 1, 1015, base), // 差 15：默认容差 100 会立即匹配，自定义 10 不会
	} {
		if _, err := q.Enqueue(e); err != nil {
			t.Fatalf("Enqueue(%s) error = %v", e.PlayerID, err)
		}
	}

	cfg := Config{
		TeamSize:          2,
		BaseRankTolerance: 10,
		RankWidenStep:     5,
		RankWidenInterval: 10 * time.Second,
	}
	called := 0
	m, err := NewMatcherWithConfig(q, cfg, func(Result) error { called++; return nil })
	if err != nil {
		t.Fatalf("NewMatcherWithConfig() error = %v", err)
	}
	before, err := m.ScanOnce(base)
	if err != nil {
		t.Fatalf("ScanOnce(before) error = %v", err)
	}
	if len(before) != 0 {
		t.Fatalf("ScanOnce(before) results = %+v, want empty", before)
	}

	after, err := m.ScanOnce(base.Add(11 * time.Second))
	if err != nil {
		t.Fatalf("ScanOnce(after) error = %v", err)
	}
	if len(after) != 1 {
		t.Fatalf("ScanOnce(after) results count = %d, want 1", len(after))
	}
	wantIDs := []string{"p1", "p2"}
	if !reflect.DeepEqual(after[0].PlayerIDs, wantIDs) {
		t.Fatalf("matched players = %v, want %v", after[0].PlayerIDs, wantIDs)
	}
	if called != 1 {
		t.Fatalf("callback called %d times, want 1", called)
	}
}

func TestMatcherRunRejectsNonPositiveInterval(t *testing.T) {
	m, err := NewMatcher(NewQueue(), 2, func(Result) error { return nil })
	if err != nil {
		t.Fatalf("NewMatcher() error = %v", err)
	}
	for _, interval := range []time.Duration{0, -time.Second} {
		if err := m.Run(context.Background(), interval); err == nil {
			t.Fatalf("Run(interval=%s) error = nil, want non-nil", interval)
		}
	}
}

func TestMatcherRunScansUntilMatch(t *testing.T) {
	q := NewQueue()
	enqueued := time.Now().Add(-time.Hour)
	for _, e := range []Entry{
		newTestEntry("p1", "party-p1", 1, 1000, enqueued),
		newTestEntry("p2", "party-p2", 1, 1000, enqueued),
	} {
		if _, err := q.Enqueue(e); err != nil {
			t.Fatalf("Enqueue(%s) error = %v", e.PlayerID, err)
		}
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	resultCh := make(chan Result, 1)
	m, err := NewMatcher(q, 2, func(r Result) error {
		resultCh <- r
		cancel()
		return nil
	})
	if err != nil {
		t.Fatalf("NewMatcher() error = %v", err)
	}
	errCh := make(chan error, 1)
	go func() { errCh <- m.Run(ctx, 5*time.Millisecond) }()

	select {
	case r := <-resultCh:
		if r.MatchID == "" {
			t.Fatal("MatchID is empty, want non-empty")
		}
	case <-time.After(time.Second):
		t.Fatal("Run did not trigger a match within 1s")
	}
	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("Run() error = %v, want nil", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Run did not stop after callback cancelled context")
	}
	if q.Len() != 0 {
		t.Fatalf("queue length = %d, want 0", q.Len())
	}
}

func TestMatcherRunStopsOnContextCancel(t *testing.T) {
	m, err := NewMatcher(NewQueue(), 2, func(Result) error { return nil })
	if err != nil {
		t.Fatalf("NewMatcher() error = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() { errCh <- m.Run(ctx, time.Hour) }()
	cancel()

	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("Run() error = %v, want nil", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Run did not stop after context cancel")
	}
}
