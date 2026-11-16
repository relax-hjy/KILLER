package monitor

import (
	"path/filepath"
	"testing"
	"time"

	"killer/internal/config"
	"killer/internal/model"
	"killer/internal/store"
)

// TestEvaluateBoardChangeThreshold 验证板块用独立涨跌幅阈值：
// 指数 1.5% 超全局 1% 触发；板块 1.5% 低于板块阈值 2% 不触发；板块 2.5% 触发且阈值为 2%。
func TestEvaluateBoardChangeThreshold(t *testing.T) {
	cfg := config.Default()
	cfg.Indices = []config.Index{{SecID: "1.000001", Name: "上证指数"}}
	cfg.Boards = []config.Index{
		{SecID: "pt02GN2211", Name: "CPO概念"},
		{SecID: "pt02101284", Name: "PCB"},
	}

	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("打开测试库失败: %v", err)
	}
	defer st.Close()

	w := &Watchdog{cfg: cfg, store: st, latest: map[string]model.Quote{}}
	quotes := []model.Quote{
		{SecID: "1.000001", Name: "上证指数", Pct: 1.5, Price: 3300},
		{SecID: "pt02GN2211", Name: "CPO概念", Pct: 1.5, Price: 1000},
		{SecID: "pt02101284", Name: "PCB", Pct: 2.5, Price: 1000},
	}

	fired := w.evaluate(quotes)

	got := map[string]model.Alert{}
	for _, a := range fired {
		got[a.SecID] = a
	}
	if a, ok := got["1.000001"]; !ok {
		t.Error("指数 1.5% 应超过全局阈值 1% 触发告警")
	} else if a.Threshold != cfg.Monitoring.Alert.ChangePct {
		t.Errorf("指数告警阈值=%v want %v", a.Threshold, cfg.Monitoring.Alert.ChangePct)
	}
	if _, ok := got["pt02GN2211"]; ok {
		t.Error("板块 1.5% 未达板块阈值 2%，不应触发告警")
	}
	if a, ok := got["pt02101284"]; !ok {
		t.Error("板块 2.5% 应超过板块阈值 2% 触发告警")
	} else if a.Threshold != cfg.Monitoring.Alert.BoardChangePct {
		t.Errorf("板块告警阈值=%v want %v", a.Threshold, cfg.Monitoring.Alert.BoardChangePct)
	} else if a.Name != "PCB" {
		t.Errorf("板块告警名称=%q want %q", a.Name, "PCB")
	}
}

// TestLastSessionDate 验证最近已收盘交易日推算（北京时间口径）。
func TestLastSessionDate(t *testing.T) {
	loc, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		loc = time.FixedZone("CST", 8*3600)
	}
	at := func(y int, mo time.Month, d, h, mi int) time.Time {
		return time.Date(y, mo, d, h, mi, 0, 0, loc)
	}
	cases := []struct {
		name string
		t    time.Time
		want string
	}{
		{"收盘后归当日", at(2026, 11, 16, 15, 30), "2026-11-16"}, // 周一
		{"盘中仍归当日", at(2026, 11, 16, 10, 0), "2026-11-16"},  // 盘中已有当日日K
		{"盘前归前一工作日", at(2026, 11, 17, 8, 0), "2026-11-16"}, // 周二 08:00
		{"周一盘前归上周五", at(2026, 11, 16, 0, 30), "2026-11-13"},
		{"周六归周五", at(2026, 11, 14, 20, 0), "2026-11-13"}, // 周六
		{"周日凌晨归上周五", at(2026, 11, 15, 1, 0), "2026-11-13"},
	}
	for _, c := range cases {
		if got := LastSessionDate(c.t); got != c.want {
			t.Errorf("%s: LastSessionDate=%s want %s", c.name, got, c.want)
		}
	}
}

// TestMarketOpen 验证交易时段判断（北京时间口径）。
func TestMarketOpen(t *testing.T) {
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
		if got := MarketOpen(c.t); got != c.want {
			t.Errorf("%s: MarketOpen=%v want %v", c.name, got, c.want)
		}
	}
}

// TestTradingHoursUTCInput 确认传入 UTC 时间同样按北京时间换算。
func TestTradingHoursUTCInput(t *testing.T) {
	// 北京 10:30 = UTC 02:30
	utc := time.Date(2026, 11, 9, 2, 30, 0, 0, time.UTC)
	if !MarketOpen(utc) {
		t.Error("UTC 02:30（北京 10:30）应为交易时段")
	}
	// 北京 12:00 = UTC 04:00（午休）
	utcNoon := time.Date(2026, 11, 9, 4, 0, 0, 0, time.UTC)
	if MarketOpen(utcNoon) {
		t.Error("UTC 04:00（北京 12:00 午休）不应为交易时段")
	}
}
