package ws

import (
	"sync" // Beat 和 Expired 来自不同 goroutine，必须加锁
	"time" // 心跳按时间判断
)

// Ticker 抽象定时器，让写泵的心跳循环可以用假 ticker 做确定性测试。
type Ticker interface {
	C() <-chan time.Time
	Stop()
}

// realTicker 把标准库 time.Ticker 包装成 Ticker 接口。
type realTicker struct {
	t *time.Ticker // 被包装的标准库定时器
}

func (r realTicker) C() <-chan time.Time { return r.t.C }
func (r realTicker) Stop()               { r.t.Stop() }

// Heartbeat 记录最后一次收到 pong 的时间。
// 超时判定不依赖具体连接实现，测试可以注入假时钟精确踩边界。
type Heartbeat struct {
	mu      sync.Mutex       // 保护 now/timeout/last 的并发读写
	now     func() time.Time // 可注入时钟，便于测试踩超时边界
	timeout time.Duration    // 超过该时长未收到 pong 视为掉线
	last    time.Time        // 最后一次收到 pong 的时间
}

// NewHeartbeat 创建心跳状态，初始时间取当前时间。
func NewHeartbeat(timeout time.Duration, now func() time.Time) *Heartbeat {
	if now == nil {
		now = time.Now
	}
	return &Heartbeat{now: now, timeout: timeout, last: now()}
}

// Beat 在收到 pong 时调用，重新开始计时。
func (h *Heartbeat) Beat() {
	h.mu.Lock()
	h.last = h.now()
	h.mu.Unlock()
}

// Expired 判断是否超过 timeout 没有收到 pong；刚好等于 timeout 也算超时。
func (h *Heartbeat) Expired() bool {
	h.mu.Lock()
	deadline := h.last.Add(h.timeout)
	h.mu.Unlock()
	return !h.now().Before(deadline)
}
