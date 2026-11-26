// Package sim 提供与房间状态无关的纯战斗判定函数：移动合法性、射程和伤害结算。
package sim

// MoveAllowed 判断一段位移是否在允许范围内；上限为负时一律拒绝。
func MoveAllowed(dx, dy, maxDistance float32) bool {
	if maxDistance < 0 {
		return false
	}
	// 用距离平方比较，既避免开方，也能正确处理 X/Y 的合成位移。
	return dx*dx+dy*dy <= maxDistance*maxDistance
}

// InRange 判断目标点是否在起点允许距离内；射程为负时一律不命中。
func InRange(ax, ay, bx, by, maxDistance float32) bool {
	if maxDistance < 0 {
		return false
	}
	dx := bx - ax
	dy := by - ay
	return dx*dx+dy*dy <= maxDistance*maxDistance
}

// ApplyDamage 结算扣血；非正伤害不扣血，血量最低钳到 0。
func ApplyDamage(hp, damage int32) int32 {
	if damage <= 0 || hp <= 0 {
		return hp
	}
	next := hp - damage
	if next < 0 {
		return 0
	}
	return next
}
