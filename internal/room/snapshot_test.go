package room

import (
	"context"
	"testing"
	"time"

	"game-backend-practice/internal/protocol"
)

func TestRoomSnapshot(t *testing.T) {
	r, err := NewRoom("room-1", []string{"p2", "p1"})
	if err != nil {
		t.Fatalf("NewRoom() error = %v", err)
	}
	p1 := r.Players["p1"]
	p1.X = 1
	p1.Y = -2
	p1.HP = 75
	p1.LastSeq = 3
	r.Advance()
	r.Advance()

	s := r.Snapshot()
	if s.RoomID != "room-1" || s.Tick != 2 {
		t.Fatalf("snapshot meta = %+v, want room-1 tick=2", s)
	}
	if len(s.Players) != 2 {
		t.Fatalf("len(Players) = %d, want 2", len(s.Players))
	}
	if s.Players[0].ID != "p1" || s.Players[1].ID != "p2" {
		t.Fatalf("players order = %v, want sorted [p1 p2]", s.Players)
	}
	first := s.Players[0]
	if first.X != 1 || first.Y != -2 || first.HP != 75 || first.LastSeq != 3 {
		t.Fatalf("p1 snapshot = %+v, want X=1 Y=-2 HP=75 LastSeq=3", first)
	}
}

func TestRoomRunEmitsSnapshot(t *testing.T) {
	r, err := NewRoom("room-1", []string{"p1"})
	if err != nil {
		t.Fatalf("NewRoom() error = %v", err)
	}
	snapshots := make(chan *protocol.Snapshot, 4)
	r.SetSnapshotSink(func(s *protocol.Snapshot) {
		select {
		case snapshots <- s:
		default:
		}
	})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- r.Run(ctx, 5*time.Millisecond) }()

	var got *protocol.Snapshot
	select {
	case got = <-snapshots:
	case <-time.After(time.Second):
		t.Fatal("no snapshot emitted within 1s")
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run() error = %v, want nil", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Run did not stop after context cancel")
	}
	if got.RoomID != "room-1" || got.Tick < 1 || len(got.Players) != 1 {
		t.Fatalf("snapshot = %+v, want room-1 tick>=1 with 1 player", got)
	}
}
