package room

import (
	"testing"

	"game-backend-practice/internal/protocol"
)

func TestHandlerHandleInputCmd(t *testing.T) {
	rooms := NewManager()
	if _, err := rooms.Create("room-1", []string{"p1", "p2"}); err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	h := NewHandler(rooms)
	// 消息体身份填错，Handler 必须用连接身份 p1。
	cmd := &protocol.InputCmd{PlayerID: "fake", Seq: 1, MoveX: 0.5}
	if err := h.handle("p1", cmd); err != nil {
		t.Fatalf("handle() error = %v", err)
	}
	rooms.mu.RLock()
	p1 := rooms.rooms["room-1"].Players["p1"]
	p2 := rooms.rooms["room-1"].Players["p2"]
	rooms.mu.RUnlock()
	if p1.X != 0.5 || p1.LastSeq != 1 {
		t.Fatalf("p1 = %+v, want X=0.5 LastSeq=1", p1)
	}
	if p2.X != 0 {
		t.Fatalf("p2 = %+v, want untouched", p2)
	}
}

func TestHandlerHandleRejectsUnknownPlayer(t *testing.T) {
	rooms := NewManager()
	if _, err := rooms.Create("room-1", []string{"p1"}); err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	h := NewHandler(rooms)
	if err := h.handle("missing", &protocol.InputCmd{Seq: 1}); err == nil {
		t.Fatal("handle(missing) error = nil, want non-nil")
	}
}

func TestHandlerHandleIgnoresNonInputMessages(t *testing.T) {
	rooms := NewManager()
	h := NewHandler(rooms)
	if err := h.handle("p1", nil); err == nil {
		t.Fatal("handle(nil) error = nil, want non-nil")
	}
	if err := h.handle("p1", &protocol.Snapshot{}); err != nil {
		t.Fatalf("handle(Snapshot) error = %v, want nil", err)
	}
}
