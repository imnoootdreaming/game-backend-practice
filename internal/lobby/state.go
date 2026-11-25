// package lobby 实现阶段 1.5 的大厅服务：玩家状态机、组队管理，以及给 ws.Router 用的消息处理器。
package lobby

import "fmt"

// State 是玩家在登录、大厅、匹配、房间、结算这一流程中的位置。
// 用 int 而不是 string，是因为比较和 switch 更便宜；String() 负责日志可读性。
type State int

const (
	Offline  State = iota // 未登录/断线
	Lobby                 // 已登录，在大厅
	Ready                 // 已准备，等待匹配
	Matching              // 在匹配队列中
	InRoom                // 已分配房间，战斗中
	Settling              // 对局结束，结算中
)

func (s State) String() string {
	switch s {
	case Offline:
		return "Offline"
	case Lobby:
		return "Lobby"
	case Ready:
		return "Ready"
	case Matching:
		return "Matching"
	case InRoom:
		return "InRoom"
	case Settling:
		return "Settling"
	default:
		return fmt.Sprintf("State(%d)", int(s))
	}
}

// allowed 是合法跳转表。只允许沿流程前进、取消操作回退、断线回到 Offline。
// 相同状态之间不允许跳转，避免重复结算这类副作用被触发两次。
var allowed = map[State]map[State]bool{
	Offline:  {Lobby: true},
	Lobby:    {Ready: true, Matching: true, Offline: true},
	Ready:    {Lobby: true, Matching: true, Offline: true},
	Matching: {Lobby: true, InRoom: true, Offline: true},
	InRoom:   {Settling: true, Offline: true},
	Settling: {Lobby: true, Offline: true},
}

// CanTransition 报告从 from 跳转到 to 是否合法。
func CanTransition(from, to State) bool {
	return allowed[from][to]
}

// Transition 返回跳转后的状态；非法跳转保持原状态并返回错误。
func Transition(from, to State) (State, error) {
	if !CanTransition(from, to) {
		return from, fmt.Errorf("lobby: invalid state transition %s -> %s", from, to)
	}
	return to, nil
}
