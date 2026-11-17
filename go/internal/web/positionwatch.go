package web

// 持仓盯盘：交易时段按巡检间隔把持仓现价与预案价位比对，
// 止损/止盈触发与大盘转弱写入现有告警表（首页告警表展示，与阈值告警同一套冷却/查询）。

import (
	"context"
	"log"
	"time"

	"killer/internal/model"
	"killer/internal/monitor"
	"killer/internal/source"
	"killer/internal/store"
)

// 预案类告警类型（与 analysis.html/trading.html 预案文案对应）。
const (
	alertPositionStop = "position_stop" // 现价跌破买入价口径止损位
	alertPositionTake = "position_take" // 现价涨到压力位（止盈条件）
	alertRegimeWeak   = "regime_weak"   // 大盘环境转弱势
)

// startPositionWatch 启动持仓盯盘巡检（交易时段外自动跳过；大盘环境无持仓也巡）。
// 与 startArchive 同为进程级 goroutine，随服务生命周期运行。
func (s *Server) startPositionWatch() {
	if s.trade == nil {
		return
	}
	go func() {
		t := time.NewTicker(s.cfg.CheckInterval())
		defer t.Stop()
		for range t.C {
			if !monitor.MarketOpen(time.Now()) {
				continue
			}
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			s.positionWatchOnce(ctx)
			cancel()
		}
	}()
	log.Printf("[PositionWatch] 持仓盯盘已启动 (间隔=%s)", s.cfg.CheckInterval())
}

// positionWatchOnce 一轮盯盘：持仓价位比对 + 大盘环境比对。
func (s *Server) positionWatchOnce(ctx context.Context) {
	s.checkPositionLevels(ctx)
	s.checkRegimeAlert(ctx)
}

// checkPositionLevels 持仓现价 vs 预案价位：止损位（买入价口径）跌破、压力位触及即告警。
// 价位来自分析研判（90 秒缓存），与交易指导/预案同一套数。
func (s *Server) checkPositionLevels(ctx context.Context) {
	positions, err := s.trade.Positions(ctx)
	if err != nil || len(positions) == 0 {
		return
	}
	secids := make([]string, 0, len(positions))
	for _, p := range positions {
		secids = append(secids, p.SecID)
	}
	stocks, err := source.FetchUSQuotes(ctx, secids)
	if err != nil {
		log.Printf("[PositionWatch] 持仓行情失败: %v", err)
		return
	}
	bySecid := map[string]model.Stock{}
	for _, st := range stocks {
		bySecid[st.SecID] = st
	}
	today := sessionToday()
	for _, p := range positions {
		st, ok := bySecid[p.SecID]
		if !ok || st.Price <= 0 || p.AvgCost <= 0 {
			continue
		}
		a, aErr := s.assess(ctx, p.SecID, p.Name)
		if aErr != nil {
			continue
		}
		stop := a.StopLossAt(p.AvgCost)
		if levelHit(alertPositionStop, st.Price, stop) {
			fireDailyAlert(s.st, p.SecID, p.Name, alertPositionStop,
				(st.Price-p.AvgCost)/p.AvgCost*100, (stop-p.AvgCost)/p.AvgCost*100, st.Price, today)
		}
		if levelHit(alertPositionTake, st.Price, a.Resistance) && st.Price > p.AvgCost {
			fireDailyAlert(s.st, p.SecID, p.Name, alertPositionTake,
				(st.Price-p.AvgCost)/p.AvgCost*100, (a.Resistance-p.AvgCost)/p.AvgCost*100, st.Price, today)
		}
	}
}

// checkRegimeAlert 大盘环境转弱势当日首触发告警（提示按预案暂停加仓、守止损）。
// 环境结论有 5 分钟缓存，连续弱势天只会触发一次/天。
func (s *Server) checkRegimeAlert(ctx context.Context) {
	r := s.marketRegime(ctx)
	if r.Label != "弱势" {
		return
	}
	fireDailyAlert(s.st, "market.regime", "大盘环境", alertRegimeWeak, r.Score, 40, 0, sessionToday())
}

// levelHit 触发判定：止损=现价不高于止损位，止盈=现价不低于压力位。
func levelHit(alertType string, price, level float64) bool {
	if price <= 0 || level <= 0 {
		return false
	}
	switch alertType {
	case alertPositionStop:
		return price <= level
	case alertPositionTake:
		return price >= level
	}
	return false
}

// shouldFireDaily 预案类告警同标的同类型每交易日只触发一次：
// 当天已触发过就不再写（沿用 30 分钟冷却会整天刷屏，而预案当天行动一次即可）。
func shouldFireDaily(last time.Time, today string) bool {
	if last.IsZero() {
		return true
	}
	return monitor.Beijing(last).Format("2006-01-02") != today
}

// sessionToday 北京时间口径的今天（YYYY-MM-DD）。
func sessionToday() string {
	return monitor.Beijing(time.Now()).Format("2006-01-02")
}

// fireDailyAlert 写入告警（落库失败只记日志，不影响下一轮巡检）。
func fireDailyAlert(st *store.Store, secid, name, alertType string, value, threshold, price float64, today string) {
	last, err := st.LastAlertTime(secid, alertType)
	if err == nil && !shouldFireDaily(last, today) {
		return
	}
	a := model.Alert{
		SecID: secid, Name: name, Type: alertType,
		Value: value, Threshold: threshold, Price: price,
		TriggeredAt: time.Now(),
	}
	id, err := st.SaveAlert(a)
	if err != nil {
		log.Printf("[PositionWatch] 告警落库失败 %s: %v", secid, err)
		return
	}
	a.ID = id
	log.Printf("[PositionWatch] ⚠️ %s", a.String())
}
