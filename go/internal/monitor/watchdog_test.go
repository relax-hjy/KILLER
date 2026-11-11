package monitor

import (
	"testing"
	"time"
)

// TestTradingHours 验证交易时段判断（北京时间口径）。
func TestTradingHours(t *testing.T) {
	loc, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Skipf("时区加载失败: %v", err)
	}
	cases := []struct {
		name string
		t    time.Time
		want bool
	}{
		{"开盘竞价 09:20 周一", time.Date(2026, 11, 9, 9, 20, 0, 0, loc), true},
		{"早盘 10:30 周一", time.Date(2026, 11, 9, 10, 30, 0, 0, loc), true},
		{"午间休市 12:00 周一", time.Date(2026, 11, 9, 12, 0, 0, 0, loc), false},
		{"午盘 14:00 周一", time.Date(2026, 11, 9, 14, 0, 0, 0, loc), true},
		{"收盘后 15:30 周一", time.Date(2026, 11, 9, 15, 30, 0, 0, loc), false},
		{"开盘前 08:00 周一", time.Date(2026, 11, 9, 8, 0, 0, 0, loc), false},
		{"周六 10:30", time.Date(2026, 11, 14, 10, 30, 0, 0, loc), false},
		{"周日 14:00", time.Date(2026, 11, 15, 14, 0, 0, 0, loc), false},
	}
	for _, c := range cases {
		if got := tradingHours(c.t); got != c.want {
			t.Errorf("%s: tradingHours=%v want %v", c.name, got, c.want)
		}
	}
}

// TestTradingHoursUTCInput 确认传入 UTC 时间同样按北京时间换算。
func TestTradingHoursUTCInput(t *testing.T) {
	// 北京 10:30 = UTC 02:30
	utc := time.Date(2026, 11, 9, 2, 30, 0, 0, time.UTC)
	if !tradingHours(utc) {
		t.Error("UTC 02:30（北京 10:30）应为交易时段")
	}
	// 北京 12:00 = UTC 04:00（午休）
	utcNoon := time.Date(2026, 11, 9, 4, 0, 0, 0, time.UTC)
	if tradingHours(utcNoon) {
		t.Error("UTC 04:00（北京 12:00 午休）不应为交易时段")
	}
}
