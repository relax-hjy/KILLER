// Package config 负责加载 config.yaml 全局配置。
package config

import (
	"fmt"
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

// Index 定义一个被监控的标的（大盘指数或板块）。
type Index struct {
	SecID string `yaml:"secid"` // 东方财富 secid：1.xxxxxx=上交所 0.xxxxxx=深交所 90.BKxxxx=板块
	Name  string `yaml:"name"`  // 显示名称
}

// AlertConfig 告警阈值配置。
type AlertConfig struct {
	ChangePct      float64 `yaml:"change_pct"`       // 指数涨跌幅阈值（%）
	AmplitudePct   float64 `yaml:"amplitude_pct"`    // 当日振幅阈值（%）
	BoardChangePct float64 `yaml:"board_change_pct"` // 板块涨跌幅阈值（%），<=0 时沿用 change_pct
	CooldownMin    int     `yaml:"cooldown_minutes"` // 同类告警冷却（分钟）
}

// MonitoringConfig 监控配置。
type MonitoringConfig struct {
	CheckIntervalSec int         `yaml:"check_interval_seconds"`
	TradingHoursOnly bool        `yaml:"trading_hours_only"`
	Alert            AlertConfig `yaml:"alert"`
}

// Config 是顶层配置。
type Config struct {
	Server struct {
		Addr string `yaml:"addr"`
	} `yaml:"server"`
	Datasource struct {
		Primary  string `yaml:"primary"`
		Fallback string `yaml:"fallback"`
	} `yaml:"datasource"`
	Database struct {
		Path string `yaml:"path"`
	} `yaml:"database"`
	Indices    []Index          `yaml:"indices"`
	Boards     []Index          `yaml:"boards"`
	Monitoring MonitoringConfig `yaml:"monitoring"`
	Logging    struct {
		Level string `yaml:"level"`
	} `yaml:"logging"`
}

// Default 返回内置默认配置（config.yaml 缺失时兜底）。
func Default() *Config {
	c := &Config{}
	c.Server.Addr = ":8080"
	c.Datasource.Primary = "eastmoney"
	c.Datasource.Fallback = "sina"
	c.Database.Path = "market.db"
	c.Indices = []Index{
		{SecID: "1.000001", Name: "上证指数"},
		{SecID: "0.399001", Name: "深证成指"},
		{SecID: "0.399006", Name: "创业板指"},
	}
	c.Boards = []Index{
		{SecID: "90.BK1128", Name: "CPO概念"},
		{SecID: "90.BK0877", Name: "PCB"},
		{SecID: "90.BK1137", Name: "存储芯片"},
		{SecID: "90.BK1134", Name: "算力概念"},
		{SecID: "90.BK1326", Name: "半导体设备"},
	}
	c.Monitoring = MonitoringConfig{
		CheckIntervalSec: 60,
		TradingHoursOnly: true,
		Alert:            AlertConfig{ChangePct: 1.0, AmplitudePct: 1.5, BoardChangePct: 2.0, CooldownMin: 30},
	}
	c.Logging.Level = "INFO"
	return c
}

// Load 读取 YAML 配置文件；path 不存在时返回默认配置。
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return Default(), nil
		}
		return nil, fmt.Errorf("读取配置失败: %w", err)
	}
	c := Default() // 以默认值为底，YAML 覆盖
	if err := yaml.Unmarshal(data, c); err != nil {
		return nil, fmt.Errorf("解析配置失败: %w", err)
	}
	return c, nil
}

// CheckInterval 返回轮询间隔。
func (c *Config) CheckInterval() time.Duration {
	sec := c.Monitoring.CheckIntervalSec
	if sec <= 0 {
		sec = 60
	}
	return time.Duration(sec) * time.Second
}

// Cooldown 返回告警冷却时长。
func (c *Config) Cooldown() time.Duration {
	m := c.Monitoring.Alert.CooldownMin
	if m <= 0 {
		m = 30
	}
	return time.Duration(m) * time.Minute
}
