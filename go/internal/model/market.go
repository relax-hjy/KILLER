package model

import "time"

// Board A股板块（行业/概念）。
type Board struct {
	SecID     string  `json:"secid"` // 如 BK1295
	Name      string  `json:"name"`
	Price     float64 `json:"price"`      // 板块指数点位
	Pct       float64 `json:"pct"`        // 涨跌幅（%）
	Change    float64 `json:"change"`     // 涨跌点
	UpCount   int     `json:"up_count"`   // 板块内上涨家数
	DownCount int     `json:"down_count"` // 板块内下跌家数
}

// Stock 个股行情（A股/美股通用）。
type Stock struct {
	SecID      string    `json:"secid"` // "1.600519" / "105.AAPL"
	Code       string    `json:"code"`
	Name       string    `json:"name"`
	Price      float64   `json:"price"`
	Pct        float64   `json:"pct"`
	Change     float64   `json:"change"`
	Open       float64   `json:"open"`
	High       float64   `json:"high"`
	Low        float64   `json:"low"`
	PrevClose  float64   `json:"prev_close"`
	Amount     float64   `json:"amount"`     // 成交额（元）
	Turnover   float64   `json:"turnover"`   // 换手率（%）
	PE         float64   `json:"pe"`         // 市盈率
	PB         float64   `json:"pb"`         // 市净率
	MarketCap  float64   `json:"market_cap"` // 总市值（元）
	FloatCap   float64   `json:"float_cap"`  // 流通市值（元）
	UpdateTime time.Time `json:"update_time"`
}

// WatchStock 自选个股（带告警阈值）。
type WatchStock struct {
	SecID     string    `json:"secid"`
	Code      string    `json:"code"`
	Name      string    `json:"name"`
	Threshold float64   `json:"threshold"` // 日涨跌告警阈值（%）
	AddedAt   time.Time `json:"added_at"`
	Notes     string    `json:"notes"`
}
