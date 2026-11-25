// package session 负责玩家身份与在线会话：生成 playerID、签发/校验 HMAC token、维护并发安全的会话表。
// 1.4 WebSocket 接入层会用 Authenticate 做连接鉴权；2.1 断线重连会在此基础上恢复会话。
package session

import (
	"crypto/rand"  // 生成不可预测的 playerID 和 token nonce
	"encoding/hex" // 把随机字节转成可读字符串
	"errors"       // 构造简单的校验错误
	"fmt"          // 格式化带上下文的错误
	"sync"         // 保护会话表的并发读写
	"time"         // 会话有效期
)

const (
	playerIDPrefix = "p_" // playerID 前缀，方便日志里一眼识别
	idBytes        = 8    // playerID 随机字节数，16 个十六进制字符
	nonceBytes     = 12   // token nonce 随机字节数
)

// Session 是一条已登录会话。Token 用于对端后续请求的鉴权，ExpiresAt 同时受 token 和会话表约束。
type Session struct {
	PlayerID  string    `json:"player_id"`
	Token     string    `json:"token"`
	IssuedAt  time.Time `json:"issued_at"`
	ExpiresAt time.Time `json:"expires_at"`
}

// Manager 把 token 编解码和会话表收在一起。
// secret 是 HMAC 密钥，生产环境应从配置/密钥管理系统注入，不能硬编码进代码。
type Manager struct {
	secret   []byte
	ttl      time.Duration
	now      func() time.Time // 注入时钟，便于测试精确控制过期边界
	mu       sync.RWMutex     // 保护 sessions
	sessions map[string]*Session
}

// NewManager 构造会话管理器。空密钥或非法 TTL 会让 token 可被伪造或永远不会过期，必须拒绝。
func NewManager(secret []byte, ttl time.Duration) (*Manager, error) {
	if len(secret) == 0 {
		return nil, errors.New("session: secret must not be empty")
	}
	if ttl <= 0 {
		return nil, errors.New("session: ttl must be positive")
	}
	return &Manager{
		secret:   append([]byte(nil), secret...), // 拷贝一份，避免调用方之后修改底层数组
		ttl:      ttl,
		now:      time.Now,
		sessions: make(map[string]*Session),
	}, nil
}

// randomHex 从 crypto/rand 读 n 个字节并转成十六进制字符串。
func randomHex(n int) (string, error) {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("session: read random bytes: %w", err)
	}
	return hex.EncodeToString(buf), nil
}

// NewPlayerID 生成一个随机的 playerID。
func (m *Manager) NewPlayerID() (string, error) {
	id, err := randomHex(idBytes)
	if err != nil {
		return "", err
	}
	return playerIDPrefix + id, nil
}

// Login 登录：playerID 为空则服务端生成；签发 token 并写入会话表。
// 同一个 playerID 再次登录会覆盖旧会话，旧 token 随之失效。
func (m *Manager) Login(playerID string) (*Session, error) {
	if playerID == "" {
		id, err := m.NewPlayerID()
		if err != nil {
			return nil, err
		}
		playerID = id
	}
	nonce, err := randomHex(nonceBytes)
	if err != nil {
		return nil, err
	}

	now := m.now()
	expiresAt := now.Add(m.ttl)
	token, err := m.sign(tokenClaims{
		PlayerID:  playerID,
		ExpiresAt: expiresAt.Unix(),
		Nonce:     nonce,
	})
	if err != nil {
		return nil, err
	}

	sess := &Session{
		PlayerID:  playerID,
		Token:     token,
		IssuedAt:  now,
		ExpiresAt: expiresAt,
	}
	m.mu.Lock()
	m.sessions[playerID] = sess
	m.mu.Unlock()

	stored, _ := m.Session(playerID) // 取回副本，避免调用方改写会话表里的对象
	return stored, nil
}

// Authenticate 校验 token 是否合法、是否属于声明的 playerID、是否仍在会话表中且未过期。
// 三步缺一不可：只看签名无法阻止 A 拿 B 的 token 冒充，只查会话表无法阻止伪造 token。
func (m *Manager) Authenticate(token, playerID string) (*Session, error) {
	claims, err := m.verify(token)
	if err != nil {
		return nil, err
	}
	if claims.PlayerID != playerID {
		return nil, fmt.Errorf("session: token player mismatch: token belongs to %q, got %q", claims.PlayerID, playerID)
	}

	sess, ok := m.Session(playerID)
	if !ok {
		return nil, fmt.Errorf("session: no active session for player %q", playerID)
	}
	if sess.Token != token {
		return nil, errors.New("session: stale token")
	}
	if !m.now().Before(sess.ExpiresAt) {
		return nil, errors.New("session: token expired")
	}
	return sess, nil
}

// Session 按 playerID 查询会话，返回副本；找不到时 ok 为 false。
func (m *Manager) Session(playerID string) (*Session, bool) {
	m.mu.RLock()
	s, ok := m.sessions[playerID]
	m.mu.RUnlock()
	if !ok {
		return nil, false
	}
	copyOfSession := *s
	return &copyOfSession, true
}

// Remove 删除会话（玩家退出登录或被踢出时调用）。
func (m *Manager) Remove(playerID string) {
	m.mu.Lock()
	delete(m.sessions, playerID)
	m.mu.Unlock()
}

// Len 返回当前在线会话数，主要用于测试与监控埋点。
func (m *Manager) Len() int {
	m.mu.RLock()
	n := len(m.sessions)
	m.mu.RUnlock()
	return n
}
