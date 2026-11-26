package room

import "testing"

func TestManagerCreateAndGet(t *testing.T) {
	m := NewManager()
	r, err := m.Create("room-1", []string{"p1", "p2"})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	got, ok := m.Get("room-1")
	if !ok {
		t.Fatal("Get(room-1) = false, want true")
	}
	if got != r {
		t.Fatal("Get() returned a different room instance")
	}
	if len(got.Players) != 2 {
		t.Fatalf("len(Players) = %d, want 2", len(got.Players))
	}
	if _, ok := m.Get("missing"); ok {
		t.Fatal("Get(missing) = true, want false")
	}
}

func TestManagerRejectsDuplicateRoom(t *testing.T) {
	m := NewManager()
	if _, err := m.Create("room-1", []string{"p1"}); err != nil {
		t.Fatalf("first Create() error = %v", err)
	}
	if _, err := m.Create("room-1", []string{"p2"}); err == nil {
		t.Fatal("duplicate Create() error = nil, want non-nil")
	}
	if _, err := m.Create("", []string{"p1"}); err == nil {
		t.Fatal("Create with empty id error = nil, want non-nil")
	}
}

func TestManagerRemove(t *testing.T) {
	m := NewManager()
	if _, err := m.Create("room-1", []string{"p1"}); err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if !m.Remove("room-1") {
		t.Fatal("Remove(room-1) = false, want true")
	}
	if _, ok := m.Get("room-1"); ok {
		t.Fatal("room still exists after Remove")
	}
	if m.Remove("missing") {
		t.Fatal("Remove(missing) = true, want false")
	}
}

func TestManagerRoomOf(t *testing.T) {
	m := NewManager()
	r, err := m.Create("room-1", []string{"p1", "p2"})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	for _, id := range []string{"p1", "p2"} {
		got, ok := m.RoomOf(id)
		if !ok || got != r {
			t.Fatalf("RoomOf(%s) = (%v, %v), want created room", id, got, ok)
		}
	}
	if _, ok := m.RoomOf("missing"); ok {
		t.Fatal("RoomOf(missing) = true, want false")
	}
	if !m.Remove("room-1") {
		t.Fatal("Remove(room-1) = false, want true")
	}
	if _, ok := m.RoomOf("p1"); ok {
		t.Fatal("RoomOf(p1) still true after removing room")
	}
}
