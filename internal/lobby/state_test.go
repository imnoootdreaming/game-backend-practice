package lobby

import "testing"

// TestCanTransition 覆盖计划 1.5 列出的所有合法状态跳转。
// 包括正常前进、取消操作回退，以及断线回到 Offline。
func TestCanTransition(t *testing.T) {
	cases := []struct {
		name     string
		from, to State
	}{
		{"login enters lobby", Offline, Lobby},
		{"ready", Lobby, Ready},
		{"unready", Ready, Lobby},
		{"match from lobby", Lobby, Matching},
		{"match from ready", Ready, Matching},
		{"cancel match", Matching, Lobby},
		{"assign room", Matching, InRoom},
		{"settle", InRoom, Settling},
		{"back to lobby", Settling, Lobby},
		{"disconnect from lobby", Lobby, Offline},
		{"disconnect from ready", Ready, Offline},
		{"disconnect from matching", Matching, Offline},
		{"disconnect from inroom", InRoom, Offline},
		{"disconnect from settling", Settling, Offline},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if !CanTransition(tc.from, tc.to) {
				t.Fatalf("CanTransition(%s, %s) = false, want true", tc.from, tc.to)
			}
		})
	}
}

// TestInvalidTransitions 验证计划点名的非法跳转（从 Matching 到 Ready）以及常见越级跳转被拒。
// 相同状态之间也不允许跳转，避免重复触发副作用（如重复结算）。
func TestInvalidTransitions(t *testing.T) {
	cases := []struct {
		name     string
		from, to State
	}{
		{"matching to ready", Matching, Ready},
		{"offline to inroom", Offline, InRoom},
		{"lobby to inroom", Lobby, InRoom},
		{"ready to settling", Ready, Settling},
		{"inroom to matching", InRoom, Matching},
		{"settle to inroom", Settling, InRoom},
		{"same state", Lobby, Lobby},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if CanTransition(tc.from, tc.to) {
				t.Fatalf("CanTransition(%s, %s) = true, want false", tc.from, tc.to)
			}
		})
	}
}

// TestTransition 验证合法跳转返回目标状态，非法跳转返回错误。
func TestTransition(t *testing.T) {
	got, err := Transition(Lobby, Ready)
	if err != nil || got != Ready {
		t.Fatalf("Transition(Lobby, Ready) = (%s, %v), want (Ready, nil)", got, err)
	}
	if _, err := Transition(Matching, Ready); err == nil {
		t.Fatal("Transition(Matching, Ready) should fail")
	}
}

// TestStateString 保证每个状态有可读名字；日志和错误信息都依赖它。
func TestStateString(t *testing.T) {
	want := map[State]string{
		Offline: "Offline", Lobby: "Lobby", Ready: "Ready",
		Matching: "Matching", InRoom: "InRoom", Settling: "Settling",
	}
	for state, name := range want {
		if got := state.String(); got != name {
			t.Errorf("State(%d).String() = %q, want %q", state, got, name)
		}
	}
}
