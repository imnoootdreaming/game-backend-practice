package main

import (
	"context"
	"net/http"
	"net/http/httptest" // 测试专用：在内存里模拟 HTTP 请求和响应，不用真开端口
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"game-backend-practice/internal/config"
	"game-backend-practice/internal/lobby"
	"game-backend-practice/internal/match"
	"game-backend-practice/internal/protocol"
	"game-backend-practice/internal/room"
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

func TestNewMatcherUsesGatewayConfig(t *testing.T) {
	cfg := &config.Config{
		Game: config.GameConfig{TeamSize: 2},
		Match: config.MatchConfig{
			BaseRankTolerance: 10,
			RankWidenStep:     5,
			RankWidenInterval: 10 * time.Second,
		},
	}
	q := match.NewQueue()
	base := time.Date(2026, 11, 26, 10, 0, 0, 0, time.UTC)
	for _, e := range []match.Entry{
		{PlayerID: "p1", PartyID: "party-p1", PartySize: 1, Rank: 1000, EnqueuedAt: base},
		{PlayerID: "p2", PartyID: "party-p2", PartySize: 1, Rank: 1015, EnqueuedAt: base},
	} {
		if _, err := q.Enqueue(e); err != nil {
			t.Fatalf("Enqueue(%s) error = %v", e.PlayerID, err)
		}
	}

	m, err := newMatcher(cfg, q, func(match.Result) error { return nil })
	if err != nil {
		t.Fatalf("newMatcher() error = %v", err)
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
	for i, id := range wantIDs {
		if after[0].PlayerIDs[i] != id {
			t.Fatalf("matched player[%d] = %q, want %q", i, after[0].PlayerIDs[i], id)
		}
	}
}

// fakeSendPeer 是测试用的在线连接，记录收到的业务帧，便于断言消息内容。
type fakeSendPeer struct {
	mu     sync.Mutex // 保护 frames 与 kinds 的并发读写
	id     string     // 玩家 ID
	frames [][]byte   // 收到的业务帧
	kinds  []int      // 每条帧对应的 WebSocket 消息类型
}

func (p *fakeSendPeer) ID() string   { return p.id }
func (p *fakeSendPeer) Close() error { return nil }
func (p *fakeSendPeer) Send(kind int, data []byte) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.kinds = append(p.kinds, kind)
	p.frames = append(p.frames, append([]byte(nil), data...))
	return nil
}

// Frames 返回已收到业务帧的副本，供测试安全读取。
func (p *fakeSendPeer) Frames() [][]byte {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([][]byte(nil), p.frames...)
}

// Kinds 返回已收到帧类型的副本，供测试安全读取。
func (p *fakeSendPeer) Kinds() []int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]int(nil), p.kinds...)
}

func TestOnMatchedAssignsRoomAndSendsMessages(t *testing.T) {
	m, err := lobby.NewManager(4)
	if err != nil {
		t.Fatalf("lobby.NewManager() error = %v", err)
	}
	registry := ws.NewRegistry()
	rooms := room.NewManager()
	peers := make(map[string]*fakeSendPeer)
	for _, id := range []string{"p1", "p2"} {
		if err := m.Login(id); err != nil {
			t.Fatalf("Login(%s): %v", id, err)
		}
		if err := m.EnterMatching(id); err != nil {
			t.Fatalf("EnterMatching(%s): %v", id, err)
		}
		peer := &fakeSendPeer{id: id}
		if err := registry.Add(peer); err != nil {
			t.Fatalf("registry.Add(%s): %v", id, err)
		}
		peers[id] = peer
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	result := match.Result{MatchID: "match-1", PlayerIDs: []string{"p1", "p2"}}
	if err := onMatched(ctx, m, registry, rooms, 20)(result); err != nil {
		t.Fatalf("onMatched() error = %v", err)
	}
	created, ok := rooms.Get("room-match-1")
	if !ok {
		t.Fatal("room room-match-1 not created")
	}
	if len(created.Players) != 2 {
		t.Fatalf("len(room.Players) = %d, want 2", len(created.Players))
	}
	for _, id := range []string{"p1", "p2"} {
		if got := m.StateOf(id); got != lobby.InRoom {
			t.Fatalf("state[%s] = %s, want InRoom", id, got)
		}
		peer := peers[id]
		frames := peer.Frames()
		kinds := peer.Kinds()
		if len(frames) < 2 {
			t.Fatalf("frames[%s] = %d, want at least 2", id, len(frames))
		}
		if kinds[0] != websocket.BinaryMessage || kinds[1] != websocket.BinaryMessage {
			t.Fatalf("kinds[%s] = %v, want binary frames", id, kinds)
		}
		startMsg, err := protocol.Decode(frames[0])
		if err != nil {
			t.Fatalf("decode MatchStart[%s]: %v", id, err)
		}
		start, ok := startMsg.(*protocol.MatchStart)
		if !ok || start.MatchID != "match-1" || len(start.PlayerIDs) != 2 {
			t.Fatalf("MatchStart[%s] = %+v, want match-1 with 2 players", id, startMsg)
		}
		assignMsg, err := protocol.Decode(frames[1])
		if err != nil {
			t.Fatalf("decode RoomAssign[%s]: %v", id, err)
		}
		assign, ok := assignMsg.(*protocol.RoomAssign)
		if !ok || assign.RoomID != "room-match-1" || assign.TickHz != 20 {
			t.Fatalf("RoomAssign[%s] = %+v, want room-match-1 tick 20", id, assignMsg)
		}
	}
}

func TestOnMatchedRejectsInvalidState(t *testing.T) {
	m, err := lobby.NewManager(4)
	if err != nil {
		t.Fatalf("lobby.NewManager() error = %v", err)
	}
	if err := m.Login("p1"); err != nil {
		t.Fatalf("Login(p1): %v", err)
	}
	registry := ws.NewRegistry()
	if err := registry.Add(&fakeSendPeer{id: "p1"}); err != nil {
		t.Fatalf("registry.Add(p1): %v", err)
	}
	result := match.Result{MatchID: "match-1", PlayerIDs: []string{"p1"}}
	if err := onMatched(context.Background(), m, registry, room.NewManager(), 20)(result); err == nil {
		t.Fatal("onMatched() error = nil, want invalid transition error")
	}
}

func TestOnMatchedRejectsDuplicateRoom(t *testing.T) {
	m, err := lobby.NewManager(4)
	if err != nil {
		t.Fatalf("lobby.NewManager() error = %v", err)
	}
	if err := m.Login("p1"); err != nil {
		t.Fatalf("Login(p1): %v", err)
	}
	if err := m.EnterMatching("p1"); err != nil {
		t.Fatalf("EnterMatching(p1): %v", err)
	}
	rooms := room.NewManager()
	if _, err := rooms.Create("room-match-1", []string{"other"}); err != nil {
		t.Fatalf("pre-create room: %v", err)
	}
	result := match.Result{MatchID: "match-1", PlayerIDs: []string{"p1"}}
	if err := onMatched(context.Background(), m, ws.NewRegistry(), rooms, 20)(result); err == nil {
		t.Fatal("onMatched() error = nil, want duplicate room error")
	}
}

func TestOnMatchedStartsRoomTicker(t *testing.T) {
	m, err := lobby.NewManager(4)
	if err != nil {
		t.Fatalf("lobby.NewManager() error = %v", err)
	}
	if err := m.Login("p1"); err != nil {
		t.Fatalf("Login(p1): %v", err)
	}
	if err := m.EnterMatching("p1"); err != nil {
		t.Fatalf("EnterMatching(p1): %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	rooms := room.NewManager()
	result := match.Result{MatchID: "match-1", PlayerIDs: []string{"p1"}}
	if err := onMatched(ctx, m, ws.NewRegistry(), rooms, 1000)(result); err != nil {
		t.Fatalf("onMatched() error = %v", err)
	}
	created, ok := rooms.Get("room-match-1")
	if !ok {
		t.Fatal("room room-match-1 not created")
	}
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if created.CurrentTick() >= 2 {
			break
		}
		time.Sleep(time.Millisecond)
	}
	if got := created.CurrentTick(); got < 2 {
		t.Fatalf("room tick = %d, want at least 2", got)
	}
}

func TestOnMatchedRejectsNonPositiveTickHz(t *testing.T) {
	m, err := lobby.NewManager(4)
	if err != nil {
		t.Fatalf("lobby.NewManager() error = %v", err)
	}
	if err := m.Login("p1"); err != nil {
		t.Fatalf("Login(p1): %v", err)
	}
	if err := m.EnterMatching("p1"); err != nil {
		t.Fatalf("EnterMatching(p1): %v", err)
	}
	rooms := room.NewManager()
	result := match.Result{MatchID: "match-1", PlayerIDs: []string{"p1"}}
	if err := onMatched(context.Background(), m, ws.NewRegistry(), rooms, 0)(result); err == nil {
		t.Fatal("onMatched() error = nil, want invalid tickHz error")
	}
	if _, ok := rooms.Get("room-match-1"); ok {
		t.Fatal("room created despite invalid tickHz")
	}
}

func TestOnMatchedBroadcastsSnapshot(t *testing.T) {
	m, err := lobby.NewManager(4)
	if err != nil {
		t.Fatalf("lobby.NewManager() error = %v", err)
	}
	registry := ws.NewRegistry()
	peers := make(map[string]*fakeSendPeer)
	for _, id := range []string{"p1", "p2"} {
		if err := m.Login(id); err != nil {
			t.Fatalf("Login(%s): %v", id, err)
		}
		if err := m.EnterMatching(id); err != nil {
			t.Fatalf("EnterMatching(%s): %v", id, err)
		}
		peer := &fakeSendPeer{id: id}
		if err := registry.Add(peer); err != nil {
			t.Fatalf("registry.Add(%s): %v", id, err)
		}
		peers[id] = peer
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	rooms := room.NewManager()
	result := match.Result{MatchID: "match-1", PlayerIDs: []string{"p1", "p2"}}
	if err := onMatched(ctx, m, registry, rooms, 1000)(result); err != nil {
		t.Fatalf("onMatched() error = %v", err)
	}

	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if len(peers["p1"].Frames()) >= 3 && len(peers["p2"].Frames()) >= 3 {
			break
		}
		time.Sleep(time.Millisecond)
	}
	for _, id := range []string{"p1", "p2"} {
		frames := peers[id].Frames()
		if len(frames) < 3 {
			t.Fatalf("frames[%s] = %d, want at least 3", id, len(frames))
		}
		msg, err := protocol.Decode(frames[2])
		if err != nil {
			t.Fatalf("decode Snapshot[%s]: %v", id, err)
		}
		snap, ok := msg.(*protocol.Snapshot)
		if !ok || snap.RoomID != "room-match-1" || snap.Tick < 1 || len(snap.Players) != 2 {
			t.Fatalf("Snapshot[%s] = %+v, want room-match-1 tick>=1 with 2 players", id, msg)
		}
	}
}
