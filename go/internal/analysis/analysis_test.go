package analysis

import (
	"strings"
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
	// 120 根样本没有年线/周线项，趋势满额是 75（30+18+17+10）
	if trend.Analyzer != NameTrend || trend.Score < 70 {
		t.Errorf("趋势分析应强偏多，实际 score=%.1f reason=%s", trend.Score, trend.Reason)
	}
	// 回归：样本不足 250 根时年线项必须缺席（不能把全量均值当年线扣分）
	if !strings.Contains(trend.Reason, "年线缺失") {
		t.Errorf("样本不足应标注年线缺失，实际 reason=%s", trend.Reason)
	}
	if a.Confidence >= 100 || a.Confidence < 60 {
		t.Errorf("120 根样本的置信度应在 60~100，实际 %.1f", a.Confidence)
	}
	if a.Atr <= 0 {
		t.Errorf("应返回 ATR 供按买入价重算止损，实际 %.2f", a.Atr)
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

// TestAssessLevels 支撑=近20日最低，压力=近120日最高。
func TestAssessLevels(t *testing.T) {
	klines := genKlines(80, 0.5, 1e6, 0)
	a, err := Assess("1.600000", "测试股", klines, 0, 0)
	if err != nil {
		t.Fatalf("Assess 失败: %v", err)
	}
	// 上升趋势：支撑=第61根起（近20根）的最低 Low，压力=近120根（本例即全部80根）最高 High
	var lo20, hi120 float64 = 1e18, 0
	for i := 60; i < 80; i++ {
		if klines[i].Low < lo20 {
			lo20 = klines[i].Low
		}
	}
	for _, k := range klines {
		if k.High > hi120 {
			hi120 = k.High
		}
	}
	if a.Support != round2(lo20) {
		t.Errorf("支撑位应为近20日最低 %.2f，实际 %.2f", lo20, a.Support)
	}
	if a.Resistance != round2(hi120) {
		t.Errorf("压力位应为近120日最高 %.2f，实际 %.2f", hi120, a.Resistance)
	}
}

// TestResistance120Day 压力位取近120日最高（而非60日）：仅前段冲高、近60日回落时仍应取前段高点。
func TestResistance120Day(t *testing.T) {
	kl := make([]model.Kline, 150)
	for i := range kl {
		high := 10.0
		if i < 50 {
			high = 20.0 // 第1~50根冲高，距今100~150根前
		}
		kl[i] = model.Kline{
			Date: "2026-01-01", Open: 10, Close: 10, High: high, Low: 9.5, Volume: 1e6,
		}
	}
	lv := calcLevels(kl, 10)
	if lv.resistance != 20.0 {
		t.Errorf("压力位应取近120日最高 20.00（覆盖前段冲高），实际 %.2f", lv.resistance)
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

// TestStopLossClamp 止损波动率口径：支撑-1×ATR 与基准价-2.5×ATR 取高，且不深于 -15%。
func TestStopLossClamp(t *testing.T) {
	near := func(got, want float64) bool {
		d := got - want
		return d < 0.01 && d > -0.01
	}
	lv := levels{support: 10, atr: 5} // 支撑-ATR=5、基准-2.5ATR=87.5 → 取 87.5，未触及 -15% 底
	if sl := stopLossOf(100, lv); !near(sl, 87.5) {
		t.Errorf("止损应取基准价-2.5×ATR=87.5，实际 %.2f", sl)
	}
	lv = levels{support: 95, atr: 1} // 94 < 97.5 → 取 97.5（波动小，止损跟得紧）
	if sl := stopLossOf(100, lv); !near(sl, 97.5) {
		t.Errorf("止损应取基准价-2.5×ATR=97.5，实际 %.2f", sl)
	}
	lv = levels{support: 50, atr: 10} // 两者都深于 -15%（85）→ 钳制在 85
	if sl := stopLossOf(100, lv); !near(sl, 85) {
		t.Errorf("止损不应深于 -15%%，实际 %.2f", sl)
	}
	lv = levels{support: 95, atr: 0} // 无 ATR 退回固定 -8%
	if sl := stopLossOf(100, lv); !near(sl, 92) {
		t.Errorf("无 ATR 应退回 -8%%=92，实际 %.2f", sl)
	}
}

// TestStopLossAt 限价等回踩的计划，止损必须锚定买入价而非现价——
// 否则用现价算出的止损高于回踩买入价，一成交就贴损。
func TestStopLossAt(t *testing.T) {
	a := &Assessment{Support: 11.0, Atr: 0.15}
	// 现价口径（12.26）：max(10.85, 11.885) ≈ 11.89 —— 高于回踩买入价 11.44，成交即贴损
	nowStop := round2(stopLossOf(12.26, levels{support: a.Support, atr: a.Atr}))
	if nowStop < 11.44 {
		t.Fatalf("本用例想演示现价口径止损高于买入价，实际 %.2f", nowStop)
	}
	// 买入价口径（11.44）：max(10.85, 11.065) ≈ 11.07，比买入价低 3%+，有呼吸空间
	entryStop := a.StopLossAt(11.44)
	if entryStop >= 11.44 {
		t.Errorf("止损必须低于买入价: %.2f", entryStop)
	}
	if entryStop >= nowStop {
		t.Errorf("买入价口径止损（%.2f）应低于现价口径（%.2f）", entryStop, nowStop)
	}
}

// TestTrendYearLine 趋势分析含年线维度：其余维度同分时，站上年线比跌破年线多 30 分（+15 vs -15）。
func TestTrendYearLine(t *testing.T) {
	// 构造 300 根：长期 10 元横盘、最后 30 根跌到 8 —— MA60≈9、MA250≈9.76。
	// 取现价 9.5（站上 MA60、跌破年线）与 10（两者都站上），仅年线维度不同。
	kl := make([]model.Kline, 300)
	for i := range kl {
		px := 10.0
		if i >= 270 {
			px = 8.0
		}
		kl[i] = model.Kline{Date: "2026-01-01", Open: px, Close: px, High: px * 1.01, Low: px * 0.99, Volume: 1e6}
	}
	below := runTrend(kl, 9.5, levels{})
	if !strings.Contains(below.Reason, "年线下方") {
		t.Errorf("跌破年线应有标记，实际 reason=%s", below.Reason)
	}
	above := runTrend(kl, 10.0, levels{})
	if !strings.Contains(above.Reason, "站上年线") {
		t.Errorf("站上年线应有标记，实际 reason=%s", above.Reason)
	}
	if above.Score-below.Score != 30 {
		t.Errorf("年线维度分差应为 30（+15 vs -15），实际 %.1f（above=%.1f below=%.1f）",
			above.Score-below.Score, above.Score, below.Score)
	}
}

// TestFindingSubs 子项得分必须齐全且合计等于结论分（前端"分析详情"靠它解释分数从哪来）。
func TestFindingSubs(t *testing.T) {
	a, err := Assess("1.600519", "测试股", genKlines(300, 0.3, 1e6, 1), 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range a.Findings {
		if len(f.Subs) == 0 {
			t.Errorf("%s 缺少子项得分", f.Analyzer)
			continue
		}
		sum := 0.0
		for _, s := range f.Subs {
			sum += s.Score
			if s.Name == "" {
				t.Errorf("%s 子项缺名称: %+v", f.Analyzer, s)
			}
		}
		if d := sum - f.Score; d > 0.01 || d < -0.01 {
			t.Errorf("%s 子项合计 %.1f ≠ 结论分 %.1f", f.Analyzer, sum, f.Score)
		}
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
