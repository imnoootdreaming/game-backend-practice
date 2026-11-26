package room

import (
	"context"
	"testing"
	"time"
)

func TestRoomRunRejectsNonPositiveInterval(t *testing.T) {
	r, err := NewRoom("room-1", []string{"p1"})
	if err != nil {
		t.Fatalf("NewRoom() error = %v", err)
	}
	for _, interval := range []time.Duration{0, -time.Second} {
		if err := r.Run(context.Background(), interval); err == nil {
			t.Fatalf("Run(interval=%s) error = nil, want non-nil", interval)
		}
	}
}

func TestRoomRunAdvancesTick(t *testing.T) {
	r, err := NewRoom("room-1", []string{"p1"})
	if err != nil {
		t.Fatalf("NewRoom() error = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- r.Run(ctx, 5*time.Millisecond) }()

	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		r.mu.RLock()
		tick := r.Tick
		r.mu.RUnlock()
		if tick >= 3 {
			break
		}
		time.Sleep(time.Millisecond)
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
	r.mu.RLock()
	final := r.Tick
	r.mu.RUnlock()
	if final < 3 {
		t.Fatalf("final Tick = %d, want at least 3", final)
	}
}

func TestRoomRunStopsOnContextCancel(t *testing.T) {
	r, err := NewRoom("room-1", []string{"p1"})
	if err != nil {
		t.Fatalf("NewRoom() error = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- r.Run(ctx, time.Hour) }()
	cancel()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run() error = %v, want nil", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Run did not stop after context cancel")
	}
}
