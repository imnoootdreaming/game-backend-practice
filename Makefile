# .PHONY 声明"伪目标"：这些名字不代表实际文件，每次执行都会真正运行对应命令
.PHONY: run test race tidy e2e

# 启动 gateway 服务（开发时最常用）
run:
	go run ./cmd/gateway

# 运行全部单元测试：TDD 的红绿循环靠它验证
test:
	go test ./...

# 竞态检测：Windows 下需 CGO_ENABLED=1 与 gcc；默认用父目录预置的 llvm-mingw，可用 CC= 覆盖
RACE_CC ?= ../.race-tools/llvm-mingw-20261117-ucrt-x86_64/bin/x86_64-w64-mingw32-gcc.exe
race: export CGO_ENABLED = 1
race: export CC = $(RACE_CC)
race:
	go test -race ./...

# 整理依赖：下载缺失的模块、清除多余的模块，并更新 go.sum
tidy:
	go mod tidy

# 端到端测试：先跑单测（依赖 test 目标），目前到阶段 1.9 才会真正实现
e2e: test
	@echo "e2e: phase 1.9 实现后接入（4 客户端完整对局）"
