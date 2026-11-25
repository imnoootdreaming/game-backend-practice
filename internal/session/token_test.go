package session

import (
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"
)

// fixedClock 返回一个可控的假时钟：测试里替换 m.now 就能精确踩到过期边界
func fixedClock(t time.Time) func() time.Time {
	return func() time.Time { return t }
}

// newTestManager 构造一个测试用 Manager，避免每个用例重复处理 NewManager 的错误
func newTestManager(t *testing.T) *Manager {
	t.Helper()
	m, err := NewManager([]byte("test-secret"), time.Hour)
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}
	return m
}

// flipFirst 把字符串首字符在 A/B 之间切换，用来构造"仍然合法但内容不同"的 token 片段
func flipFirst(s string) string {
	if s == "" {
		return "A"
	}
	if s[0] == 'A' {
		return "B" + s[1:]
	}
	return "A" + s[1:]
}

// TestNewManagerErrors 错误路径：空密钥或非法 TTL 必须拒绝，不能签发"谁都能伪造"的 token
func TestNewManagerErrors(t *testing.T) {
	tests := []struct {
		name    string
		secret  []byte
		ttl     time.Duration
		wantErr string
	}{
		{"nil secret", nil, time.Hour, "secret"},
		{"empty secret", []byte{}, time.Hour, "secret"},
		{"zero ttl", []byte("s"), 0, "ttl"},
		{"negative ttl", []byte("s"), -time.Second, "ttl"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewManager(tt.secret, tt.ttl)
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("NewManager() error = %v, want containing %q", err, tt.wantErr)
			}
		})
	}
}

// TestLoginAndAuthenticate 正常路径：登录后拿到 token，校验通过且会话表里能查到同一条记录
func TestLoginAndAuthenticate(t *testing.T) {
	m := newTestManager(t)

	sess, err := m.Login("p1")
	if err != nil {
		t.Fatalf("Login() error = %v", err)
	}
	if sess.PlayerID != "p1" {
		t.Errorf("PlayerID = %q, want p1", sess.PlayerID)
	}
	if sess.Token == "" {
		t.Error("Token is empty")
	}
	if !sess.IssuedAt.Before(sess.ExpiresAt) {
		t.Errorf("IssuedAt %v is not before ExpiresAt %v", sess.IssuedAt, sess.ExpiresAt)
	}

	got, err := m.Authenticate(sess.Token, "p1")
	if err != nil {
		t.Fatalf("Authenticate() error = %v", err)
	}
	if got.PlayerID != "p1" || got.Token != sess.Token {
		t.Errorf("session = %#v, want p1 with token %q", got, sess.Token)
	}

	stored, ok := m.Session("p1")
	if !ok {
		t.Fatal("Session(p1) not found")
	}
	if !stored.ExpiresAt.Equal(sess.ExpiresAt) {
		t.Errorf("stored ExpiresAt = %v, want %v", stored.ExpiresAt, sess.ExpiresAt)
	}
}

// TestLoginGeneratesPlayerID 边界值：playerID 传空时服务端生成，且两次生成不能重复
func TestLoginGeneratesPlayerID(t *testing.T) {
	m := newTestManager(t)

	a, err := m.Login("")
	if err != nil {
		t.Fatalf("first Login() error = %v", err)
	}
	b, err := m.Login("")
	if err != nil {
		t.Fatalf("second Login() error = %v", err)
	}
	if a.PlayerID == "" || b.PlayerID == "" || a.PlayerID == b.PlayerID {
		t.Fatalf("generated IDs = %q and %q, want two distinct non-empty IDs", a.PlayerID, b.PlayerID)
	}
	if _, err := m.Authenticate(a.Token, a.PlayerID); err != nil {
		t.Errorf("Authenticate(generated) error = %v", err)
	}
}

// TestTokenTamperingRejected 错误路径：改签名、改 payload、token 格式不对都必须被拒
func TestTokenTamperingRejected(t *testing.T) {
	m := newTestManager(t)
	sess, err := m.Login("p1")
	if err != nil {
		t.Fatal(err)
	}
	parts := strings.Split(sess.Token, ".")
	if len(parts) != 2 {
		t.Fatalf("token = %q, want payload.signature", sess.Token)
	}

	tests := []struct {
		name    string
		token   string
		wantErr string
	}{
		{"tampered signature", strings.Join([]string{parts[0], flipFirst(parts[1])}, "."), "invalid signature"},
		{"tampered payload", strings.Join([]string{flipFirst(parts[0]), parts[1]}, "."), "invalid signature"},
		{"missing dot", "not-a-token", "malformed token"},
		{"empty token", "", "malformed token"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := m.Authenticate(tt.token, "p1"); err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("Authenticate() error = %v, want containing %q", err, tt.wantErr)
			}
		})
	}
}

// TestExpiredTokenRejected 错误路径 + 边界值：用假时钟验证到期前一秒可用、刚好到期和过期后都被拒
func TestExpiredTokenRejected(t *testing.T) {
	m, err := NewManager([]byte("test-secret"), time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	t0 := time.Date(2026, 11, 25, 12, 0, 0, 0, time.UTC)
	m.now = fixedClock(t0)

	sess, err := m.Login("p1")
	if err != nil {
		t.Fatal(err)
	}

	m.now = fixedClock(t0.Add(time.Hour - time.Second))
	if _, err := m.Authenticate(sess.Token, "p1"); err != nil {
		t.Fatalf("Authenticate(before expiry) error = %v", err)
	}

	m.now = fixedClock(t0.Add(time.Hour)) // 刚好到期也算过期
	if _, err := m.Authenticate(sess.Token, "p1"); err == nil || !strings.Contains(err.Error(), "expired") {
		t.Fatalf("Authenticate(at expiry) error = %v, want containing %q", err, "expired")
	}

	m.now = fixedClock(t0.Add(time.Hour + time.Second))
	if _, err := m.Authenticate(sess.Token, "p1"); err == nil || !strings.Contains(err.Error(), "expired") {
		t.Fatalf("Authenticate(after expiry) error = %v, want containing %q", err, "expired")
	}
}

// TestTokenNotInterchangeable 错误路径：p1 的 token 不能冒充 p2，重新登录后旧 token 也要失效
func TestTokenNotInterchangeable(t *testing.T) {
	m := newTestManager(t)

	p1, err := m.Login("p1")
	if err != nil {
		t.Fatal(err)
	}
	p2, err := m.Login("p2")
	if err != nil {
		t.Fatal(err)
	}

	t.Run("other player", func(t *testing.T) {
		if _, err := m.Authenticate(p1.Token, "p2"); err == nil || !strings.Contains(err.Error(), "mismatch") {
			t.Fatalf("Authenticate(p1 token as p2) error = %v, want containing %q", err, "mismatch")
		}
		if _, err := m.Authenticate(p2.Token, "p1"); err == nil || !strings.Contains(err.Error(), "mismatch") {
			t.Fatalf("Authenticate(p2 token as p1) error = %v, want containing %q", err, "mismatch")
		}
	})

	t.Run("stale token after relogin", func(t *testing.T) {
		first, err := m.Login("p3")
		if err != nil {
			t.Fatal(err)
		}
		second, err := m.Login("p3")
		if err != nil {
			t.Fatal(err)
		}
		if first.Token == second.Token {
			t.Fatal("two logins produced the same token, want unique tokens")
		}
		if _, err := m.Authenticate(first.Token, "p3"); err == nil || !strings.Contains(err.Error(), "stale token") {
			t.Fatalf("Authenticate(old token) error = %v, want containing %q", err, "stale token")
		}
	})
}

// TestSessionStoreConcurrentAccess 并发安全：多个 goroutine 同时登录、查询、校验、删除，不应 panic 或串号
func TestSessionStoreConcurrentAccess(t *testing.T) {
	m := newTestManager(t)
	const n = 100
	var wg sync.WaitGroup

	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			id := fmt.Sprintf("p_%d", i)

			sess, err := m.Login(id)
			if err != nil {
				t.Errorf("Login(%s) error = %v", id, err)
				return
			}
			if got, ok := m.Session(id); !ok || got.Token != sess.Token {
				t.Errorf("Session(%s) = %#v, ok = %v", id, got, ok)
			}
			if _, err := m.Authenticate(sess.Token, id); err != nil {
				t.Errorf("Authenticate(%s) error = %v", id, err)
			}

			m.Remove(id)
			if _, ok := m.Session(id); ok {
				t.Errorf("Session(%s) still exists after Remove", id)
			}
		}(i)
	}
	wg.Wait()

	if got := m.Len(); got != 0 {
		t.Errorf("Len() = %d, want 0", got)
	}
}
