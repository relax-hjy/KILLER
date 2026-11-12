package trading

import (
	"testing"

	"killer/internal/config"
	"killer/internal/model"
)

func TestFee(t *testing.T) {
	cfg := config.Default()
	cfg.Trading.CommissionRate = 0.00025
	cfg.Trading.MinCommission = 5
	cfg.Trading.StampTaxRate = 0.0005
	e := New(cfg, nil)

	// 买入：仅佣金（万2.5）
	_, _, total := e.fee("buy", 100000)
	want := 100000 * 0.00025 // 25 元佣金
	if total != want {
		t.Errorf("买入手续费=%v want %v", total, want)
	}

	// 卖出：佣金 + 印花税
	_, _, total = e.fee("sell", 100000)
	want = 100000*0.00025 + 100000*0.0005 // 25 + 50 = 75
	if total != want {
		t.Errorf("卖出手续费=%v want %v", total, want)
	}

	// 最低佣金：小额成交按 5 元
	commission, _, _ := e.fee("buy", 1000)
	if commission != 5 {
		t.Errorf("最低佣金=%v want 5", commission)
	}
}

func TestStrategySignals(t *testing.T) {
	// 均线金叉：短均线上穿长均线
	up := []float64{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}
	action, _, _ := maCross(up, 2, 5)
	if action != "buy" && action != "hold" {
		t.Errorf("上升趋势应偏多，got %s", action)
	}

	// RSI 超卖/超买
	oversold := make([]float64, 20)
	for i := range oversold {
		oversold[i] = 100 - float64(i) // 持续下跌
	}
	action, _, _ = rsiSignal(oversold, 14)
	if action != "buy" {
		t.Errorf("持续下跌 RSI 应超卖→buy, got %s", action)
	}

	overbought := make([]float64, 20)
	for i := range overbought {
		overbought[i] = float64(i) // 持续上涨
	}
	action, _, _ = rsiSignal(overbought, 14)
	if action != "sell" {
		t.Errorf("持续上涨 RSI 应超买→sell, got %s", action)
	}
}

func TestBreakout(t *testing.T) {
	// 前 20 天震荡，最后一天突破新高
	closes := make([]float64, 21)
	for i := 0; i < 20; i++ {
		closes[i] = 10 + float64(i%5) // 10-14 震荡
	}
	closes[20] = 20 // 突破
	action, _, _ := breakout(closes, 20)
	if action != "buy" {
		t.Errorf("突破新高应→buy, got %s", action)
	}
}

func TestSharpe(t *testing.T) {
	// 单调上升权益曲线 → 正夏普
	curve := make([]model.EquityPoint, 0, 100)
	for i := 0; i < 100; i++ {
		curve = append(curve, model.EquityPoint{Date: "d", Asset: float64(1000 + i*10)})
	}
	sharpe := calcSharpe(curve)
	if sharpe <= 0 {
		t.Errorf("单调上升曲线夏普应>0, got %v", sharpe)
	}
}
