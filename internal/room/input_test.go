package room

import (
	"testing"

	"game-backend-practice/internal/protocol"
)

func TestRoomApplyInputMovesAuthoritativePosition(t *testing.T) {
	r, err := NewRoom("room-1", []string{"p1", "p2"})
	if err != nil {
		t.Fatalf("NewRoom() error = %v", err)
	}
	// PlayerID 故意填错：服务端必须以连接身份 playerID 为准。
	cmd := protocol.InputCmd{PlayerID: "fake", Seq: 1, MoveX: 0.5, MoveY: -0.25}
	if err := r.ApplyInput("p1", cmd); err != nil {
		t.Fatalf("ApplyInput() error = %v", err)
	}
	p1 := r.Players["p1"]
	if p1.X != 0.5 || p1.Y != -0.25 || p1.LastSeq != 1 {
		t.Fatalf("p1 = %+v, want X=0.5 Y=-0.25 LastSeq=1", p1)
	}
	p2 := r.Players["p2"]
	if p2.X != 0 || p2.Y != 0 || p2.LastSeq != 0 {
		t.Fatalf("p2 = %+v, want untouched", p2)
	}
}

func TestRoomApplyInputRejectsOverspeed(t *testing.T) {
	r, err := NewRoom("room-1", []string{"p1"})
	if err != nil {
		t.Fatalf("NewRoom() error = %v", err)
	}
	overspeed := protocol.InputCmd{Seq: 1, MoveX: 2}
	if err := r.ApplyInput("p1", overspeed); err == nil {
		t.Fatal("ApplyInput(overspeed) error = nil, want non-nil")
	}
	p := r.Players["p1"]
	if p.X != 0 || p.LastSeq != 0 {
		t.Fatalf("p after overspeed = %+v, want untouched", p)
	}
	boundary := protocol.InputCmd{Seq: 2, MoveX: 1}
	if err := r.ApplyInput("p1", boundary); err != nil {
		t.Fatalf("ApplyInput(boundary) error = %v", err)
	}
	if p.X != 1 || p.LastSeq != 2 {
		t.Fatalf("p after boundary = %+v, want X=1 LastSeq=2", p)
	}
}

func TestRoomApplyInputIgnoresOldAndDuplicateSeq(t *testing.T) {
	r, err := NewRoom("room-1", []string{"p1"})
	if err != nil {
		t.Fatalf("NewRoom() error = %v", err)
	}
	if err := r.ApplyInput("p1", protocol.InputCmd{Seq: 2, MoveX: 0.3}); err != nil {
		t.Fatalf("ApplyInput(seq=2) error = %v", err)
	}
	if err := r.ApplyInput("p1", protocol.InputCmd{Seq: 2, MoveX: 0.4}); err != nil {
		t.Fatalf("ApplyInput(duplicate seq) error = %v", err)
	}
	if err := r.ApplyInput("p1", protocol.InputCmd{Seq: 1, MoveX: 0.5}); err != nil {
		t.Fatalf("ApplyInput(old seq) error = %v", err)
	}
	if err := r.ApplyInput("missing", protocol.InputCmd{Seq: 3, MoveX: 0.1}); err == nil {
		t.Fatal("ApplyInput(missing player) error = nil, want non-nil")
	}
	p := r.Players["p1"]
	if p.X != 0.3 || p.LastSeq != 2 {
		t.Fatalf("p = %+v, want X=0.3 LastSeq=2", p)
	}
}
