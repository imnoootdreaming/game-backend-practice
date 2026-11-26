package room

import (
	"testing"
	"time"

	"game-backend-practice/internal/protocol"
)

func TestRoomApplyInputFireCooldown(t *testing.T) {
	r, err := NewRoom("room-1", []string{"p1"})
	if err != nil {
		t.Fatalf("NewRoom() error = %v", err)
	}
	base := time.Date(2026, 11, 26, 10, 0, 0, 0, time.UTC)
	r.now = func() time.Time { return base }
	p := r.Players["p1"]

	first := protocol.InputCmd{Seq: 1, MoveX: 0.2, Fire: true}
	if err := r.ApplyInput("p1", first); err != nil {
		t.Fatalf("first fire error = %v", err)
	}
	if p.X != 0.2 || p.LastFireAt != base || p.LastSeq != 1 {
		t.Fatalf("p after first fire = %+v, want X=0.2 fireAt=base seq=1", p)
	}

	// 冷却内同一玩家再次开火必须被拒绝，且位置/序号也不推进。
	r.now = func() time.Time { return base.Add(100 * time.Millisecond) }
	tooFast := protocol.InputCmd{Seq: 2, MoveX: 0.3, Fire: true}
	if err := r.ApplyInput("p1", tooFast); err == nil {
		t.Fatal("fire inside cooldown error = nil, want non-nil")
	}
	if p.X != 0.2 || p.LastFireAt != base || p.LastSeq != 1 {
		t.Fatalf("p after rejected fire = %+v, want unchanged", p)
	}

	// 恰好到冷却边界时允许再次开火。
	r.now = func() time.Time { return base.Add(200 * time.Millisecond) }
	atBoundary := protocol.InputCmd{Seq: 3, Fire: true}
	if err := r.ApplyInput("p1", atBoundary); err != nil {
		t.Fatalf("fire at boundary error = %v", err)
	}
	if p.LastFireAt != base.Add(200*time.Millisecond) || p.LastSeq != 3 {
		t.Fatalf("p after boundary fire = %+v, want fireAt=base+200ms seq=3", p)
	}
}
