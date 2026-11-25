package lobby

import (
	"testing"

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
