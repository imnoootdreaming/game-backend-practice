package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// writeConfig 是测试辅助函数：在临时目录里写一份配置文件，测试结束自动清理，不污染项目目录
func writeConfig(t *testing.T, content string) string {
	t.Helper() // 报错时定位到调用它的测试函数，而不是这一行
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return path
}

// TestLoadFullConfig 正常路径：配置文件写全了，每个字段都应该被正确读出来
func TestLoadFullConfig(t *testing.T) {
	path := writeConfig(t, `
server:
  addr: ":9090"
  read_header_timeout: 3s
  shutdown_timeout: 7s
log:
  level: "debug"
game:
  tick_hz: 30
  team_size: 10
`)

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Server.Addr != ":9090" {
		t.Errorf("Server.Addr = %q, want :9090", cfg.Server.Addr)
	}
	if cfg.Server.ReadHeaderTimeout != 3*time.Second {
		t.Errorf("ReadHeaderTimeout = %v, want 3s", cfg.Server.ReadHeaderTimeout)
	}
	if cfg.Server.ShutdownTimeout != 7*time.Second {
		t.Errorf("ShutdownTimeout = %v, want 7s", cfg.Server.ShutdownTimeout)
	}
	if cfg.Log.Level != "debug" {
		t.Errorf("Log.Level = %q, want debug", cfg.Log.Level)
	}
	if cfg.Game.TickHz != 30 {
		t.Errorf("Game.TickHz = %d, want 30", cfg.Game.TickHz)
	}
	if cfg.Game.TeamSize != 10 {
		t.Errorf("Game.TeamSize = %d, want 10", cfg.Game.TeamSize)
	}
}

// TestLoadAppliesDefaultsForMissingFields 边界值：配置只写了一个字段，其余应该用默认值补齐
func TestLoadAppliesDefaultsForMissingFields(t *testing.T) {
	path := writeConfig(t, "game:\n  tick_hz: 15\n")

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Server.Addr != ":8080" {
		t.Errorf("Server.Addr = %q, want :8080", cfg.Server.Addr)
	}
	if cfg.Server.ReadHeaderTimeout != 5*time.Second {
		t.Errorf("ReadHeaderTimeout = %v, want 5s", cfg.Server.ReadHeaderTimeout)
	}
	if cfg.Server.ShutdownTimeout != 5*time.Second {
		t.Errorf("ShutdownTimeout = %v, want 5s", cfg.Server.ShutdownTimeout)
	}
	if cfg.Log.Level != "info" {
		t.Errorf("Log.Level = %q, want info", cfg.Log.Level)
	}
	if cfg.Game.TickHz != 15 {
		t.Errorf("Game.TickHz = %d, want 15", cfg.Game.TickHz)
	}
	if cfg.Game.TeamSize != 4 {
		t.Errorf("Game.TeamSize = %d, want 4", cfg.Game.TeamSize)
	}
}

// TestLoadErrors 错误路径：缺文件、YAML 语法错、配置值非法，都必须报错而不是悄悄用错误的值
func TestLoadErrors(t *testing.T) {
	t.Run("missing file", func(t *testing.T) {
		if _, err := Load(filepath.Join(t.TempDir(), "missing.yaml")); err == nil {
			t.Fatal("Load() error = nil, want error")
		}
	})

	t.Run("invalid yaml", func(t *testing.T) {
		path := writeConfig(t, "server: [unterminated\n")
		if _, err := Load(path); err == nil {
			t.Fatal("Load() error = nil, want error")
		}
	})

	// 表驱动测试：一行一个非法场景，统一断言错误信息里包含预期的字段名
	tests := []struct {
		name    string
		yaml    string
		wantErr string
	}{
		{"empty addr", "server:\n  addr: \"\"\n", "server.addr"},
		{"tick zero", "game:\n  tick_hz: 0\n", "game.tick_hz"},
		{"team size negative", "game:\n  team_size: -1\n", "game.team_size"},
		{"bad log level", "log:\n  level: verbose\n", "log.level"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := writeConfig(t, tt.yaml)
			_, err := Load(path)
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("Load() error = %v, want containing %q", err, tt.wantErr)
			}
		})
	}
}
