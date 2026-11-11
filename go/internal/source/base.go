// Package source 实现大盘行情数据源。
//
// 主源 eastmoney（push2 接口，实测返回干净 JSON），
// 备源 sina（hq.sinajs.cn，GBK 编码，主源失败时自动切换）。
package source

import (
	"context"
	"fmt"

	"killer/internal/model"
)

// Source 行情数据源接口。
type Source interface {
	Name() string
	// FetchQuotes 批量获取指数实时行情。
	FetchQuotes(ctx context.Context, secids []string) ([]model.Quote, error)
	// FetchKline 获取日 K 线（limit 根，倒序返回）。
	FetchKline(ctx context.Context, secid string, limit int) ([]model.Kline, error)
	// FetchMinuteKline 获取最新交易日的当日分时（1 分钟线，升序）。
	// 数据源不支持时返回错误。
	FetchMinuteKline(ctx context.Context, secid string) ([]model.Kline, error)
}

// New 按名称构造数据源。
func New(name string) (Source, error) {
	switch name {
	case "eastmoney":
		return &EastmoneySource{}, nil
	case "sina":
		return &SinaSource{}, nil
	default:
		return nil, fmt.Errorf("未知数据源: %s", name)
	}
}
