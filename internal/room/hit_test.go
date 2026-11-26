package room

import (
	"testing"
	"time"

	"game-backend-practice/internal/protocol"
)

func TestRoomApplyInputFireHitsNearbyPlayer(t *testing.T) {
	r, err := NewRoom("room-1", []string{"p1", "p2"})
	if err != nil {
		t.Fatalf("NewRoom() error = %v", err)
	}
	base := time.Date(2026, 11, 26, 10, 0, 0, 0, time.UTC)
	r.now = func() time.Time { return base }
	r.Players["p2"].X = 1 // 目标在射程内

	if err := r.ApplyInput("p1", protocol.InputCmd{Seq: 1, Fire: true}); err != nil {
		t.Fatalf("ApplyInput(fire) error = %v", err)
	}
	p1 := r.Players["p1"]
	p2 := r.Players["p2"]
	if p2.HP != 75 {
		t.Fatalf("target HP = %d, want 75", p2.HP)
	}
	if p1.HP != 100 {
		t.Fatalf("shooter HP = %d, want 100", p1.HP)
	}
}

func TestRoomApplyInputFireRespectsRangeBoundary(t *testing.T) {
	r, err := NewRoom("room-1", []string{"p1", "p2"})
	if err != nil {
		t.Fatalf("NewRoom() error = %v", err)
	}
	base := time.Date(2026, 11, 26, 10, 0, 0, 0, time.UTC)
	r.now = func() time.Time { return base }
	p2 := r.Players["p2"]

	p2.X = 2 // 恰好在射程边界，应该命中
	if err := r.ApplyInput("p1", protocol.InputCmd{Seq: 1, Fire: true}); err != nil {
		t.Fatalf("ApplyInput(boundary fire) error = %v", err)
	}
	if p2.HP != 75 {
		t.Fatalf("target HP at boundary = %d, want 75", p2.HP)
	}

	// 冷却结束后目标超出射程，不应继续扣血。
	r.now = func() time.Time { return base.Add(200 * time.Millisecond) }
	p2.X = 2.1
	if err := r.ApplyInput("p1", protocol.InputCmd{Seq: 2, Fire: true}); err != nil {
		t.Fatalf("ApplyInput(out-of-range fire) error = %v", err)
	}
	if p2.HP != 75 {
		t.Fatalf("target HP out of range = %d, want unchanged 75", p2.HP)
	}
}

func TestRoomApplyInputFireIgnoresDeadTarget(t *testing.T) {
	r, err := NewRoom("room-1", []string{"p1", "p2"})
	if err != nil {
		t.Fatalf("NewRoom() error = %v", err)
	}
	base := time.Date(2026, 11, 26, 10, 0, 0, 0, time.UTC)
	r.now = func() time.Time { return base }
	p2 := r.Players["p2"]
	p2.X = 0.5
	p2.HP = 0

	if err := r.ApplyInput("p1", protocol.InputCmd{Seq: 1, Fire: true}); err != nil {
		t.Fatalf("ApplyInput(fire at dead) error = %v", err)
	}
	if p2.HP != 0 {
		t.Fatalf("dead target HP = %d, want 0", p2.HP)
	}
}
