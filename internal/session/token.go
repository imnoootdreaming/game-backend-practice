package session

import (
	"crypto/hmac"     // 用常量时间比较签名，防止计时侧信道
	"crypto/sha256"   // HMAC-SHA256 摘要算法
	"encoding/base64" // token 片段用 URL 安全的 base64，避免 + / = 在 URL/JSON 里被转义
	"encoding/json"   // 身份信息序列化成 JSON
	"errors"          // 构造简单的校验错误
	"fmt"             // 格式化带上下文的错误
	"strings"         // 按 "." 拆分 token
	"time"            // 过期时间判断
)

// tokenClaims 是放进 token 的身份信息。
// 注意 token 只证明"持有人是某个 playerID 且未过期"，真正的在线会话还要查会话表。
type tokenClaims struct {
	PlayerID  string `json:"pid"`   // 玩家 ID
	ExpiresAt int64  `json:"exp"`   // 过期时间，Unix 秒
	Nonce     string `json:"nonce"` // 随机数，保证同一玩家重复登录也能拿到不同 token
}

// sign 生成 "base64url(payload).base64url(signature)" 格式的 token。
// 签名只覆盖 payload 字符串，因此任何对 payload 或签名片段的改动都会导致校验失败。
func (m *Manager) sign(c tokenClaims) (string, error) {
	body, err := json.Marshal(c)
	if err != nil {
		return "", fmt.Errorf("session: marshal claims: %w", err)
	}
	payload := base64.RawURLEncoding.EncodeToString(body)
	signature := base64.RawURLEncoding.EncodeToString(m.signature(payload))
	return payload + "." + signature, nil
}

// signature 计算 payload 的 HMAC-SHA256，密钥来自 Manager.secret。
func (m *Manager) signature(payload string) []byte {
	mac := hmac.New(sha256.New, m.secret)
	_, _ = mac.Write([]byte(payload))
	return mac.Sum(nil)
}

// verify 校验 token 的格式、签名和过期时间，返回其中的身份信息。
// 使用 hmac.Equal 而不是 ==，避免通过比较耗时差异推断正确签名。
func (m *Manager) verify(token string) (*tokenClaims, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 2 {
		return nil, errors.New("session: malformed token")
	}

	sig, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, fmt.Errorf("session: malformed token signature: %w", err)
	}
	if !hmac.Equal(sig, m.signature(parts[0])) {
		return nil, errors.New("session: invalid signature")
	}

	body, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return nil, fmt.Errorf("session: malformed token payload: %w", err)
	}
	var c tokenClaims
	if err := json.Unmarshal(body, &c); err != nil {
		return nil, fmt.Errorf("session: malformed token payload: %w", err)
	}
	if c.PlayerID == "" {
		return nil, errors.New("session: malformed token payload: empty player id")
	}
	if !m.now().Before(time.Unix(c.ExpiresAt, 0)) {
		return nil, errors.New("session: token expired")
	}
	return &c, nil
}
