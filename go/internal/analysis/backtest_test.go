package analysis

import (
	"testing"

	"killer/internal/model"
)

// synthKlines 生成 n 根日K：起价 start，每日变动 pct%，日期从 2026-01-01 起顺延。
func synthKlines(n int, start, pct float64) []model.Kline {
	out := make([]model.Kline, 0, n)
	px := start
	for i := 0; i < n; i++ {
		out = append(out, model.Kline{
			Date:   dateOf(i),
			Open:   px, Close: px, High: px * 1.005, Low: px * 0.995,
			Volume: 1000,
		})
		px *= 1 + pct/100
	}
	return out
}

func dateOf(i int) string {
	// 只要唯一递增即可，不必是真实交易日
	return "2026-01-01+" + string(rune('a'+i/26)) + string(rune('a'+i%26))
}

// TestRollScoreCount 评估次数 = 总根数 - 预热 - 1（最后一根无次根入场价）。
func TestRollScoreCount(t *testing.T) {
	kl := synthKlines(100, 10, 1)
	evals := RollScore(kl, 60)
	if len(evals) != 39 {
		t.Fatalf("评估次数=%d, want 39", len(evals))
	}
	// 入场=次根收盘，5 日观察期要求 i+1+5 < n → 前 34 个有效
	valid := 0
	for _, e := range evals {
		if e.Valid[0] {
			valid++
		}
	}
	if valid != 34 {
		t.Fatalf("5日观察期有效样本=%d, want 34", valid)
	}
	if evals[len(evals)-1].Valid[2] {
		t.Fatalf("最后一段不应有 20 日观察期数据")
	}
}

// TestRollScoreDirection 上行序列评分应高于下行序列，未来收益符号要对得上。
func TestRollScoreDirection(t *testing.T) {
	up := RollScore(synthKlines(120, 10, 1), 60)
	down := RollScore(synthKlines(120, 10, -1), 60)
	if len(up) == 0 || len(down) == 0 {
		t.Fatal("评估为空")
	}
	var upSum, downSum, upR, downR float64
	for i := range up {
		upSum += up[i].Score
		downSum += down[i].Score
		if up[i].Valid[0] {
			upR += up[i].Ret[0]
			downR += down[i].Ret[0]
		}
	}
	if upSum <= downSum {
		t.Errorf("上行序列评分 %.1f 应高于下行 %.1f", upSum, downSum)
	}
	if upR <= 0 || downR >= 0 {
		t.Errorf("上行未来收益 %.2f%% 应为正、下行 %.2f%% 应为负", upR, downR)
	}
}

// TestAggregate 分层统计的胜率/均值/超额与分层顺序。
func TestAggregate(t *testing.T) {
	mk := func(score, r5 float64, ok bool) RollEval {
		return RollEval{Score: score, Advice: adviceOf(score), Ret: [3]float64{r5}, Valid: [3]bool{ok}}
	}
	evals := []RollEval{
		mk(90, 10, true),  // ≥80：+10
		mk(85, -2, true),  // ≥80：-2
		mk(70, 6, true),   // 65-80：+6
		mk(40, -4, true),  // 35-50：-4
		mk(30, 4, true),   // <35：+4
		mk(32, 1, false),  // <35 但无未来数据：不计入均值/胜率，只计入评估次数
	}
	got := Aggregate(evals)
	if len(got) != 4 {
		t.Fatalf("分层数=%d, want 4（50-65 无样本应省略）", len(got))
	}
	if got[0].Bucket != "≥80" || got[len(got)-1].Bucket != "<35" {
		t.Errorf("分层顺序应为评分降序: %+v", got)
	}
	all := (10 - 2 + 6 - 4 + 4) / 5.0 // 全样本 5 日均收益 = 2.8
	if got[0].Count != 2 || got[0].N[0] != 2 {
		t.Errorf("≥80 样本统计错误: %+v", got[0])
	}
	if want := (10-2)/2.0; got[0].Avg[0] != want {
		t.Errorf("≥80 平均收益=%.2f, want %.2f", got[0].Avg[0], want)
	}
	if want := 50.0; got[0].Win[0] != want {
		t.Errorf("≥80 胜率=%.1f, want 50.0", got[0].Win[0])
	}
	if want := (10-2)/2.0 - all; got[0].Excess[0] != want {
		t.Errorf("≥80 超额=%.2f, want %.2f", got[0].Excess[0], want)
	}
	// 无未来数据那笔只进 count，不进 n/均值
	var lt35 BucketStat
	for _, b := range got {
		if b.Bucket == "<35" {
			lt35 = b
		}
	}
	if lt35.Count != 2 || lt35.N[0] != 1 || lt35.Avg[0] != 4 {
		t.Errorf("<35 统计错误: %+v", lt35)
	}
}
