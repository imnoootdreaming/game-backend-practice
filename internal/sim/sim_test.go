package sim

import "testing"

func TestMoveAllowed(t *testing.T) {
	cases := []struct {
		name        string  // 用例名
		dx          float32 // X 方向位移
		dy          float32 // Y 方向位移
		maxDistance float32 // 允许的最大位移
		want        bool    // 期望是否放行
	}{
		{"within", 0.5, 0.5, 1, true},
		{"boundary", 1, 0, 1, true},
		{"outside", 1.1, 0, 1, false},
		{"zero limit", 0, 0, 0, true},
		{"negative limit", 0, 0, -1, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := MoveAllowed(tc.dx, tc.dy, tc.maxDistance); got != tc.want {
				t.Fatalf("MoveAllowed(%v, %v, %v) = %v, want %v", tc.dx, tc.dy, tc.maxDistance, got, tc.want)
			}
		})
	}
}

func TestInRange(t *testing.T) {
	cases := []struct {
		name        string  // 用例名
		ax          float32 // 起点 X
		ay          float32 // 起点 Y
		bx          float32 // 目标 X
		by          float32 // 目标 Y
		maxDistance float32 // 最大射程
		want        bool    // 期望是否命中
	}{
		{"near", 0, 0, 1, 1, 2, true},
		{"boundary", 0, 0, 2, 0, 2, true},
		{"far", 0, 0, 2.1, 0, 2, false},
		{"negative range", 0, 0, 0, 0, -1, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := InRange(tc.ax, tc.ay, tc.bx, tc.by, tc.maxDistance); got != tc.want {
				t.Fatalf("InRange() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestApplyDamage(t *testing.T) {
	cases := []struct {
		name   string // 用例名
		hp     int32  // 当前血量
		damage int32  // 伤害值
		want   int32  // 期望血量
	}{
		{"normal", 100, 25, 75},
		{"clamp to zero", 10, 25, 0},
		{"already dead", 0, 25, 0},
		{"non-positive damage", 100, 0, 100},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ApplyDamage(tc.hp, tc.damage); got != tc.want {
				t.Fatalf("ApplyDamage(%d, %d) = %d, want %d", tc.hp, tc.damage, got, tc.want)
			}
		})
	}
}
