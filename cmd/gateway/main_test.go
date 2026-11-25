package main

import (
	"net/http"
	"net/http/httptest" // 测试专用：在内存里模拟 HTTP 请求和响应，不用真开端口
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"game-backend-practice/internal/session"
	"game-backend-practice/internal/ws"
)

// wsURL 把 httptest 给的 http:// 地址换成 gorilla 拨号需要的 ws://
func wsURL(base string) string { return "ws" + strings.TrimPrefix(base, "http") }

// newTestWSServer 给 gateway 测试构造一个带会话管理器的 WebSocket 服务
func newTestWSServer(t *testing.T) (*ws.Server, *session.Manager) {
	t.Helper()
	mgr, err := session.NewManager([]byte("gateway-test-secret"), time.Hour)
	if err != nil {
		t.Fatalf("session.NewManager() error = %v", err)
	}
	return ws.NewServer(mgr, ws.NewRouter(nil, nil)), mgr
}

// TestHealthzOK 验证：GET /healthz 返回 200、JSON 头、{"status":"ok"} 响应体
func TestHealthzOK(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	w := httptest.NewRecorder() // 记录处理器写出的状态码、响应头和响应体

	srv, _ := newTestWSServer(t)
	newHandler(srv).ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	if got := w.Header().Get("Content-Type"); got != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", got)
	}
	if got := w.Body.String(); got != `{"status":"ok"}` {
		t.Errorf("body = %q, want %q", got, `{"status":"ok"}`)
	}
}

// TestHealthzRejectsPost 验证：POST /healthz 被路由层拒绝（405），呼应路由写法 "GET /healthz"
func TestHealthzRejectsPost(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/healthz", nil)
	w := httptest.NewRecorder()

	srv, _ := newTestWSServer(t)
	newHandler(srv).ServeHTTP(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", w.Code)
	}
}

// TestWebSocketEndpointRegistered 验证：gateway 的 /ws 路由存在，并复用 ws.Server 的鉴权与注册逻辑
func TestWebSocketEndpointRegistered(t *testing.T) {
	srv, mgr := newTestWSServer(t)
	ts := httptest.NewServer(newHandler(srv))
	defer ts.Close()

	t.Run("rejects missing token", func(t *testing.T) {
		_, resp, err := websocket.DefaultDialer.Dial(wsURL(ts.URL)+"/ws?player_id=p1", nil)
		if err == nil {
			t.Fatal("Dial() error = nil, want rejection")
		}
		if resp == nil || resp.StatusCode != http.StatusUnauthorized {
			t.Fatalf("status = %v, want 401", resp)
		}
	})

	t.Run("accepts valid token", func(t *testing.T) {
		sess, err := mgr.Login("p1")
		if err != nil {
			t.Fatal(err)
		}
		rawURL := wsURL(ts.URL) + "/ws?token=" + url.QueryEscape(sess.Token) + "&player_id=p1"
		c, _, err := websocket.DefaultDialer.Dial(rawURL, nil)
		if err != nil {
			t.Fatalf("Dial() error = %v", err)
		}
		defer c.Close()

		deadline := time.Now().Add(time.Second)
		for time.Now().Before(deadline) {
			if srv.Registry().Len() == 1 {
				return
			}
			time.Sleep(10 * time.Millisecond)
		}
		t.Fatal("connection not registered in ws.Server")
	})
}
