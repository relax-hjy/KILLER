package analysis

import (
	"testing"

	"killer/internal/model"
)

// genKlines 生成 n 根合成日K：每天涨/跌 pct%，成交量按 volStep 增长。
func genKlines(n int, pct float64, vol0, volStep float64) []model.Kline {
	out := make([]model.Kline, 0, n)
	price := 100.0
	vol := vol0
	for i := 0; i < n; i++ {
		open := price
		price *= 1 + pct/100
		high := open * 1.005
		low := open * 0.995
		if price > high {
			high = price * 1.001
		}
		if price < low {
			low = price * 0.999
		}
		out = append(out, model.Kline{
			Date:   "2026-01-01",
			Open:   open,
			Close:  price,
			High:   high,
			Low:    low,
			Volume: vol,
		})
		vol *= 1 + volStep/100
	}
	return out
}

// TestAssessUptrend 强势上升趋势应给出买入方向的结论。
func TestAssessUptrend(t *testing.T) {
	klines := genKlines(120, 1.0, 1e6, 5) // 每日 +1%，量每日 +5%
	a, err := Assess("1.688981", "测试股", klines, 0, 0)
	if err != nil {
		t.Fatalf("Assess 失败: %v", err)
	}
	if len(a.Findings) != len(analyzers) {
		t.Fatalf("应返回 %d 个分析结论，实际 %d", len(analyzers), len(a.Findings))
	}
	if a.Score <= 60 {
		t.Errorf("上升趋势综合评分应 >60，实际 %.1f（advice=%s）", a.Score, a.Advice)
	}
	trend := a.Findings[0]
	if trend.Analyzer != NameTrend || trend.Score < 80 {
		t.Errorf("趋势分析应强偏多，实际 score=%.1f reason=%s", trend.Score, trend.Reason)
	}
	if a.Support <= 0 || a.Resistance <= 0 || a.Support > a.Resistance {
		t.Errorf("支撑/压力异常: support=%.2f resistance=%.2f", a.Support, a.Resistance)
	}
	if a.StopLoss <= 0 || a.StopLoss >= a.Price {
		t.Errorf("止损位应为正且低于现价: stop=%.2f price=%.2f", a.StopLoss, a.Price)
	}
}

// TestAssessDowntrend 强势下跌趋势应回避。
func TestAssessDowntrend(t *testing.T) {
	klines := genKlines(120, -1.5, 1e6, 6) // 每日 -1.5%，放量下跌
	a, err := Assess("0.002371", "测试股", klines, 0, 0)
	if err != nil {
		t.Fatalf("Assess 失败: %v", err)
	}
	if a.Score >= 45 {
		t.Errorf("下跌趋势综合评分应 <45，实际 %.1f（advice=%s）", a.Score, a.Advice)
	}
	if a.Advice != "回避" && a.Advice != "偏弱，暂缓买入" {
		t.Errorf("下跌趋势建议应为回避/暂缓，实际 %q", a.Advice)
	}
	if a.Findings[0].Score >= 0 {
		t.Errorf("趋势分析应偏空，实际 %.1f", a.Findings[0].Score)
	}
}

// TestAssessLevels 支撑=近20日最低，压力=近60日最高。
func TestAssessLevels(t *testing.T) {
	klines := genKlines(80, 0.5, 1e6, 0)
	a, err := Assess("1.600000", "测试股", klines, 0, 0)
	if err != nil {
		t.Fatalf("Assess 失败: %v", err)
	}
	// 上升趋势：支撑=第61根起（近20根）的最低 Low，压力=全部最高 High
	var lo20, hi60 float64 = 1e18, 0
	for i := 60; i < 80; i++ {
		if klines[i].Low < lo20 {
			lo20 = klines[i].Low
		}
	}
	for _, k := range klines {
		if k.High > hi60 {
			hi60 = k.High
		}
	}
	if a.Support != round2(lo20) {
		t.Errorf("支撑位应为近20日最低 %.2f，实际 %.2f", lo20, a.Support)
	}
	if a.Resistance != round2(hi60) {
		t.Errorf("压力位应为近60日最高 %.2f，实际 %.2f", hi60, a.Resistance)
	}
}

// TestAssessInsufficient 数据不足应报错。
func TestAssessInsufficient(t *testing.T) {
	if _, err := Assess("1.600000", "x", genKlines(20, 1, 1e6, 0), 0, 0); err == nil {
		t.Error("20 根日K应报数据不足")
	}
}

// TestSupportBreakdown 现价跌破 20 日支撑时支撑分析应判破位（偏空），而非贴近支撑加分。
func TestSupportBreakdown(t *testing.T) {
	klines := genKlines(80, 0.5, 1e6, 0) // 上升趋势，支撑=第60根附近低点
	var lo20 float64 = 1e18
	for i := 60; i < 80; i++ {
		if klines[i].Low < lo20 {
			lo20 = klines[i].Low
		}
	}
	lv := calcLevels(klines, klines[79].Close)
	f := runSupport(klines, lo20*0.95, lv) // 现价低于支撑 5%
	if f.Score >= 0 {
		t.Errorf("跌破支撑应偏空，实际 score=%.1f reason=%s", f.Score, f.Reason)
	}
}

// TestStopLossClamp 止损不深于现价 -8%。
func TestStopLossClamp(t *testing.T) {
	lv := levels{support: 10, atr: 5} // 支撑-ATR = 5，远深于 88
	if sl := stopLossOf(100, lv); sl != 92 {
		t.Errorf("止损应被钳制在 92（-8%%），实际 %.2f", sl)
	}
	lv = levels{support: 95, atr: 1} // 94 > 92，用支撑-ATR
	if sl := stopLossOf(100, lv); sl != 94 {
		t.Errorf("止损应为支撑-ATR=94，实际 %.2f", sl)
	}
}

// TestAdviceBuckets 评分区间与建议映射。
func TestAdviceBuckets(t *testing.T) {
	cases := map[float64]string{
		85: "可积极买入",
		70: "可以买入",
		55: "观望，回踩支撑再介入",
		40: "偏弱，暂缓买入",
		20: "回避",
	}
	for score, want := range cases {
		if got := adviceOf(score); got != want {
			t.Errorf("adviceOf(%.0f)=%q，期望 %q", score, got, want)
		}
	}
}
