package web

import (
	"testing"
	"time"

	"killer/internal/model"
)

// TestIntradaySnapshot 验证「盘中快照需收盘后重算」判定：
// 只有当日收盘（15:05 缓冲）之前生成的存档才算快照，次日补算与 UTC 入参都不算。
func TestIntradaySnapshot(t *testing.T) {
	cst := time.FixedZone("CST", 8*3600)
	bj := func(day string, h, m int) time.Time {
		d, err := time.ParseInLocation("2006-01-02", day, cst)
		if err != nil {
			t.Fatalf("解析日期失败: %v", err)
		}
		return time.Date(d.Year(), d.Month(), d.Day(), h, m, 0, 0, cst)
	}
	cases := []struct {
		name string
		rpt  *model.AnalysisReport
		date string
		want bool
	}{
		{"盘中生成需重算", &model.AnalysisReport{CreatedAt: bj("2026-11-16", 10, 30)}, "2026-11-16", true},
		{"收盘缓冲前仍需重算", &model.AnalysisReport{CreatedAt: bj("2026-11-16", 15, 2)}, "2026-11-16", true},
		{"收盘后为终版", &model.AnalysisReport{CreatedAt: bj("2026-11-16", 15, 5)}, "2026-11-16", false},
		{"晚间补算为终版", &model.AnalysisReport{CreatedAt: bj("2026-11-16", 22, 0)}, "2026-11-16", false},
		{"次日凌晨补算为终版", &model.AnalysisReport{CreatedAt: bj("2026-11-17", 2, 0)}, "2026-11-16", false},
		{"UTC 入参按北京时间判断", &model.AnalysisReport{CreatedAt: time.Date(2026, 11, 16, 8, 0, 0, 0, time.UTC)}, "2026-11-16", false},
	}
	for _, c := range cases {
		if got := intradaySnapshot(c.rpt, c.date); got != c.want {
			t.Errorf("%s: intradaySnapshot=%v want %v", c.name, got, c.want)
		}
	}
}
