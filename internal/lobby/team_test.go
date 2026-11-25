package lobby

import (
	"fmt"
	"sync"
	"testing"
)

func newTestTeamManager(t *testing.T, size int) *TeamManager {
	t.Helper()
	tm, err := NewTeamManager(size)
	if err != nil {
		t.Fatalf("NewTeamManager(%d): %v", size, err)
	}
	return tm
}

// TestNewTeamManagerErrors 验证非法队伍容量被拒绝（0 或负数会导致永远无法成队）。
func TestNewTeamManagerErrors(t *testing.T) {
	for _, size := range []int{0, -1} {
		if _, err := NewTeamManager(size); err == nil {
			t.Errorf("NewTeamManager(%d) should fail", size)
		}
	}
}

// TestTeamCreate 验证创建队伍后创建者自动成为唯一成员兼队长，并形成 Map<playerID, teamID> 映射。
func TestTeamCreate(t *testing.T) {
	tm := newTestTeamManager(t, 4)
	team, err := tm.Create("p1")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if team.ID == "" {
		t.Fatal("team ID must not be empty")
	}
	if len(team.Members) != 1 || team.Members[0] != "p1" {
		t.Fatalf("members = %v, want [p1]", team.Members)
	}
	if got := tm.TeamOf("p1"); got != team {
		t.Fatalf("TeamOf(p1) = %v, want created team", got)
	}
	if got := tm.TeamOf("p2"); got != nil {
		t.Fatalf("TeamOf(p2) = %v, want nil", got)
	}
	if tm.Len() != 1 {
		t.Fatalf("Len() = %d, want 1", tm.Len())
	}
}

// TestTeamCreateDuplicate 验证同一玩家不能重复建队。
func TestTeamCreateDuplicate(t *testing.T) {
	tm := newTestTeamManager(t, 4)
	tm.Create("p1")
	if _, err := tm.Create("p1"); err == nil {
		t.Fatal("duplicate Create should fail")
	}
}

// TestInviteAndAccept 验证邀请阶段不直接入队，接受后才加入并被记录准备状态。
func TestInviteAndAccept(t *testing.T) {
	tm := newTestTeamManager(t, 4)
	team, _ := tm.Create("leader")
	if err := tm.Invite("leader", "guest"); err != nil {
		t.Fatalf("Invite: %v", err)
	}
	if tm.TeamOf("guest") != nil {
		t.Fatal("invitee should not join before accepting")
	}
	if err := tm.Accept("guest", team.ID); err != nil {
		t.Fatalf("Accept: %v", err)
	}
	if got := tm.TeamOf("guest"); got != team {
		t.Fatalf("TeamOf(guest) = %v, want team", got)
	}
	if len(team.Members) != 2 || team.Members[1] != "guest" {
		t.Fatalf("members = %v, want [leader guest]", team.Members)
	}
	if team.Ready == nil {
		t.Fatal("Ready map must be initialized")
	}
}

// TestInviteRejectsInvalidInput 验证非成员邀请、空被邀请人、玩家已在其他队伍均被拒；
// 重复邀请同一人保持幂等，避免网络重试导致报错。
func TestInviteRejectsInvalidInput(t *testing.T) {
	tm := newTestTeamManager(t, 4)
	tm.Create("leader")
	if err := tm.Invite("outsider", "guest"); err == nil {
		t.Fatal("invite by non-member should fail")
	}
	if err := tm.Invite("leader", ""); err == nil {
		t.Fatal("invite empty player should fail")
	}
	if err := tm.Invite("leader", "guest"); err != nil {
		t.Fatalf("first Invite: %v", err)
	}
	if err := tm.Invite("leader", "guest"); err != nil {
		t.Fatalf("duplicate Invite should be idempotent: %v", err)
	}

	other, _ := tm.Create("another")
	_ = other
	if err := tm.Invite("leader", "another"); err == nil {
		t.Fatal("invite player already in another team should fail")
	}
}

// TestAcceptRejectsInvalidTeam 验证接受不存在的队伍、接受者已是队长、接受满员队伍均被拒。
func TestAcceptRejectsInvalidTeam(t *testing.T) {
	tm := newTestTeamManager(t, 2)
	if err := tm.Accept("guest", "missing"); err == nil {
		t.Fatal("accept unknown team should fail")
	}
	team, _ := tm.Create("leader")
	if err := tm.Accept("leader", team.ID); err == nil {
		t.Fatal("leader cannot accept own team as invitee")
	}
	tm.Invite("leader", "guest")
	tm.Accept("guest", team.ID)
	tm.Invite("leader", "third")
	if err := tm.Accept("third", team.ID); err == nil {
		t.Fatal("accept into full team should fail")
	}
}

// TestTeamFull 验证满员后邀请被拒。
func TestTeamFull(t *testing.T) {
	tm := newTestTeamManager(t, 2)
	team, _ := tm.Create("p1")
	tm.Invite("p1", "p2")
	tm.Accept("p2", team.ID)
	if len(team.Members) != 2 {
		t.Fatalf("members = %v, want 2", team.Members)
	}
	if err := tm.Invite("p1", "p3"); err == nil {
		t.Fatal("invite to full team should fail")
	}
}

// TestLeaveCleanup 验证退出后 Map<playerID, teamID> 映射被清理；队长退出转移给下一位；
// 最后一人退出时队伍本身被删除。
func TestLeaveCleanup(t *testing.T) {
	tm := newTestTeamManager(t, 4)
	team, _ := tm.Create("leader")
	tm.Invite("leader", "m1")
	tm.Accept("m1", team.ID)
	tm.Invite("leader", "m2")
	tm.Accept("m2", team.ID)

	if err := tm.Leave("leader"); err != nil {
		t.Fatalf("Leave(leader): %v", err)
	}
	if tm.TeamOf("leader") != nil {
		t.Fatal("leader should have no team after leaving")
	}
	if team.Members[0] != "m1" {
		t.Fatalf("new leader = %s, want m1", team.Members[0])
	}
	if team.Ready["leader"] {
		t.Fatal("leaver's ready state should be cleared")
	}

	tm.Leave("m1")
	if tm.Len() != 1 {
		t.Fatalf("Len() = %d, want 1 while one member remains", tm.Len())
	}
	tm.Leave("m2")
	if tm.Len() != 0 {
		t.Fatalf("Len() = %d, want 0 after last member leaves", tm.Len())
	}
	if tm.TeamOf("m2") != nil {
		t.Fatal("m2 should have no team")
	}
	if err := tm.Leave("ghost"); err == nil {
		t.Fatal("Leave non-member should fail")
	}
}

// TestSetReady 验证准备状态写入与清除，以及非成员设置准备被拒。
func TestSetReady(t *testing.T) {
	tm := newTestTeamManager(t, 4)
	team, _ := tm.Create("leader")
	tm.Invite("leader", "m1")
	tm.Accept("m1", team.ID)

	if err := tm.SetReady("m1", true); err != nil {
		t.Fatalf("SetReady(true): %v", err)
	}
	if !team.Ready["m1"] {
		t.Fatal("m1 should be ready")
	}
	if err := tm.SetReady("m1", false); err != nil {
		t.Fatalf("SetReady(false): %v", err)
	}
	if team.Ready["m1"] {
		t.Fatal("m1 should not be ready")
	}
	if err := tm.SetReady("outsider", true); err == nil {
		t.Fatal("SetReady for non-member should fail")
	}
}

// TestTeamManagerConcurrent 并发邀请/接受/准备，供 -race 检测数据竞争；
// 队伍容量 100 刚好容纳 50 个并发加入者，避免满员拒绝干扰断言。
func TestTeamManagerConcurrent(t *testing.T) {
	tm := newTestTeamManager(t, 100)
	team, _ := tm.Create("leader")
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			id := fmt.Sprintf("p%d", i)
			if err := tm.Invite("leader", id); err != nil {
				t.Error(err)
				return
			}
			if err := tm.Accept(id, team.ID); err != nil {
				t.Error(err)
				return
			}
			if err := tm.SetReady(id, true); err != nil {
				t.Error(err)
			}
		}(i)
	}
	wg.Wait()
	if len(team.Members) != 51 {
		t.Fatalf("members = %d, want 51", len(team.Members))
	}
}
