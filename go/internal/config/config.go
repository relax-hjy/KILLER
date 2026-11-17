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
	DataDir        string  `yaml:"data_dir"`        // 交易数据持久化目录（默认 D:\CloudDriver\EVO\killer）
	InitialCash    float64 `yaml:"initial_cash"`    // 初始资金
	CommissionRate float64 `yaml:"commission_rate"` // 佣金费率（双边，如 0.00025）
	MinCommission  float64 `yaml:"min_commission"`  // 最低佣金（元）
	StampTaxRate   float64 `yaml:"stamp_tax_rate"`  // 印花税（仅卖出，如 0.0005）
	Enabled        bool    `yaml:"enabled"`         // 是否启用交易系统
	// 交易指导的仓位约束（% 与股）：单票上限、总仓位上限、A股整手股数
	SinglePositionPct float64 `yaml:"single_position_pct"`
	MaxPositionPct    float64 `yaml:"max_position_pct"`
	LotSize           int64   `yaml:"lot_size"`
}

// AnalysisGroup 分析观察分组：按产业主题归类，分析页每个分组一个 tab。
type AnalysisGroup struct {
	Name      string  `yaml:"name"`
	From      string  `yaml:"from,omitempty"` // 引用其他配置段作为成员："etfs" = 顶部自监控 ETF，避免两处抄 secid
	Watchlist []Index `yaml:"watchlist"`      // 组内标的（未配置 From 时使用）
}

// GroupMembers 解析分析分组的实际成员：配置了 From 就引用对应配置段，否则用组内 watchlist。
// 引用段为空时回退 watchlist，避免整组静默变空。
func (c Config) GroupMembers(g AnalysisGroup) []Index {
	if g.From == "etfs" && len(c.ETFs) > 0 {
		return c.ETFs
	}
	return g.Watchlist
}

// AnalysisConfig 分析系统配置。
type AnalysisConfig struct {
	Groups []AnalysisGroup `yaml:"groups"` // 分析观察分组（每组的日报都单独存档）
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
	Analysis   AnalysisConfig   `yaml:"analysis"` // 分析系统（多维度研判 + 操作建议）
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
	// 与交易数据同放网盘同步目录：行情快照/告警/自选/分析日报都要跟着走，别散在启动目录
	c.Database.Path = `D:\CloudDriver\EVO\killer\market.db`
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
		DataDir:           `D:\CloudDriver\EVO\killer`,
		InitialCash:       20000,
		CommissionRate:    0.00025,
		MinCommission:     5,
		StampTaxRate:      0.0005,
		Enabled:           true,
		SinglePositionPct: 30,
		MaxPositionPct:    80,
		LotSize:           100,
	}
	c.Analysis = AnalysisConfig{Groups: []AnalysisGroup{
		{Name: "AI 算力/半导体", Watchlist: []Index{
			{SecID: "1.688981", Name: "中芯国际"}, // 半导体代工
			{SecID: "1.688256", Name: "寒武纪"},  // AI 芯片
			{SecID: "1.688041", Name: "海光信息"}, // CPU/DCU
			{SecID: "1.601138", Name: "工业富联"}, // AI 服务器代工
			{SecID: "0.002463", Name: "沪电股份"}, // PCB
			{SecID: "0.002475", Name: "立讯精密"}, // 消费电子
		}},
		{Name: "存储", Watchlist: []Index{
			{SecID: "1.688825", Name: "长鑫科技"}, // DRAM 原厂，2026-07-27 科创板上市
			{SecID: "1.603986", Name: "兆易创新"}, // NOR/利基 DRAM
			{SecID: "1.688008", Name: "澜起科技"}, // 内存接口芯片 RCD
			{SecID: "0.301308", Name: "江波龙"},  // 存储模组
			{SecID: "1.688525", Name: "佰维存储"}, // 存储模组
			{SecID: "0.001309", Name: "德明利"},  // 主控 + 模组
			{SecID: "1.688110", Name: "东芯股份"}, // SLC NAND
			{SecID: "1.688766", Name: "普冉股份"}, // NOR/EEPROM
			{SecID: "1.688123", Name: "聚辰股份"}, // EEPROM/SPD
			{SecID: "0.000021", Name: "深科技"},  // 存储封测
			{SecID: "0.300475", Name: "香农芯创"}, // 存储分销
		}},
		// 设备按前道工艺环节覆盖，材料按硅片→前驱体/特气→CMP 耗材→靶材→光刻胶排布
		{Name: "半导体设备/材料", Watchlist: []Index{
			{SecID: "0.002371", Name: "北方华创"}, // 刻蚀/薄膜/清洗/炉管 平台型
			{SecID: "1.688012", Name: "中微公司"}, // 刻蚀 + MOCVD
			{SecID: "1.688072", Name: "拓荆科技"}, // 薄膜沉积 PECVD/ALD
			{SecID: "1.688120", Name: "华海清科"}, // CMP 抛光机 + 减薄
			{SecID: "1.688082", Name: "盛美上海"}, // 单片清洗 + 电镀
			{SecID: "1.688037", Name: "芯源微"},  // 涂胶显影 Track
			{SecID: "1.688361", Name: "中科飞测"}, // 量检测
			{SecID: "1.688627", Name: "精智达"},  // 存储测试设备
			{SecID: "1.688126", Name: "沪硅产业"}, // 300mm 大硅片
			{SecID: "0.002409", Name: "雅克科技"}, // 前驱体 + 特气 + 光刻胶材料
			{SecID: "1.688019", Name: "安集科技"}, // CMP 抛光液 + 蚀刻液
			{SecID: "0.300054", Name: "鼎龙股份"}, // CMP 抛光垫
			{SecID: "0.300666", Name: "江丰电子"}, // 高纯溅射靶材
			{SecID: "0.300346", Name: "南大光电"}, // ArF 光刻胶 + MO 源
			{SecID: "1.688268", Name: "华特气体"}, // 电子特气
		}},
		// 白酒按高端 → 次高端 → 区域龙头排序
		{Name: "白酒", Watchlist: []Index{
			{SecID: "1.600519", Name: "贵州茅台"}, // 高端酱酒
			{SecID: "0.000858", Name: "五粮液"},  // 高端浓香
			{SecID: "0.000568", Name: "泸州老窖"}, // 高端浓香 国窖1573
			{SecID: "1.600809", Name: "山西汾酒"}, // 清香型，全国化最快
			{SecID: "0.002304", Name: "洋河股份"}, // 次高端绵柔
			{SecID: "0.000596", Name: "古井贡酒"}, // 安徽根据地
			{SecID: "1.603369", Name: "今世缘"},  // 江苏根据地
			{SecID: "1.600702", Name: "舍得酒业"}, // 老酒概念
			{SecID: "0.000799", Name: "酒鬼酒"},  // 馥郁香型
			{SecID: "1.600779", Name: "水井坊"},  // 高端川酒
			{SecID: "1.603198", Name: "迎驾贡酒"}, // 安徽次高端
			{SecID: "1.603919", Name: "金徽酒"},  // 西北区域
		}},
		// 人形机器人产业链：执行器/减速器/电机/丝杠/本体
		{Name: "机器人", Watchlist: []Index{
			{SecID: "0.002050", Name: "三花智控"}, // 执行器 + 热管理
			{SecID: "1.601689", Name: "拓普集团"}, // 执行器总成
			{SecID: "1.688017", Name: "绿的谐波"}, // 谐波减速器
			{SecID: "0.002472", Name: "双环传动"}, // RV 减速器齿轮
			{SecID: "0.002896", Name: "中大力德"}, // 行星减速器
			{SecID: "1.603728", Name: "鸣志电器"}, // 空心杯电机
			{SecID: "0.300124", Name: "汇川技术"}, // 伺服驱动
			{SecID: "0.002979", Name: "雷赛智能"}, // 运动控制
			{SecID: "0.003021", Name: "兆威机电"}, // 微型传动
			{SecID: "1.603009", Name: "北特科技"}, // 行星滚柱丝杠
			{SecID: "0.002747", Name: "埃斯顿"},  // 工业机器人本体
			{SecID: "0.300024", Name: "机器人"},  // 新松机器人
		}},
		// 商业航天：卫星制造/运营 → 相控阵部件 → 箭上配套
		{Name: "商业航天", Watchlist: []Index{
			{SecID: "1.600118", Name: "中国卫星"}, // 小卫星制造
			{SecID: "1.601698", Name: "中国卫通"}, // 卫星运营
			{SecID: "1.600879", Name: "航天电子"}, // 箭上电子/测控
			{SecID: "0.001270", Name: "铖昌科技"}, // 相控阵 T/R 芯片
			{SecID: "1.688375", Name: "国博电子"}, // T/R 组件
			{SecID: "0.301050", Name: "雷电微力"}, // 毫米波 T/R 组件
			{SecID: "0.300342", Name: "天银机电"}, // 星敏感器
			{SecID: "1.688523", Name: "航天环宇"}, // 卫星结构/地面
			{SecID: "1.688102", Name: "斯瑞新材"}, // 火箭推力室内壁
			{SecID: "1.688239", Name: "航宇科技"}, // 航发环锻件
		}},
		// CPO/光通信：光模块/光器件/光芯片/连接器，1.6T 与 CPO 共封装是主线
		{Name: "CPO/光通信", Watchlist: []Index{
			{SecID: "0.300308", Name: "中际旭创"}, // 光模块龙头
			{SecID: "0.300502", Name: "新易盛"},  // 光模块
			{SecID: "0.300394", Name: "天孚通信"}, // 光器件
			{SecID: "0.002281", Name: "光迅科技"}, // 光模块 + 光芯片
			{SecID: "1.688498", Name: "源杰科技"}, // 激光器芯片
			{SecID: "0.300570", Name: "太辰光"},  // MPO 连接器
			{SecID: "1.688313", Name: "仕佳光子"}, // AWG/PLC 分光
			{SecID: "1.603083", Name: "剑桥科技"}, // 光模块
			{SecID: "0.000988", Name: "华工科技"}, // 激光 + 光模块
			{SecID: "1.688205", Name: "德科立"},  // 光传输子系统
		}},
		// 创新药：出海授权与重磅管线是估值主线
		{Name: "创新药", Watchlist: []Index{
			{SecID: "1.600276", Name: "恒瑞医药"}, // 创新药龙头
			{SecID: "1.688235", Name: "百济神州"}, // 泽布替尼出海
			{SecID: "1.688180", Name: "君实生物"}, // PD-1/ADC
			{SecID: "0.002422", Name: "科伦药业"}, // 科伦博泰 ADC 出海
			{SecID: "1.688331", Name: "荣昌生物"}, // ADC/自免
			{SecID: "0.300558", Name: "贝达药业"}, // 肿瘤小分子
			{SecID: "1.688266", Name: "泽璟制药"}, // 泛肿瘤小分子
			{SecID: "1.688506", Name: "百利天恒"}, // ADC 双抗
			{SecID: "1.688578", Name: "艾力斯"},  // 三代 EGFR
			{SecID: "0.002294", Name: "信立泰"},  // 慢病 + 创新转型
		}},
		// 银行：国有大行 → 股份行 → 城商行，高股息主线
		{Name: "银行", Watchlist: []Index{
			{SecID: "1.601398", Name: "工商银行"},
			{SecID: "1.601939", Name: "建设银行"},
			{SecID: "1.601288", Name: "农业银行"},
			{SecID: "1.601988", Name: "中国银行"},
			{SecID: "1.601328", Name: "交通银行"},
			{SecID: "1.601658", Name: "邮储银行"},
			{SecID: "1.600036", Name: "招商银行"},
			{SecID: "1.601166", Name: "兴业银行"},
			{SecID: "1.600000", Name: "浦发银行"},
			{SecID: "1.601998", Name: "中信银行"},
			{SecID: "1.600919", Name: "江苏银行"},
			{SecID: "0.002142", Name: "宁波银行"},
		}},
		// ETF 组成员直接引用顶部 etfs 自监控列表，不在分析配置里重复抄 secid
		{Name: "ETF", From: "etfs"},
	}}
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
