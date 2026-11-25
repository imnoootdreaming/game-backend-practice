package ws

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"game-backend-practice/internal/protocol"
	"game-backend-practice/internal/session"
)

// fakeTicker 是可注入的假 ticker：测试手动往 ch 里送一次"时间到了"
type fakeTicker struct{ ch chan time.Time }

func (f *fakeTicker) C() <-chan time.Time { return f.ch }
func (f *fakeTicker) Stop()               {}

func newTestServer(t *testing.T, router *Router, opts ...ServerOption) (*Server, *session.Manager) {
	t.Helper()
	mgr, err := session.NewManager([]byte("ws-test-secret"), time.Hour)
	if err != nil {
		t.Fatalf("session.NewManager() error = %v", err)
	}
	return NewServer(mgr, router, opts...), mgr
}

// wsURL 把 httptest 给的 http:// 地址换成 gorilla 拨号需要的 ws://
func wsURL(base string) string { return "ws" + strings.TrimPrefix(base, "http") }

// loginURL 给指定 playerID 登录并拼出带 token 的 WebSocket 地址
func loginURL(t *testing.T, mgr *session.Manager, base, playerID string) string {
	t.Helper()
	sess, err := mgr.Login(playerID)
	if err != nil {
		t.Fatalf("Login(%s) error = %v", playerID, err)
	}
	return wsURL(base) + "/?token=" + url.QueryEscape(sess.Token) + "&player_id=" + url.QueryEscape(playerID)
}

func dialWS(t *testing.T, rawURL string) *websocket.Conn {
	t.Helper()
	c, resp, err := websocket.DefaultDialer.Dial(rawURL, nil)
	if err != nil {
		if resp != nil {
			t.Fatalf("Dial(%s) error = %v, status = %d", rawURL, err, resp.StatusCode)
		}
		t.Fatalf("Dial(%s) error = %v", rawURL, err)
	}
	return c
}

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("condition not met within 2s")
}

// TestServerAcceptsMultipleConnectionsAndRoutes 正常路径：两个连接同时在线，消息按类型分别进 lobby / room，断开后自动注销
func TestServerAcceptsMultipleConnectionsAndRoutes(t *testing.T) {
	lobby := &recordingHandler{}
	room := &recordingHandler{}
	srv, mgr := newTestServer(t, NewRouter(lobby, room))
	ts := httptest.NewServer(srv)
	defer ts.Close()

	c1 := dialWS(t, loginURL(t, mgr, ts.URL, "p1"))
	defer c1.Close()
	c2 := dialWS(t, loginURL(t, mgr, ts.URL, "p2"))
	defer c2.Close()

	waitFor(t, func() bool { return srv.Registry().Len() == 2 })

	frame1, err := protocol.Encode(&protocol.ReadyReq{PlayerID: "p1", Ready: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := c1.WriteMessage(websocket.TextMessage, frame1); err != nil {
		t.Fatalf("write p1: %v", err)
	}
	frame2, err := protocol.Encode(&protocol.InputCmd{PlayerID: "p2", Seq: 1, Fire: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := c2.WriteMessage(websocket.BinaryMessage, frame2); err != nil {
		t.Fatalf("write p2: %v", err)
	}

	waitFor(t, func() bool { return len(lobby.types()) == 1 && len(room.types()) == 1 })
	if got := lobby.types()[0]; got != "ReadyReq" {
		t.Errorf("lobby got %q, want ReadyReq", got)
	}
	if got := room.types()[0]; got != "InputCmd" {
		t.Errorf("room got %q, want InputCmd", got)
	}

	_ = c1.Close()
	waitFor(t, func() bool { return srv.Registry().Len() == 1 })
	if _, ok := srv.Registry().Get("p1"); ok {
		t.Error("p1 still registered after disconnect")
	}
}

// TestServerRejectsUnauthenticated 错误路径：没有 token 或 token 非法时拒绝升级为 WebSocket
func TestServerRejectsUnauthenticated(t *testing.T) {
	srv, _ := newTestServer(t, NewRouter(nil, nil))
	ts := httptest.NewServer(srv)
	defer ts.Close()

	tests := []struct {
		name  string
		query string
	}{
		{"missing token", "player_id=p1"},
		{"bad token", "token=bad&player_id=p1"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, resp, err := websocket.DefaultDialer.Dial(wsURL(ts.URL)+"/?"+tt.query, nil)
			if err == nil {
				t.Fatal("Dial() error = nil, want rejection")
			}
			if resp == nil || resp.StatusCode != http.StatusUnauthorized {
				t.Fatalf("status = %v, want 401", resp)
			}
		})
	}
}

// TestServerRejectsDuplicatePlayer 错误路径：同一 playerID 重复连接时拒绝，防止两个连接互相覆盖状态
func TestServerRejectsDuplicatePlayer(t *testing.T) {
	srv, mgr := newTestServer(t, NewRouter(nil, nil))
	ts := httptest.NewServer(srv)
	defer ts.Close()

	c1 := dialWS(t, loginURL(t, mgr, ts.URL, "p1"))
	defer c1.Close()
	waitFor(t, func() bool { return srv.Registry().Len() == 1 })

	_, resp, err := websocket.DefaultDialer.Dial(loginURL(t, mgr, ts.URL, "p1"), nil)
	if err == nil {
		t.Fatal("second Dial() error = nil, want rejection")
	}
	if resp == nil || resp.StatusCode != http.StatusConflict {
		t.Fatalf("status = %v, want 409", resp)
	}
}

// TestHeartbeatTimeoutClosesConnection 错误路径：假时钟越过超时点后，下一次 tick 应踢掉连接并注销
func TestHeartbeatTimeoutClosesConnection(t *testing.T) {
	fc := &fakeClock{t: time.Unix(0, 0)}
	ft := &fakeTicker{ch: make(chan time.Time)}
	srv, mgr := newTestServer(t, NewRouter(nil, nil),
		WithClock(fc.Now),
		WithTickerFactory(func(time.Duration) Ticker { return ft }),
		WithPongTimeout(time.Second),
	)
	ts := httptest.NewServer(srv)
	defer ts.Close()

	c := dialWS(t, loginURL(t, mgr, ts.URL, "p1"))
	defer c.Close()
	waitFor(t, func() bool { return srv.Registry().Len() == 1 })

	fc.Advance(2 * time.Second)
	select {
	case ft.ch <- fc.Now():
	case <-time.After(time.Second):
		t.Fatal("write pump did not consume the fake tick")
	}

	_ = c.SetReadDeadline(time.Now().Add(time.Second))
	if _, _, err := c.ReadMessage(); err == nil {
		t.Fatal("connection still readable after heartbeat timeout")
	}
	waitFor(t, func() bool { return srv.Registry().Len() == 0 })
}
