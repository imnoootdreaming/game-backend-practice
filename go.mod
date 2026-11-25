// 模块声明：Go 用这个名字作为本项目的唯一标识，其他包引用它时也要用这个名字
module game-backend-practice

// 项目要求的最低 Go 版本（本机 Go 1.27.4 满足）
go 1.27

// 依赖清单：yaml.v3 解析 configs/config.yaml；gorilla/websocket 支撑 1.4 WebSocket 接入层
require (
	github.com/gorilla/websocket v1.5.3
	gopkg.in/yaml.v3 v3.0.1
)
