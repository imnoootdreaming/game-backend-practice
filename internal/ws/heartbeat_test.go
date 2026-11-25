package ws

import (
	"sync"
	"testing"
	"time"
)

// fakeClock 是可控时钟，测试通过 Advance 精确踩到心跳超时边界
type fakeClock struct {
	mu sync.Mutex
	t  time.Time
}

func (f *fakeClock) Now() time.Time {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.t
}

func (f *fakeClock) Advance(d time.Duration) {
	f.mu.Lock()
	f.t = f.t.Add(d)
	f.mu.Unlock()
}

// TestHeartbeatWithFakeClock 正常路径 + 边界值：pong 重置计时，刚好到超时点算超时
func TestHeartbeatWithFakeClock(t *testing.T) {
	fc := &fakeClock{t: time.Unix(0, 0)}
	h := NewHeartbeat(30*time.Second, fc.Now)

	if h.Expired() {
		t.Fatal("new heartbeat is already expired")
	}

	fc.Advance(10 * time.Second)
	if h.Expired() {
		t.Fatal("expired after 10s, want alive")
	}

	fc.Advance(20 * time.Second) // 累计 30s，刚好到期
	if !h.Expired() {
		t.Fatal("not expired at exactly 30s")
	}

	h.Beat() // 收到 pong，重新计时
	if h.Expired() {
		t.Fatal("expired right after Beat()")
	}
	fc.Advance(29 * time.Second)
	if h.Expired() {
		t.Fatal("expired after 29s since Beat(), want alive")
	}
	fc.Advance(time.Second)
	if !h.Expired() {
		t.Fatal("not expired after 30s since Beat()")
	}
}
