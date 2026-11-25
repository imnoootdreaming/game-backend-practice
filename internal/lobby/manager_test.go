package lobby

import "testing"

func newTestManager(t *testing.T, size int) *Manager {
	t.Helper()
	m, err := NewManager(size)
	if err != nil {
		t.Fatalf("NewManager(%d): %v", size, err)
	}
	return m
}

// TestManagerPlayerLifecycle 覆盖完整状态链，依次经过：
// Offline、Lobby、Ready、Matching、InRoom、Settling，再回到 Lobby 与 Offline。
func TestManagerPlayerLifecycle(t *testing.T) {
	m := newTestManager(t, 4)
	if got := m.StateOf("p1"); got != Offline {
		t.Fatalf("initial state = %s, want Offline", got)
	}
	if err := m.Login("p1"); err != nil {
		t.Fatalf("Login: %v", err)
	}
	if got := m.StateOf("p1"); got != Lobby {
		t.Fatalf("after Login = %s, want Lobby", got)
	}
	if err := m.SetReady("p1", true); err != nil {
		t.Fatalf("SetReady(true): %v", err)
	}
	if got := m.StateOf("p1"); got != Ready {
		t.Fatalf("after SetReady(true) = %s, want Ready", got)
	}
	if err := m.SetReady("p1", false); err != nil {
		t.Fatalf("SetReady(false): %v", err)
	}
	if got := m.StateOf("p1"); got != Lobby {
		t.Fatalf("after SetReady(false) = %s, want Lobby", got)
	}
	if err := m.EnterMatching("p1"); err != nil {
		t.Fatalf("EnterMatching: %v", err)
	}
	if err := m.AssignRoom("p1"); err != nil {
		t.Fatalf("AssignRoom: %v", err)
	}
	if got := m.StateOf("p1"); got != InRoom {
		t.Fatalf("after AssignRoom = %s, want InRoom", got)
	}
	if err := m.Settle("p1"); err != nil {
		t.Fatalf("Settle: %v", err)
	}
	if err := m.FinishSettle("p1"); err != nil {
		t.Fatalf("FinishSettle: %v", err)
	}
	if got := m.StateOf("p1"); got != Lobby {
		t.Fatalf("after FinishSettle = %s, want Lobby", got)
	}
	if err := m.Disconnect("p1"); err != nil {
		t.Fatalf("Disconnect: %v", err)
	}
	if got := m.StateOf("p1"); got != Offline {
		t.Fatalf("after Disconnect = %s, want Offline", got)
	}
}

// TestManagerRejectsInvalidTransitions 验证非法跳转被拒且状态不发生变化。
func TestManagerRejectsInvalidTransitions(t *testing.T) {
	m := newTestManager(t, 4)
	if err := m.Login("p1"); err != nil {
		t.Fatal(err)
	}
	m.SetReady("p1", true)
	m.EnterMatching("p1")
	if err := m.SetReady("p1", true); err == nil {
		t.Fatal("ready while matching should fail")
	}
	if got := m.StateOf("p1"); got != Matching {
		t.Fatalf("failed transition changed state to %s, want Matching", got)
	}
	if err := m.AssignRoom("ghost"); err == nil {
		t.Fatal("assign room to unknown player should fail")
	}
}

// TestManagerDisconnectCleansTeam 验证断线时玩家从队伍移除，队伍人数更新。
func TestManagerDisconnectCleansTeam(t *testing.T) {
	m := newTestManager(t, 4)
	m.Login("leader")
	team, err := m.CreateTeam("leader")
	if err != nil {
		t.Fatalf("CreateTeam: %v", err)
	}
	m.Login("m1")
	m.Invite("leader", "m1")
	m.Accept("m1", team.ID)
	if len(team.Members) != 2 {
		t.Fatalf("members = %v, want 2", team.Members)
	}
	if err := m.Disconnect("m1"); err != nil {
		t.Fatalf("Disconnect: %v", err)
	}
	if m.Teams().TeamOf("m1") != nil {
		t.Fatal("disconnected player should be removed from team")
	}
	if len(team.Members) != 1 {
		t.Fatalf("members = %v, want [leader]", team.Members)
	}
}

// TestManagerRejectsBadNewManager 验证非法队伍容量在构造阶段被拒绝。
func TestManagerRejectsBadNewManager(t *testing.T) {
	if _, err := NewManager(0); err == nil {
		t.Fatal("NewManager(0) should fail")
	}
}
