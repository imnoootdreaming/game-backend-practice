// package config 负责把 YAML 配置文件读进程序，并保证配置值是合法的
package config

import (
	"errors"  // 构造简单的校验错误
	"fmt"     // 格式化带上下文的错误信息
	"os"      // 读取配置文件
	"strings" // 日志级别比较时忽略大小写
	"time"    // 超时字段的类型 time.Duration

	"gopkg.in/yaml.v3" // 第三方 YAML 解析库

	"game-backend-practice/internal/match" // 把配置转换成匹配器参数
)

// Config 是配置的根结构，对应 config.yaml 的 server / log / game / match / session 五块。
type Config struct {
	Server  ServerConfig  `yaml:"server"`  // 服务器监听与超时设置
	Log     LogConfig     `yaml:"log"`     // 日志设置
	Game    GameConfig    `yaml:"game"`    // 游戏逻辑参数
	Match   MatchConfig   `yaml:"match"`   // 匹配器参数
	Session SessionConfig `yaml:"session"` // token 签发与会话有效期
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

// MatchConfig 描述匹配器的扫描周期和段位放宽参数。
type MatchConfig struct {
	ScanInterval      time.Duration `yaml:"scan_interval"`       // 匹配队列的扫描周期
	BaseRankTolerance int           `yaml:"base_rank_tolerance"` // 初始段位容差
	RankWidenStep     int           `yaml:"rank_widen_step"`     // 每次放宽增加的容差
	RankWidenInterval time.Duration `yaml:"rank_widen_interval"` // 放宽一次的等待时长
	RoomSizes         []int         `yaml:"room_sizes"`          // 支持的房间人数档位
}

// MatcherConfig 把 YAML 中的匹配参数转换成 match 包需要的配置。
func (c Config) MatcherConfig() match.Config {
	return match.Config{
		TeamSize:          c.Game.TeamSize,
		BaseRankTolerance: c.Match.BaseRankTolerance,
		RankWidenStep:     c.Match.RankWidenStep,
		RankWidenInterval: c.Match.RankWidenInterval,
	}
}

// SessionConfig 描述登录 token 的 HMAC 密钥和有效期。
// 默认值只适合本地开发，部署时必须用配置或环境变量覆盖。
type SessionConfig struct {
	Secret   string        `yaml:"secret"`    // HMAC-SHA256 密钥
	TokenTTL time.Duration `yaml:"token_ttl"` // token 有效期
}

// Load 读取配置文件并做三层处理：
// 1. 读文件；2. 先填默认值，再用文件内容覆盖；3. 校验合法性
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
		Match: MatchConfig{
			ScanInterval:      500 * time.Millisecond,
			BaseRankTolerance: 100,
			RankWidenStep:     100,
			RankWidenInterval: 30 * time.Second,
			RoomSizes:         []int{2, 4, 10},
		},
		Session: SessionConfig{Secret: "dev-only-change-me", TokenTTL: 30 * time.Minute},
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
	if c.Match.ScanInterval <= 0 {
		return errors.New("match.scan_interval must be positive")
	}
	if c.Match.BaseRankTolerance < 0 {
		return errors.New("match.base_rank_tolerance must not be negative")
	}
	if c.Match.RankWidenStep < 0 {
		return errors.New("match.rank_widen_step must not be negative")
	}
	if c.Match.RankWidenInterval <= 0 {
		return errors.New("match.rank_widen_interval must be positive")
	}
	if len(c.Match.RoomSizes) == 0 {
		return errors.New("match.room_sizes must not be empty")
	}
	// 逐个检查人数档位，避免 0 或负数让匹配器找不到可用的房间规格。
	for _, size := range c.Match.RoomSizes {
		if size <= 0 {
			return errors.New("match.room_sizes must contain positive values")
		}
	}
	if c.Session.Secret == "" {
		return errors.New("session.secret must not be empty")
	}
	if c.Session.TokenTTL <= 0 {
		return errors.New("session.token_ttl must be positive")
	}
	return nil
}
