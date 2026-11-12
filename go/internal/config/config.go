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
	SecID string `yaml:"secid"` // secid：1.xxxxxx=上交所 0.xxxxxx=深交所 pt0xxx=腾讯板块
	Name  string `yaml:"name"`  // 显示名称
}

// ETFGroup 市场热门 ETF 组合（按类别分组展示，不参与告警巡检）。
// 与 etfs（自监控固定列表，带告警）的区别：纯看盘，点击卡片可看详情；
// 同一只 ETF 可同时出现在 etfs 自监控列表与某 etf_groups 组合中。
type ETFGroup struct {
	Name string  `yaml:"name"` // 组名（如 "宽基指数"/"行业主题"/"跨境QDII"）
	Desc string  `yaml:"desc"` // 组描述（一句话说明，前端展示在组标题旁）
	ETFs []Index `yaml:"etfs"` // 该组 ETF 列表（secid + 显示名）
}

// AlertConfig 告警阈值配置。
type AlertConfig struct {
	ChangePct      float64 `yaml:"change_pct"`       // 指数涨跌幅阈值（%）
	AmplitudePct   float64 `yaml:"amplitude_pct"`    // 当日振幅阈值（%）
	BoardChangePct float64 `yaml:"board_change_pct"` // 板块涨跌幅阈值（%），<=0 时沿用 change_pct
	EtfChangePct   float64 `yaml:"etf_change_pct"`   // ETF 涨跌幅阈值（%），<=0 时沿用 change_pct
	CooldownMin    int     `yaml:"cooldown_minutes"` // 同类告警冷却（分钟）
}

// MonitoringConfig 监控配置。
type MonitoringConfig struct {
	CheckIntervalSec int         `yaml:"check_interval_seconds"`
	TradingHoursOnly bool        `yaml:"trading_hours_only"`
	Alert            AlertConfig `yaml:"alert"`
}

// TradingConfig 模拟交易系统配置。
type TradingConfig struct {
	DataDir      string  `yaml:"data_dir"`      // 交易数据持久化目录（默认 D:\CloudDriver\EVO\killer）
	InitialCash  float64 `yaml:"initial_cash"`  // 初始资金
	CommissionRate float64 `yaml:"commission_rate"` // 佣金费率（双边，如 0.00025）
	MinCommission float64 `yaml:"min_commission"`  // 最低佣金（元）
	StampTaxRate  float64 `yaml:"stamp_tax_rate"`  // 印花税（仅卖出，如 0.0005）
	Enabled      bool    `yaml:"enabled"`        // 是否启用交易系统
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
	ETFs       []Index          `yaml:"etfs"`
	ETFGroups  []ETFGroup       `yaml:"etf_groups"` // 市场热门 ETF 组合（分类展示，不告警）
	Globals    []Index          `yaml:"globals"`
	Monitoring MonitoringConfig `yaml:"monitoring"`
	Trading    TradingConfig    `yaml:"trading"`
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
		{SecID: "pt02GN2211", Name: "CPO概念"},
		{SecID: "pt02101284", Name: "PCB"},
		{SecID: "pt02GN2124", Name: "存储芯片"},
		{SecID: "pt02GN2006", Name: "算力概念"},
		{SecID: "pt02GN2328", Name: "半导体设备"},
	}
	c.ETFs = []Index{
		{SecID: "1.510300", Name: "沪深300ETF"},
		{SecID: "0.159915", Name: "创业板ETF"},
		{SecID: "1.512880", Name: "证券ETF"},
		{SecID: "1.510050", Name: "上证50ETF"},
		{SecID: "1.588000", Name: "科创50ETF"},
		{SecID: "1.510500", Name: "中证500ETF"},
		{SecID: "0.159901", Name: "深100ETF"},
		{SecID: "1.512000", Name: "券商ETF"},
	}
	c.ETFGroups = []ETFGroup{
		{Name: "宽基指数", Desc: "覆盖全市场的规模指数 ETF", ETFs: []Index{
			{SecID: "1.510300", Name: "沪深300ETF"},
			{SecID: "1.510500", Name: "中证500ETF"},
			{SecID: "0.159915", Name: "创业板ETF"},
			{SecID: "1.588000", Name: "科创50ETF"},
			{SecID: "1.510050", Name: "上证50ETF"},
			{SecID: "1.563000", Name: "中证A50ETF"},
			{SecID: "1.512100", Name: "中证1000ETF"},
			{SecID: "0.159901", Name: "深100ETF"},
		}},
		{Name: "行业主题", Desc: "聚焦细分行业或主题的 ETF", ETFs: []Index{
			{SecID: "1.512480", Name: "半导体ETF"},
			{SecID: "1.512760", Name: "芯片ETF"},
			{SecID: "1.512880", Name: "证券ETF"},
			{SecID: "1.512800", Name: "银行ETF"},
			{SecID: "1.512010", Name: "医药ETF"},
			{SecID: "0.159928", Name: "消费ETF"},
			{SecID: "1.515030", Name: "新能源车ETF"},
			{SecID: "1.515790", Name: "光伏ETF"},
			{SecID: "1.512660", Name: "军工ETF"},
			{SecID: "1.512690", Name: "酒ETF"},
		}},
		{Name: "跨境QDII", Desc: "投资海外市场的跨境 ETF", ETFs: []Index{
			{SecID: "1.513100", Name: "纳指ETF"},
			{SecID: "1.513500", Name: "标普500ETF"},
			{SecID: "1.513130", Name: "恒生科技ETF"},
			{SecID: "1.510900", Name: "H股ETF"},
			{SecID: "1.513520", Name: "日经ETF"},
			{SecID: "1.513080", Name: "法国ETF"},
			{SecID: "1.513030", Name: "德国ETF"},
		}},
		{Name: "商品", Desc: "跟踪大宗商品价格的 ETF", ETFs: []Index{
			{SecID: "1.518880", Name: "黄金ETF"},
			{SecID: "0.159985", Name: "豆粕ETF"},
		}},
		{Name: "债券", Desc: "跟踪债券指数的固定收益 ETF", ETFs: []Index{
			{SecID: "1.511010", Name: "国债ETF"},
			{SecID: "1.511260", Name: "十年国债ETF"},
			{SecID: "1.511220", Name: "城投债ETF"},
			{SecID: "1.511380", Name: "可转债ETF"},
		}},
	}
	c.Globals = []Index{
		{SecID: "hf_CL", Name: "石油（WTI原油）"},
		{SecID: "znb_NKY", Name: "日经225"},
		{SecID: "znb_KOSPI", Name: "韩国综指"},
		{SecID: "hf_GC", Name: "黄金（纽约金）"},
		{SecID: "znb_NDX", Name: "纳斯达克100"},
		{SecID: "hkHSI", Name: "恒生指数"},
		{SecID: "hkHSTECH", Name: "恒生科技"},
	}
	c.Monitoring = MonitoringConfig{
		CheckIntervalSec: 60,
		TradingHoursOnly: true,
		Alert:            AlertConfig{ChangePct: 1.0, AmplitudePct: 1.5, BoardChangePct: 2.0, EtfChangePct: 2.0, CooldownMin: 30},
	}
	c.Trading = TradingConfig{
		DataDir:        `D:\CloudDriver\EVO\killer`,
		InitialCash:    1000000,
		CommissionRate: 0.00025,
		MinCommission:  5,
		StampTaxRate:   0.0005,
		Enabled:        true,
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
