// Package model 定义系统核心数据结构。
package model

import (
	"strconv"
	"time"
)

// Quote 指数实时行情快照。
type Quote struct {
	SecID      string    `json:"secid"`
	Code       string    `json:"code"`
	Name       string    `json:"name"`
	Price      float64   `json:"price"`       // 最新点位
	Change     float64   `json:"change"`      // 涨跌点数
	Pct        float64   `json:"pct"`         // 涨跌幅（%）
	Open       float64   `json:"open"`
	High       float64   `json:"high"`
	Low        float64   `json:"low"`
	PrevClose  float64   `json:"prev_close"`
	Amplitude  float64   `json:"amplitude"`   // 当日振幅（%）
	Volume     float64   `json:"volume"`      // 成交额（元）
	UpdateTime time.Time `json:"update_time"`
}

// Kline 单根日 K。
type Kline struct {
	Date   string  `json:"date"`
	Open   float64 `json:"open"`
	Close  float64 `json:"close"`
	High   float64 `json:"high"`
	Low    float64 `json:"low"`
	Volume float64 `json:"volume"`
}

// Alert 一条告警记录。
type Alert struct {
	ID         int64     `json:"id"`
	SecID      string    `json:"secid"`
	Name       string    `json:"name"`
	Type       string    `json:"type"` // change_up | change_down | amplitude
	Value      float64   `json:"value"`      // 触发值（%）
	Threshold  float64   `json:"threshold"`  // 阈值（%）
	Price      float64   `json:"price"`      // 触发时点位
	TriggeredAt time.Time `json:"triggered_at"`
}

func (a Alert) String() string {
	return "[" + a.Type + "] " + a.Name +
		" 当前=" + strconv.FormatFloat(a.Value, 'f', 2, 64) + "%" +
		" 阈值=" + strconv.FormatFloat(a.Threshold, 'f', 2, 64) + "%"
}
