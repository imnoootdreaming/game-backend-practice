package ws

import (
	"strings"
	"sync"
	"testing"
)

// fakePeer 是注册表测试用的假连接，只实现 Peer 需要的 ID 和 Close
type fakePeer struct{ id string }

func (p *fakePeer) ID() string   { return p.id }
func (p *fakePeer) Close() error { return nil }

func TestRegistryAddGetRemove(t *testing.T) {
	r := NewRegistry()
	p := &fakePeer{id: "p1"}

	if err := r.Add(p); err != nil {
		t.Fatalf("Add() error = %v", err)
	}
	if got, ok := r.Get("p1"); !ok || got != p {
		t.Fatalf("Get(p1) = %#v, ok = %v", got, ok)
	}
	if r.Len() != 1 {
		t.Errorf("Len() = %d, want 1", r.Len())
	}
	if err := r.Add(&fakePeer{id: "p1"}); err == nil || !strings.Contains(err.Error(), "already registered") {
		t.Errorf("Add(duplicate) error = %v, want containing %q", err, "already registered")
	}
	if r.Len() != 1 {
		t.Errorf("Len() after duplicate = %d, want 1", r.Len())
	}

	if !r.Remove("p1") {
		t.Error("Remove(p1) = false, want true")
	}
	if r.Remove("p1") {
		t.Error("Remove(p1) twice = true, want false")
	}
	if r.Len() != 0 {
		t.Errorf("Len() after remove = %d, want 0", r.Len())
	}
	if ids := r.IDs(); len(ids) != 0 {
		t.Errorf("IDs() = %v, want empty", ids)
	}
}

func TestRegistryConcurrentAccess(t *testing.T) {
	r := NewRegistry()
	const n = 100
	var wg sync.WaitGroup

	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			id := "p" + string(rune('a'+i%26)) + "-" + string(rune('a'+i/26))
			if err := r.Add(&fakePeer{id: id}); err != nil {
				t.Errorf("Add(%s) error = %v", id, err)
				return
			}
			if _, ok := r.Get(id); !ok {
				t.Errorf("Get(%s) not found", id)
			}
			_ = r.Len()
			_ = r.IDs()
			r.Remove(id)
		}(i)
	}
	wg.Wait()

	if got := r.Len(); got != 0 {
		t.Errorf("Len() = %d, want 0", got)
	}
}
