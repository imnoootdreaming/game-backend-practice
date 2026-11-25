# .PHONY 声明"伪目标"：这些名字不代表实际文件，每次执行都会真正运行对应命令
.PHONY: run test tidy e2e

# 启动 gateway 服务（开发时最常用）
run:
	go run ./cmd/gateway

# 运行全部单元测试：TDD 的红绿循环靠它验证
test:
	go test ./...

# 整理依赖：下载缺失的模块、清除多余的模块，并更新 go.sum
tidy:
	go mod tidy

# 端到端测试：先跑单测（依赖 test 目标），目前到阶段 1.9 才会真正实现
e2e: test
	@echo "e2e: phase 1.9 实现后接入（4 客户端完整对局）"
