// Package config loads config.yaml (docs/spec/07-ops-admin.md §Config).
// Every magic number from the spec lives here; changing one requires a restart.
package config

import (
	"fmt"
	"os"
	"regexp"
	"time"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Server Server  `yaml:"server"`
	DB     DB      `yaml:"db"`
	Redis  Redis   `yaml:"redis"`
	Admin  []Admin `yaml:"admin"`
	Money  Money   `yaml:"money"`
	Loan   Loan    `yaml:"loan"`
	Game   GameCfg `yaml:"game"`
	Jobs   Jobs    `yaml:"jobs"`
	Log    Log     `yaml:"log"`
	Rate   Rate    `yaml:"rate"`
}

type Server struct {
	GameAddr       string   `yaml:"game_addr"`
	AdminAddr      string   `yaml:"admin_addr"`
	GameOrigin     string   `yaml:"game_origin"`
	AdminOrigin    string   `yaml:"admin_origin"`
	TrustedProxies []string `yaml:"trusted_proxies"`
}

type DB struct {
	DSN         string        `yaml:"dsn"`
	MaxConns    int32         `yaml:"max_conns"`
	MinConns    int32         `yaml:"min_conns"`
	ConnTimeout time.Duration `yaml:"conn_timeout"`
}

type Redis struct {
	Addr              string        `yaml:"addr"`
	DB                int           `yaml:"db"`
	SessionTTL        time.Duration `yaml:"session_ttl"`
	HeartbeatInterval time.Duration `yaml:"heartbeat_interval"`
}

type Admin struct {
	Username   string `yaml:"username"`
	BcryptHash string `yaml:"bcrypt_hash"`
}

type Money struct {
	SignupBonus        int64 `yaml:"signup_bonus"`
	TransferDailyLimit int64 `yaml:"transfer_daily_limit"`
	TransferMin        int64 `yaml:"transfer_min"`
}

type LoanTerm struct {
	Name     string        `yaml:"name"`
	Duration time.Duration `yaml:"duration"`
	RateBps  int           `yaml:"rate_bps"`
}

type Loan struct {
	Packages      []int64    `yaml:"packages"`
	Terms         []LoanTerm `yaml:"terms"`
	OverdueBps    int        `yaml:"overdue_bps"`
	CapMultiplier int64      `yaml:"cap_multiplier"`
}

type GameCfg struct {
	MinPlayers      int           `yaml:"min_players"`
	MaxPlayers      int           `yaml:"max_players"`
	HandSize        int           `yaml:"hand_size"`
	TurnTimeout     time.Duration `yaml:"turn_timeout"`
	ReadyTimeout    time.Duration `yaml:"ready_timeout"`
	ReconnectGrace  time.Duration `yaml:"reconnect_grace"`
	MaxMessageBytes int           `yaml:"max_message_bytes"`
	AllowedStakes   []int64       `yaml:"allowed_stakes"`
}

type Jobs struct {
	InvariantInterval      time.Duration `yaml:"invariant_interval"`
	LoanSeizeInterval      time.Duration `yaml:"loan_seize_interval"`
	HTTPLogRetention       time.Duration `yaml:"http_log_retention"`
	HTTPLogCleanupInterval time.Duration `yaml:"http_log_cleanup_interval"`
	StartDelay             time.Duration `yaml:"start_delay"`
}

type Log struct {
	Level  string `yaml:"level"`
	Format string `yaml:"format"`
}

// Rate holds the app-layer rate limits of docs/spec/04-http-api.md §Rate limit,
// expressed in requests per window.
type Rate struct {
	LoginPerIP          Window        `yaml:"login_per_ip"`
	LoginPerUsername    Window        `yaml:"login_per_username"`
	LoginLockDuration   time.Duration `yaml:"login_lock"`
	RegisterPerIP       Window        `yaml:"register_per_ip"`
	HeartbeatPerSession Window        `yaml:"heartbeat_per_session"`
	WSActionsPerUser    Window        `yaml:"ws_actions_per_user"`
	WSUnoPerUser        Window        `yaml:"ws_uno_per_user"`
	MatchStatePerUser   Window        `yaml:"match_state_per_user"`
	MoneyPerUser        Window        `yaml:"money_per_user"`
	CredentialPerUser   Window        `yaml:"credential_per_user"`
	ReadPerUser         Window        `yaml:"read_per_user"`
	RoomCreatePerUser   Window        `yaml:"room_create_per_user"`
	RoomJoinPerUser     Window        `yaml:"room_join_per_user"`
	AdminLoginPerIP     Window        `yaml:"admin_login_per_ip"`
	AdminPerAdmin       Window        `yaml:"admin_per_admin"`
	WSMessagesPerConn   Window        `yaml:"ws_messages_per_conn"`
}

type Window struct {
	Limit  int           `yaml:"limit"`
	Period time.Duration `yaml:"period"`
}

var stakePresets = []int64{10, 25, 50, 100, 500}

var adminHashRe = regexp.MustCompile(`^\$2[aby]\$`)

// Load reads path, expands ${ENV} references, then validates.
func Load(path string) (*Config, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var c Config
	if err := yaml.Unmarshal([]byte(ExpandEnv(string(raw))), &c); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}
	c.applyDefaults()
	if err := c.Validate(); err != nil {
		return nil, err
	}
	return &c, nil
}

func (c *Config) Validate() error {
	if c.Server.GameAddr == "" || c.Server.AdminAddr == "" {
		return fmt.Errorf("server.game_addr and server.admin_addr are required")
	}
	if c.DB.DSN == "" {
		return fmt.Errorf("db.dsn is required")
	}
	if c.Redis.Addr == "" {
		return fmt.Errorf("redis.addr is required")
	}
	for _, a := range c.Admin {
		if a.Username == "" || !adminHashRe.MatchString(a.BcryptHash) {
			return fmt.Errorf("admin %q: missing username or non-bcrypt hash", a.Username)
		}
	}
	presets := map[int64]bool{}
	for _, s := range stakePresets {
		presets[s] = true
	}
	for _, s := range c.Game.AllowedStakes {
		if !presets[s] {
			return fmt.Errorf("game.allowed_stakes %d is not a subset of {10,25,50,100,500}", s)
		}
	}
	if c.Game.MaxPlayers < 2 || c.Game.MaxPlayers > 10 {
		return fmt.Errorf("game.max_players must be in [2,10]")
	}
	if c.Game.MinPlayers < 2 || c.Game.MinPlayers > c.Game.MaxPlayers {
		return fmt.Errorf("game.min_players must be in [2,max_players]")
	}
	for _, p := range c.Loan.Packages {
		switch p {
		case 500, 1000, 2000:
		default:
			return fmt.Errorf("loan.packages %d is outside {500,1000,2000}", p)
		}
	}
	return nil
}

func (c *Config) applyDefaults() {
	if c.DB.MaxConns == 0 {
		c.DB.MaxConns = 20
	}
	if c.DB.MinConns == 0 {
		c.DB.MinConns = 5
	}
	if c.DB.ConnTimeout == 0 {
		c.DB.ConnTimeout = 5 * time.Second
	}
	if c.Redis.SessionTTL == 0 {
		c.Redis.SessionTTL = 15 * time.Minute
	}
	if c.Redis.HeartbeatInterval == 0 {
		c.Redis.HeartbeatInterval = time.Minute
	}
	if c.Game.HandSize == 0 {
		c.Game.HandSize = 7
	}
	if c.Game.TurnTimeout == 0 {
		c.Game.TurnTimeout = 20 * time.Second
	}
	if c.Game.ReadyTimeout == 0 {
		c.Game.ReadyTimeout = 5 * time.Second
	}
	if c.Game.ReconnectGrace == 0 {
		c.Game.ReconnectGrace = 90 * time.Second
	}
	if c.Game.MaxMessageBytes == 0 {
		c.Game.MaxMessageBytes = 4096
	}
	if c.Jobs.StartDelay == 0 {
		c.Jobs.StartDelay = 30 * time.Second
	}
	if c.Log.Format == "" {
		c.Log.Format = "json"
	}
}

// StakeAllowed reports whether stake is accepted for a new room.
func (c *Config) StakeAllowed(stake int64) bool {
	for _, s := range c.Game.AllowedStakes {
		if s == stake {
			return true
		}
	}
	return false
}

// LoanTermByName returns the configured term, or false.
func (c *Config) LoanTermByName(name string) (LoanTerm, bool) {
	for _, t := range c.Loan.Terms {
		if t.Name == name {
			return t, true
		}
	}
	return LoanTerm{}, false
}

// PackageAllowed reports whether principal is a configured loan package.
func (c *Config) PackageAllowed(principal int64) bool {
	for _, p := range c.Loan.Packages {
		if p == principal {
			return true
		}
	}
	return false
}
