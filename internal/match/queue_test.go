package match

import (
	"fmt"
	"sync"
	"testing"
	"time"
)

func newTestEntry(playerID, partyID string, partySize, rank int, at time.Time) Entry {
	return Entry{
		PlayerID:   playerID,
		PartyID:    partyID,
		PartySize:  partySize,
		Rank:       rank,
		EnqueuedAt: at,
	}
}

func assertPlayerIDs(t *testing.T, got []Entry, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("player count = %d, want %d (got %+v)", len(got), len(want), got)
	}
	for i := range want {
		if got[i].PlayerID != want[i] {
			t.Fatalf("player[%d] = %q, want %q", i, got[i].PlayerID, want[i])
		}
	}
}

func TestQueueEnqueueRejectsInvalidEntry(t *testing.T) {
	base := time.Date(2026, 11, 25, 10, 0, 0, 0, time.UTC)
	cases := []struct {
		name  string
		entry Entry
	}{
		{"empty player id", newTestEntry("", "party-1", 1, 1000, base)},
		{"empty party id", newTestEntry("p1", "", 1, 1000, base)},
		{"zero party size", newTestEntry("p1", "party-1", 0, 1000, base)},
		{"negative rank", newTestEntry("p1", "party-1", 1, -1, base)},
		{"zero enqueued time", newTestEntry("p1", "party-1", 1, 1000, time.Time{})},
	}
	q := NewQueue()
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			added, err := q.Enqueue(tc.entry)
			if err == nil {
				t.Fatalf("Enqueue(%+v) error = nil, want non-nil", tc.entry)
			}
			if added {
				t.Fatalf("Enqueue(%+v) added = true, want false", tc.entry)
			}
			if q.Len() != 0 {
				t.Fatalf("queue length = %d after invalid enqueue, want 0", q.Len())
			}
		})
	}
}

func TestQueueEnqueueIsIdempotent(t *testing.T) {
	base := time.Date(2026, 11, 25, 10, 0, 0, 0, time.UTC)
	q := NewQueue()
	first := newTestEntry("p1", "party-1", 1, 1200, base)
	if added, err := q.Enqueue(first); err != nil || !added {
		t.Fatalf("first Enqueue = (%v, %v), want (true, nil)", added, err)
	}

	duplicate := first
	duplicate.Rank = 999
	duplicate.EnqueuedAt = base.Add(time.Hour)
	if added, err := q.Enqueue(duplicate); err != nil || added {
		t.Fatalf("duplicate Enqueue = (%v, %v), want (false, nil)", added, err)
	}
	if q.Len() != 1 {
		t.Fatalf("queue length = %d, want 1", q.Len())
	}
	got, ok := q.Get("p1")
	if !ok {
		t.Fatal("Get(p1) = false, want true")
	}
	if got.Rank != 1200 || !got.EnqueuedAt.Equal(base) {
		t.Fatalf("duplicate enqueue overwrote original entry: %+v", got)
	}
}

func TestQueueCandidatesFiltersByPartySizeAndRankWindow(t *testing.T) {
	base := time.Date(2026, 11, 25, 10, 0, 0, 0, time.UTC)
	q := NewQueue()
	entries := []Entry{
		newTestEntry("solo-early", "party-s1", 1, 1000, base),
		newTestEntry("solo-late", "party-s2", 1, 1300, base.Add(time.Second)),
		newTestEntry("solo-far", "party-s3", 1, 4000, base.Add(2*time.Second)),
		newTestEntry("duo-1", "party-d1", 2, 1100, base.Add(3*time.Second)),
		newTestEntry("duo-2", "party-d1", 2, 1200, base.Add(4*time.Second)),
	}
	for _, e := range entries {
		if added, err := q.Enqueue(e); err != nil || !added {
			t.Fatalf("Enqueue(%+v) = (%v, %v), want (true, nil)", e, added, err)
		}
	}

	assertPlayerIDs(t, q.Candidates(1, 900, 1400), []string{"solo-early", "solo-late"})
	assertPlayerIDs(t, q.Candidates(1, 900, 1100), []string{"solo-early"})
	assertPlayerIDs(t, q.Candidates(2, 900, 1400), []string{"duo-1", "duo-2"})
	if got := q.Candidates(1, 1400, 900); len(got) != 0 {
		t.Fatalf("Candidates with reversed rank window = %+v, want empty", got)
	}
}

func TestQueueCandidatesSortedByEnqueuedAtThenPlayerID(t *testing.T) {
	base := time.Date(2026, 11, 25, 10, 0, 0, 0, time.UTC)
	q := NewQueue()
	// 故意乱序入队，验证输出按等待时间排序；同一时间按 playerID 排序保证确定性。
	for _, e := range []Entry{
		newTestEntry("b", "party-b", 1, 1000, base.Add(2*time.Second)),
		newTestEntry("c", "party-c", 1, 1000, base),
		newTestEntry("a", "party-a", 1, 1000, base),
	} {
		if _, err := q.Enqueue(e); err != nil {
			t.Fatalf("Enqueue(%+v) error = %v", e, err)
		}
	}
	assertPlayerIDs(t, q.Candidates(1, 0, 2000), []string{"a", "c", "b"})
}

func TestQueueRemoveAndRemoveAll(t *testing.T) {
	base := time.Date(2026, 11, 25, 10, 0, 0, 0, time.UTC)
	q := NewQueue()
	for _, id := range []string{"p1", "p2", "p3"} {
		if _, err := q.Enqueue(newTestEntry(id, "party-"+id, 1, 1000, base)); err != nil {
			t.Fatalf("Enqueue(%s) error = %v", id, err)
		}
	}
	if !q.Remove("p1") {
		t.Fatal("Remove(p1) = false, want true")
	}
	if q.Remove("p1") {
		t.Fatal("Remove(p1) twice = true, want false (idempotent)")
	}
	q.RemoveAll([]string{"p2", "p3", "missing"})
	if q.Len() != 0 {
		t.Fatalf("queue length = %d after removals, want 0", q.Len())
	}
}

func TestQueueConcurrentOperations(t *testing.T) {
	base := time.Date(2026, 11, 25, 10, 0, 0, 0, time.UTC)
	q := NewQueue()
	var wg sync.WaitGroup
	for i := 0; i < 64; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			id := fmt.Sprintf("p%02d", i)
			_, _ = q.Enqueue(newTestEntry(id, "party-"+id, 1, i*10, base.Add(time.Duration(i)*time.Millisecond)))
			_ = q.Candidates(1, 0, 1000)
			if i%2 == 0 {
				q.Remove(id)
			}
		}(i)
	}
	wg.Wait()
	for i := 0; i < 64; i++ {
		id := fmt.Sprintf("p%02d", i)
		_, ok := q.Get(id)
		if i%2 == 0 && ok {
			t.Fatalf("even player %s still in queue, want removed", id)
		}
		if i%2 == 1 && !ok {
			t.Fatalf("odd player %s missing from queue", id)
		}
	}
}
