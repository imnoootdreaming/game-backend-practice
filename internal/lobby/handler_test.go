package lobby

import (
	"testing"

	"game-backend-practice/internal/match"
	"game-backend-practice/internal/protocol"
)

// TestHandlerLoginAndReady 验证 LoginReq/ReadyReq 驱动状态机，
// 并且以传入的已鉴权 playerID 为准，忽略消息体里可伪造的 PlayerID 字段。
func TestHandlerLoginAndReady(t *testing.T) {
	m := newTestManager(t, 4)
	h := NewHandler(m)

	if err := h.handle("real", &protocol.LoginReq{PlayerID: "fake", Nickname: "x"}); err != nil {
		t.Fatalf("LoginReq: %v", err)
	}
	if got := m.StateOf("real"); got != Lobby {
		t.Fatalf("real state = %s, want Lobby", got)
	}
	if got := m.StateOf("fake"); got != Offline {
		t.Fatalf("fake state = %s, want Offline (must not trust message body)", got)
	}

	err := h.handle("real", &protocol.ReadyReq{PlayerID: "fake", Ready: true})
	if err != nil {
		t.Fatalf("ReadyReq: %v", err)
	}
	if got := m.StateOf("real"); got != Ready {
		t.Fatalf("real state = %s, want Ready", got)
	}
}

// TestHandlerTeamInviteFlow 验证邀请与接受分别映射到 TeamInvite 的 Accept 字段，
// 且邀请人身份取已鉴权 playerID 而非消息体的 From。
func TestHandlerTeamInviteFlow(t *testing.T) {
	m := newTestManager(t, 4)
	h := NewHandler(m)
	m.Login("leader")
	if _, err := m.CreateTeam("leader"); err != nil {
		t.Fatalf("CreateTeam: %v", err)
	}

	if err := h.handle("leader", &protocol.TeamInvite{From: "fake", To: "guest"}); err != nil {
		t.Fatalf("invite: %v", err)
	}
	team := m.Teams().TeamOf("leader")
	if team == nil || !team.Invited["guest"] {
		t.Fatalf("guest should be invited, team = %+v", team)
	}

	h.handle("guest", &protocol.TeamInvite{TeamID: team.ID, Accept: true})
	if got := m.Teams().TeamOf("guest"); got != team {
		t.Fatalf("guest team = %v, want %v", got, team)
	}
}

// TestHandlerIgnoresNonLobbyMessages 验证服务端下发类消息与 nil 被安全忽略，
// 避免 ws 读泵因为 handler 报错而关闭连接。
func TestHandlerIgnoresNonLobbyMessages(t *testing.T) {
	h := NewHandler(newTestManager(t, 4))
	for _, msg := range []protocol.Message{nil, &protocol.LoginResp{}, &protocol.InputCmd{}, &protocol.Snapshot{}} {
		if err := h.handle("p1", msg); err != nil {
			t.Errorf("handle(%v) = %v, want nil", msg, err)
		}
	}
}

func TestHandlerReadyEnqueuesPlayer(t *testing.T) {
	m := newTestManager(t, 4)
	q := match.NewQueue()
	h := NewHandlerWithMatch(m, q)
	if err := h.handle("p1", &protocol.LoginReq{}); err != nil {
		t.Fatalf("LoginReq: %v", err)
	}
	if err := h.handle("p1", &protocol.ReadyReq{Ready: true, Rank: 1200}); err != nil {
		t.Fatalf("ReadyReq: %v", err)
	}
	if got := m.StateOf("p1"); got != Matching {
		t.Fatalf("state = %s, want Matching", got)
	}
	e, ok := q.Get("p1")
	if !ok {
		t.Fatal("player p1 not enqueued")
	}
	if e.Rank != 1200 || e.PartyID != "p1" || e.PartySize != 1 || e.EnqueuedAt.IsZero() {
		t.Fatalf("entry = %+v, want rank=1200 party=p1 size=1 non-zero time", e)
	}
}

func TestHandlerCancelReadyRemovesFromQueue(t *testing.T) {
	m := newTestManager(t, 4)
	q := match.NewQueue()
	h := NewHandlerWithMatch(m, q)
	if err := h.handle("p1", &protocol.LoginReq{}); err != nil {
		t.Fatalf("LoginReq: %v", err)
	}
	if err := h.handle("p1", &protocol.ReadyReq{Ready: true, Rank: 1200}); err != nil {
		t.Fatalf("ReadyReq: %v", err)
	}
	if err := h.handle("p1", &protocol.ReadyReq{Ready: false}); err != nil {
		t.Fatalf("cancel ReadyReq: %v", err)
	}
	if got := m.StateOf("p1"); got != Lobby {
		t.Fatalf("state = %s, want Lobby", got)
	}
	if _, ok := q.Get("p1"); ok {
		t.Fatal("player p1 still in match queue after cancel ready")
	}
}

func TestHandlerReadyRejectsNegativeRank(t *testing.T) {
	m := newTestManager(t, 4)
	q := match.NewQueue()
	h := NewHandlerWithMatch(m, q)
	if err := h.handle("p1", &protocol.LoginReq{}); err != nil {
		t.Fatalf("LoginReq: %v", err)
	}
	if err := h.handle("p1", &protocol.ReadyReq{Ready: true, Rank: -1}); err == nil {
		t.Fatal("ReadyReq with negative rank error = nil, want non-nil")
	}
	if q.Len() != 0 {
		t.Fatalf("queue length = %d, want 0", q.Len())
	}
}

func newTeamForTest(t *testing.T, m *Manager) (*Team, *match.Queue, *Handler) {
	t.Helper()
	q := match.NewQueue()
	h := NewHandlerWithMatch(m, q)
	for _, id := range []string{"leader", "guest"} {
		if err := m.Login(id); err != nil {
			t.Fatalf("Login(%s): %v", id, err)
		}
	}
	team, err := m.CreateTeam("leader")
	if err != nil {
		t.Fatalf("CreateTeam: %v", err)
	}
	if err := m.Invite("leader", "guest"); err != nil {
		t.Fatalf("Invite: %v", err)
	}
	if err := m.Accept("guest", team.ID); err != nil {
		t.Fatalf("Accept: %v", err)
	}
	return team, q, h
}

func TestHandlerReadyEnqueuesWholeTeam(t *testing.T) {
	m := newTestManager(t, 4)
	team, q, h := newTeamForTest(t, m)

	// 第一个人准备时队友还没准备，应该被拒绝，不产生排队记录。
	if err := h.handle("leader", &protocol.ReadyReq{Ready: true, Rank: 1200}); err == nil {
		t.Fatal("leader ready before guest error = nil, want non-nil")
	}
	if q.Len() != 0 {
		t.Fatalf("queue length = %d, want 0 before all ready", q.Len())
	}

	if err := h.handle("guest", &protocol.ReadyReq{Ready: true, Rank: 1300}); err != nil {
		t.Fatalf("guest ready: %v", err)
	}
	if q.Len() != 2 {
		t.Fatalf("queue length = %d, want 2", q.Len())
	}
	for _, id := range []string{"leader", "guest"} {
		e, ok := q.Get(id)
		if !ok {
			t.Fatalf("player %s not enqueued", id)
		}
		if e.PartyID != team.ID || e.PartySize != 2 || e.Rank != 1300 {
			t.Fatalf("entry[%s] = %+v, want party=%s size=2 rank=1300", id, e, team.ID)
		}
		if got := m.StateOf(id); got != Matching {
			t.Fatalf("state[%s] = %s, want Matching", id, got)
		}
	}
}

func TestHandlerCancelReadyRemovesWholeTeam(t *testing.T) {
	m := newTestManager(t, 4)
	_, q, h := newTeamForTest(t, m)

	// leader 先准备会因为 guest 未准备而报错；guest 准备后才真正整队入队。
	_ = h.handle("leader", &protocol.ReadyReq{Ready: true, Rank: 1200})
	if err := h.handle("guest", &protocol.ReadyReq{Ready: true, Rank: 1300}); err != nil {
		t.Fatalf("guest ready: %v", err)
	}
	if q.Len() != 2 {
		t.Fatalf("queue length = %d, want 2 after all ready", q.Len())
	}

	if err := h.handle("guest", &protocol.ReadyReq{Ready: false}); err != nil {
		t.Fatalf("cancel ready: %v", err)
	}
	if q.Len() != 0 {
		t.Fatalf("queue length = %d, want 0 after cancel", q.Len())
	}
	for _, id := range []string{"leader", "guest"} {
		if got := m.StateOf(id); got != Lobby {
			t.Fatalf("state[%s] = %s, want Lobby", id, got)
		}
	}
}
