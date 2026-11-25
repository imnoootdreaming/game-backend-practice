# game-backend-practice

按《游戏后台开发实践计划》实现的小型多人竞技后台模拟器，依次覆盖登录、大厅、匹配、房间、权威战斗、同步、服务化七个环节。当前处于**阶段 1**（最小可运行版本）。

## 环境要求

- Go 1.27（与 `go.mod` 一致）
- Git

## 快速开始

```bash
go mod tidy          # 首次运行：下载依赖并生成 go.sum
make test            # 运行全部单元测试
make run             # 启动 gateway，默认读取 configs/config.yaml
curl localhost:8080/healthz
```

也可指定配置文件：

```bash
go run ./cmd/gateway -config configs/config.yaml
```

## 配置

`configs/config.yaml` 中的关键参数：

| 字段 | 含义 | 默认值 |
|---|---|---|
| `server.addr` | HTTP 监听地址 | `:8080` |
| `server.read_header_timeout` | 请求头读取超时 | `5s` |
| `server.shutdown_timeout` | 优雅关闭最长等待 | `5s` |
| `log.level` | 日志级别（debug/info/warn/error） | `info` |
| `game.tick_hz` | 战斗逻辑每秒 tick 次数 | `20` |
| `game.team_size` | 匹配满员人数 | `4` |

缺失字段自动使用默认值；字段非法时服务拒绝启动（配置加载层已有单元测试覆盖）。

## 目录结构

```text
cmd/gateway/      单体入口（阶段 1）；阶段 3 拆出多个服务
internal/config/  YAML 配置加载与校验
configs/          配置文件
```

完整目标目录见实践计划文档。

## 开发约定

- **TDD**：每个任务依次经过写失败测试（Red）、最小实现（Green）、重构三个阶段；`make test` 必须通过再进入下一任务。
- 阶段完成打 tag：`phase-1` / `phase-2` / `phase-3`。
- 协议先 JSON 后 Protobuf；先单机闭环后分布式。
