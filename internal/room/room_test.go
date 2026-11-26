package room

import "testing"

func TestNewRoomInitializesPlayers(t *testing.T) {
	r, err := NewRoom("room-1", []string{"p1", "p2"})
	if err != nil {
		t.Fatalf("NewRoom() error = %v", err)
	}
	if r.ID != "room-1" {
		t.Fatalf("ID = %q, want room-1", r.ID)
	}
	if r.Tick != 0 {
		t.Fatalf("Tick = %d, want 0", r.Tick)
	}
	if len(r.Players) != 2 {
		t.Fatalf("len(Players) = %d, want 2", len(r.Players))
	}
	for _, id := range []string{"p1", "p2"} {
		p, ok := r.Players[id]
		if !ok {
			t.Fatalf("player %s missing", id)
		}
		if p.ID != id || p.X != 0 || p.Y != 0 || p.HP != 100 || p.LastSeq != 0 {
			t.Fatalf("player %s = %+v, want zero state with HP=100", id, p)
		}
	}
}

func TestNewRoomRejectsInvalidInput(t *testing.T) {
	cases := []struct {
		name      string   // 用例名
		roomID    string   // 房间 ID
		playerIDs []string // 玩家列表
	}{
		{"empty room id", "", []string{"p1"}},
		{"no players", "room-1", nil},
		{"empty player id", "room-1", []string{"p1", ""}},
		{"duplicate player", "room-1", []string{"p1", "p1"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := NewRoom(tc.roomID, tc.playerIDs); err == nil {
				t.Fatal("NewRoom() error = nil, want non-nil")
			}
		})
	}
}

func TestRoomAddAndRemovePlayer(t *testing.T) {
	r, err := NewRoom("room-1", []string{"p1"})
	if err != nil {
		t.Fatalf("NewRoom() error = %v", err)
	}
	if err := r.AddPlayer("p2"); err != nil {
		t.Fatalf("AddPlayer(p2) error = %v", err)
	}
	if err := r.AddPlayer("p2"); err == nil {
		t.Fatal("AddPlayer(duplicate) error = nil, want non-nil")
	}
	if err := r.AddPlayer(""); err == nil {
		t.Fatal("AddPlayer(empty) error = nil, want non-nil")
	}
	if err := r.RemovePlayer("p1"); err != nil {
		t.Fatalf("RemovePlayer(p1) error = %v", err)
	}
	if err := r.RemovePlayer("missing"); err == nil {
		t.Fatal("RemovePlayer(missing) error = nil, want non-nil")
	}
	if len(r.Players) != 1 {
		t.Fatalf("len(Players) = %d, want 1", len(r.Players))
	}
	if _, ok := r.Players["p2"]; !ok {
		t.Fatal("player p2 missing after removing p1")
	}
}

func TestRoomAdvanceIncrementsTick(t *testing.T) {
	r, err := NewRoom("room-1", []string{"p1"})
	if err != nil {
		t.Fatalf("NewRoom() error = %v", err)
	}
	for i := 0; i < 3; i++ {
		r.Advance()
	}
	if r.Tick != 3 {
		t.Fatalf("Tick = %d, want 3", r.Tick)
	}
}
