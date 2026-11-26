// package lobby 实现“大厅”服务。
// 它对应和平精英里进战斗前的那个大厅界面：玩家在这里登录、邀请好友组队、点准备，然后开始匹配。
// 单独做这一层，是为了在开打之前把人和队伍组织好；战斗房间不用再处理“谁和谁一队、谁准备好了”。
// 代码包含玩家状态机、组队管理，以及给网络层调用的消息处理器。
package lobby

import "fmt"

// State 表示玩家目前走到哪一步，比如“在大厅”“已准备”“匹配中”“战斗中”。
// 内部用数字存，比较起来快；String() 让日志里能看懂。
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
