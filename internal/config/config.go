// package config 负责把 YAML 配置文件读进程序，并保证配置值是合法的
package config

import (
	"errors"  // 构造简单的校验错误
	"fmt"     // 格式化带上下文的错误信息
	"os"      // 读取配置文件
	"strings" // 日志级别比较时忽略大小写
	"time"    // 超时字段的类型 time.Duration

	"gopkg.in/yaml.v3" // 第三方 YAML 解析库
)

// Config 是配置的根结构，对应 config.yaml 的三大块：server / log / game
type Config struct {
	Server ServerConfig `yaml:"server"` // 服务器监听与超时设置
	Log    LogConfig    `yaml:"log"`    // 日志设置
	Game   GameConfig   `yaml:"game"`   // 游戏逻辑参数
}

// ServerConfig 描述 HTTP 服务的监听地址和各种超时
type ServerConfig struct {
	Addr              string        `yaml:"addr"`                // 监听地址，如 ":8080"
	ReadHeaderTimeout time.Duration `yaml:"read_header_timeout"` // 读请求头的最长等待时间
	ShutdownTimeout   time.Duration `yaml:"shutdown_timeout"`    // 优雅关闭最长等待时间
}

// LogConfig 描述日志级别
type LogConfig struct {
	Level string `yaml:"level"` // 可选值：debug / info / warn / error
}

// GameConfig 描述对局相关的全局参数（后续阶段会用到）
type GameConfig struct {
	TickHz   int `yaml:"tick_hz"`   // 每秒游戏逻辑推进次数（20 = 20Hz）
	TeamSize int `yaml:"team_size"` // 匹配满员人数
}

// Load 读取配置文件并做三层处理：
// 1. 读文件 → 2. 先填默认值，再用文件内容覆盖 → 3. 校验合法性
// 任何一步失败都返回错误，调用方据此决定"拒绝启动"。
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config %q: %w", path, err)
	}

	cfg := defaults() // 先铺一层默认值，YAML 里没写的字段就保留默认值
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse config %q: %w", path, err)
	}
	if err := cfg.validate(); err != nil {
		return nil, fmt.Errorf("invalid config %q: %w", path, err)
	}
	return &cfg, nil
}

// defaults 给出所有配置的默认值，让配置文件可以只写想改的字段
func defaults() Config {
	return Config{
		Server: ServerConfig{
			Addr:              ":8080",
			ReadHeaderTimeout: 5 * time.Second,
			ShutdownTimeout:   5 * time.Second,
		},
		Log:  LogConfig{Level: "info"},
		Game: GameConfig{TickHz: 20, TeamSize: 4},
	}
}

// validate 校验配置值，防止"端口为空""tick 为 0"这类错误配置让服务跑起来后行为诡异
func (c *Config) validate() error {
	if c.Server.Addr == "" {
		return errors.New("server.addr must not be empty")
	}
	if c.Server.ReadHeaderTimeout <= 0 {
		return errors.New("server.read_header_timeout must be positive")
	}
	if c.Server.ShutdownTimeout <= 0 {
		return errors.New("server.shutdown_timeout must be positive")
	}
	// 日志级别只接受四种合法值，否则拒绝启动
	switch strings.ToLower(c.Log.Level) {
	case "debug", "info", "warn", "error":
	default:
		return fmt.Errorf("log.level %q must be one of debug/info/warn/error", c.Log.Level)
	}
	if c.Game.TickHz <= 0 {
		return errors.New("game.tick_hz must be positive")
	}
	if c.Game.TeamSize <= 0 {
		return errors.New("game.team_size must be positive")
	}
	return nil
}
