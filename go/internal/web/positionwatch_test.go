package web

import (
	"testing"
	"time"
)

// TestLevelHit 止损=现价≤位触发，止盈=现价≥位触发；0 位/0 价永不触发。
func TestLevelHit(t *testing.T) {
	cases := []struct {
		name      string
		alertType string
		price     float64
		level     float64
		want      bool
	}{
		{"止损触发", alertPositionStop, 94, 95, true},
		{"止损未触", alertPositionStop, 96, 95, false},
		{"止损正好打到", alertPositionStop, 95, 95, true},
		{"止盈触发", alertPositionTake, 110, 110, true},
		{"止盈未到", alertPositionTake, 109, 110, false},
		{"无止损位", alertPositionStop, 90, 0, false},
		{"无行情", alertPositionStop, 0, 95, false},
		{"未知类型", "change_up", 90, 95, false},
	}
	for _, c := range cases {
		if got := levelHit(c.alertType, c.price, c.level); got != c.want {
			t.Errorf("%s: levelHit(%s, %v, %v) = %v, want %v", c.name, c.alertType, c.price, c.level, got, c.want)
		}
	}
}

// TestShouldFireDaily 同标的同类型每交易日只触发一次：首触发放行、当日已触发拦截、隔日放行。
// 注意用 UTC 构造——北京时间 17 日 16:00 的告警换算到北京仍是 17 日。
func TestShouldFireDaily(t *testing.T) {
	cases := []struct {
		name  string
		last  time.Time
		today string
		want  bool
	}{
		{"从未触发", time.Time{}, "2026-11-18", true},
		{"当天上午已触发", time.Date(2026, 11, 18, 1, 0, 0, 0, time.UTC), "2026-11-18", false}, // 北京 09:00
		{"昨天触发过", time.Date(2026, 11, 17, 8, 0, 0, 0, time.UTC), "2026-11-18", true},    // 北京 16:00 17日
		{"跨零点边界", time.Date(2026, 11, 17, 16, 30, 0, 0, time.UTC), "2026-11-18", false}, // 北京 00:30 18日
	}
	for _, c := range cases {
		if got := shouldFireDaily(c.last, c.today); got != c.want {
			t.Errorf("%s: shouldFireDaily(%v, %s) = %v, want %v", c.name, c.last, c.today, got, c.want)
		}
	}
}
