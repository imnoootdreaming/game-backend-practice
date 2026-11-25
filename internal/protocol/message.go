// package protocol 定义客户端与服务端之间交换的消息，以及"4 字节长度前缀 + JSON"的帧格式。
// 当前先用 JSON 把流程跑通；Codec 接口给阶段 3 的 Protobuf 实现留出替换位置。
package protocol

import (
	"encoding/json" // payload 用 RawMessage 先读出 type，再按注册表反序列化
	"fmt"           // 格式化未知类型的错误信息
)

// Message 是所有业务消息的公共接口。
// Type() 返回消息类型名，服务端据此把消息路由到登录/大厅/房间等处理器。
type Message interface {
	Type() string
}

// 消息类型名常量。客户端和服务端必须使用同一套名字，否则解码会被拒绝。
// 名字直接对应结构体名，1.4 做消息路由时可以直接 switch msg.Type()。
const (
	TypeLoginReq   = "LoginReq"   // 登录请求
	TypeLoginResp  = "LoginResp"  // 登录响应（含 token）
	TypeReadyReq   = "ReadyReq"   // 大厅准备/取消准备
	TypeTeamInvite = "TeamInvite" // 组队邀请与应答
	TypeMatchStart = "MatchStart" // 匹配成功，通知玩家
	TypeRoomAssign = "RoomAssign" // 分配房间与初始参数
	TypeInputCmd   = "InputCmd"   // 客户端输入命令（只发输入，不发坐标）
	TypeSnapshot   = "Snapshot"   // 服务器权威状态快照
	TypeGameResult = "GameResult" // 对局结算战绩
)

// Envelope 是 JSON 帧的外层结构：type 指明具体消息，payload 放业务字段。
// Payload 用 RawMessage 而不是 interface{}，是为了先读出 type，再按注册表解成正确的结构体；
// 如果直接解成 map，后续还要再编码一次，既慢又容易丢字段类型（如 uint64 精度）。
type Envelope struct {
	Type    string          `json:"type"`    // 消息类型名，决定 Payload 按哪个结构体解码
	Payload json.RawMessage `json:"payload"` // 业务字段的原始 JSON，解码时再按 Type 还原
}

// registry 是类型名到构造函数的映射。
// 新增消息只需在这里登记一次；未来 Protobuf 版本也可以复用这张表。
var registry = map[string]func() Message{
	TypeLoginReq:   func() Message { return new(LoginReq) },
	TypeLoginResp:  func() Message { return new(LoginResp) },
	TypeReadyReq:   func() Message { return new(ReadyReq) },
	TypeTeamInvite: func() Message { return new(TeamInvite) },
	TypeMatchStart: func() Message { return new(MatchStart) },
	TypeRoomAssign: func() Message { return new(RoomAssign) },
	TypeInputCmd:   func() Message { return new(InputCmd) },
	TypeSnapshot:   func() Message { return new(Snapshot) },
	TypeGameResult: func() Message { return new(GameResult) },
}

// Register 允许扩展新的消息类型（如测试用的假消息、未来的 Protobuf 消息）。
// 只在程序启动阶段调用，不要在并发读写消息的 hot path 中调用。
func Register(msgType string, factory func() Message) {
	registry[msgType] = factory
}

// newMessage 按类型名创建空消息；未知类型直接返回错误，避免静默丢弃对端发来的消息。
func newMessage(msgType string) (Message, error) {
	factory, ok := registry[msgType]
	if !ok {
		return nil, fmt.Errorf("protocol: unknown message type %q", msgType)
	}
	return factory(), nil
}

// LoginReq 是登录请求。首次登录 PlayerID 可为空，由服务端生成后通过 LoginResp 返回。
type LoginReq struct {
	PlayerID string `json:"player_id"` // 已有账号带上；新账号留空
	Nickname string `json:"nickname"`  // 展示昵称
}

func (LoginReq) Type() string { return TypeLoginReq }

// LoginResp 是登录响应。Code 为 0 表示成功，Token 后续用于鉴权和断线重连。
type LoginResp struct {
	Code      int    `json:"code"`       // 0 成功，非 0 为业务错误码
	PlayerID  string `json:"player_id"`  // 服务端分配或确认的玩家 ID
	Token     string `json:"token"`      // HMAC-SHA256 签发的令牌（1.3 实现）
	ExpiresAt int64  `json:"expires_at"` // 过期时间，Unix 秒
}

func (LoginResp) Type() string { return TypeLoginResp }

// ReadyReq 是大厅准备状态变更。Ready=true 准备，false 取消准备。
type ReadyReq struct {
	PlayerID string `json:"player_id"` // 请求玩家（服务端以连接身份为准，不信任此字段）
	Ready    bool   `json:"ready"`     // true 准备，false 取消准备
}

func (ReadyReq) Type() string { return TypeReadyReq }

// TeamInvite 同时承载邀请和应答：Accept=false 表示邀请，true 表示接受。
// 拒绝邀请可复用该结构并把 Accept 设为 false 且 TeamID 为空——阶段 1 只做最小闭环。
type TeamInvite struct {
	TeamID string `json:"team_id"` // 队伍 ID
	From   string `json:"from"`    // 邀请人
	To     string `json:"to"`      // 被邀请人
	Accept bool   `json:"accept"`  // 是否接受
}

func (TeamInvite) Type() string { return TypeTeamInvite }

// MatchStart 通知玩家匹配成功，随后服务端会再发 RoomAssign 指定房间。
type MatchStart struct {
	MatchID   string   `json:"match_id"`   // 本次匹配 ID
	PlayerIDs []string `json:"player_ids"` // 同一局的玩家列表
}

func (MatchStart) Type() string { return TypeMatchStart }

// RoomAssign 把玩家分配到具体房间，TickHz 让客户端用与服务端一致的频率做预测。
type RoomAssign struct {
	RoomID    string   `json:"room_id"`    // 房间 ID
	PlayerIDs []string `json:"player_ids"` // 房间内玩家
	TickHz    int      `json:"tick_hz"`    // 服务器 tick 频率，来自 configs/config.yaml
}

func (RoomAssign) Type() string { return TypeRoomAssign }

// InputCmd 是客户端输入命令。注意没有 X/Y 字段：位置只由服务端 tick 计算，
// 客户端伪造坐标对权威状态无效（阶段 1.7 验收标准）。
type InputCmd struct {
	PlayerID  string  `json:"player_id"` // 命令发出者（服务端以连接身份为准）
	Seq       uint64  `json:"seq"`       // 命令序列号，用于丢弃重复/乱序输入（2.1 使用）
	MoveX     float32 `json:"move_x"`    // 本帧水平移动量
	MoveY     float32 `json:"move_y"`    // 本帧垂直移动量
	Fire      bool    `json:"fire"`      // 是否开火
	Timestamp int64   `json:"timestamp"` // 客户端毫秒时间戳，2.3 延迟补偿回溯用
}

func (InputCmd) Type() string { return TypeInputCmd }

// PlayerState 是快照中单个玩家的状态。LastSeq 是服务端已应用的最后一条命令序列号，
// 客户端据此重放尚未确认的输入（2.2 预测与和解）。
type PlayerState struct {
	ID      string  `json:"id"`       // 玩家 ID
	X       float32 `json:"x"`        // 服务器权威计算出的 X 坐标
	Y       float32 `json:"y"`        // 服务器权威计算出的 Y 坐标
	HP      int32   `json:"hp"`       // 当前血量
	LastSeq uint64  `json:"last_seq"` // 服务端已应用的最后一条命令序列号
}

// Snapshot 是服务器广播的权威状态快照。Tick 单调递增，客户端据此丢弃乱序快照。
type Snapshot struct {
	RoomID  string        `json:"room_id"` // 快照所属房间
	Tick    uint64        `json:"tick"`    // 单调递增的逻辑帧号，客户端据此丢弃乱序快照
	Players []PlayerState `json:"players"` // 房间内全部玩家的权威状态
}

func (Snapshot) Type() string { return TypeSnapshot }

// GameResult 是对局结算战绩。RequestID 是幂等键，阶段 2.6 用它防止重复发奖。
type GameResult struct {
	RoomID    string           `json:"room_id"`    // 对局所在房间
	RequestID string           `json:"request_id"` // 同一局结算重试必须带上同一个值
	WinnerID  string           `json:"winner_id"`  // 获胜玩家 ID
	Scores    map[string]int32 `json:"scores"`     // Map<playerID, 击杀数/积分>
	EndedAt   int64            `json:"ended_at"`   // Unix 秒
}

func (GameResult) Type() string { return TypeGameResult }
