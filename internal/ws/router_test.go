package ws

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"testing"

	"game-backend-practice/internal/protocol"
)

// recordingHandler 记录被路由过来的消息类型，用来断言"分发到了正确的处理器"
// 多态测试桩：它和 *lobby.Handler 没有任何继承关系，只因实现了 Handle 就能当 Handler 用。
type recordingHandler struct {
	mu   sync.Mutex
	got  []string
	fail error
}

func (h *recordingHandler) Handle(_ context.Context, _ *Conn, msg protocol.Message) error {
	h.mu.Lock()
	h.got = append(h.got, msg.Type())
	h.mu.Unlock()
	return h.fail
}

func (h *recordingHandler) types() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]string(nil), h.got...)
}

// unknownMessage 是测试专用的消息类型，用来验证未归类消息会走 fallback
type unknownMessage struct{}

func (unknownMessage) Type() string { return "Unknown" }

// TestRouterDispatch 正常路径：大厅类消息给 lobby，战斗类消息给 room，其余给 fallback
func TestRouterDispatch(t *testing.T) {
	// 三个测试桩以 Handler 接口类型传入 Router：同一个接口，不同实例，
	// 路由后各自记录属于自己的消息，用来验证接口调用分派到了正确的实现。
	lobby := &recordingHandler{}
	room := &recordingHandler{}
	fallback := &recordingHandler{}
	r := NewRouterWithFallback(lobby, room, fallback)

	msgs := []protocol.Message{
		&protocol.LoginReq{},
		&protocol.LoginResp{},
		&protocol.ReadyReq{},
		&protocol.TeamInvite{},
		&protocol.MatchStart{},
		&protocol.RoomAssign{},
		&protocol.InputCmd{},
		&protocol.Snapshot{},
		&protocol.GameResult{},
		unknownMessage{},
	}
	for _, msg := range msgs {
		if err := r.Route(context.Background(), nil, msg); err != nil {
			t.Fatalf("Route(%s) error = %v", msg.Type(), err)
		}
	}

	wantLobby := []string{"LoginReq", "LoginResp", "ReadyReq", "TeamInvite", "MatchStart", "RoomAssign"}
	if got := lobby.types(); !reflect.DeepEqual(got, wantLobby) {
		t.Errorf("lobby got %v, want %v", got, wantLobby)
	}
	wantRoom := []string{"InputCmd", "Snapshot", "GameResult"}
	if got := room.types(); !reflect.DeepEqual(got, wantRoom) {
		t.Errorf("room got %v, want %v", got, wantRoom)
	}
	if got := fallback.types(); !reflect.DeepEqual(got, []string{"Unknown"}) {
		t.Errorf("fallback got %v, want [Unknown]", got)
	}
}

// TestRouterReturnsHandlerError 错误路径：处理器返回的错误要原样传回读泵，不能吞掉
func TestRouterReturnsHandlerError(t *testing.T) {
	wantErr := errors.New("boom")
	r := NewRouterWithFallback(&recordingHandler{fail: wantErr}, &recordingHandler{}, &recordingHandler{})

	if err := r.Route(context.Background(), nil, &protocol.ReadyReq{}); !errors.Is(err, wantErr) {
		t.Errorf("Route() error = %v, want %v", err, wantErr)
	}
}
