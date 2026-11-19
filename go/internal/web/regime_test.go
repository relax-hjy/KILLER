package web

import (
	"testing"

	"killer/internal/analysis"
)

// TestDowngradeAdvice 弱市降档只按买入类，回避不再降。
func TestDowngradeAdvice(t *testing.T) {
	cases := map[string]string{
		"可积极买入":      "可以买入",
		"可以买入":       "观望，回踩支撑再介入",
		"观望，回踩支撑再介入": "偏弱，暂缓买入",
		"偏弱，暂缓买入":    "回避",
		"回避":         "回避",
	}
	for in, want := range cases {
		if got := downgradeAdvice(in); got != want {
			t.Errorf("downgradeAdvice(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestCalibrate 阈值校准：前 70% 找超额转正的分界，后 30% 验证为正才 reliable。
func TestCalibrate(t *testing.T) {
	// 构造：评分 ≥60 的样本 5 日收益 +2，低于 60 的 -1 → 全样本均值约 0.5，
	// 最优阈值应落在 60~65 附近且样本外同样成立。
	var evals []analysis.RollEval
	for i := 0; i < 400; i++ {
		score := 30.0
		ret := -1.0
		if i%2 == 0 {
			score, ret = 70.0, 2.0
		}
		evals = append(evals, analysis.RollEval{
			Date: dateOf(i), Score: score,
			Ret: [3]float64{ret}, Valid: [3]bool{true},
		})
	}
	c := calibrate(evals)
	if c["reliable"] != true {
		t.Errorf("样本内外都应成立，实际 %+v", c)
	}
	if th, _ := c["threshold"].(float64); th <= 30 || th > 70 {
		t.Errorf("阈值应落在高分段（30,70]，实际 %v", c["threshold"])
	}

	// 反例：前 70% 高分赚钱、后 30% 高分亏钱 → 不可靠
	evals = nil
	for i := 0; i < 400; i++ {
		score, ret := 30.0, -1.0
		if i%2 == 0 {
			score, ret = 70.0, 2.0
		}
		if i >= 280 {
			score, ret = 70.0, -5.0
		}
		evals = append(evals, analysis.RollEval{
			Date: dateOf(i), Score: score,
			Ret: [3]float64{ret}, Valid: [3]bool{true},
		})
	}
	if c := calibrate(evals); c["reliable"] == true {
		t.Errorf("样本外失效应判不可靠，实际 %+v", c)
	}

	// 样本不足不给结论
	if c := calibrate(evals[:50]); c["reliable"] != false {
		t.Errorf("样本不足应 reliable=false，实际 %+v", c)
	}
}

// dateOf 生成唯一递增日期串。
func dateOf(i int) string {
	return "2026-01-01+" + string(rune('a'+i/26)) + string(rune('a'+i%26))
}
