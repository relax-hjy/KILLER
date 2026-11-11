// Package monitor 大盘监控看门狗。
//
// 职责：
//  1. 定时轮询配置中指数的实时行情（主源失败自动切备用源）
//  2. 快照落库（走势数据）
//  3. 阈值比对：涨跌幅 / 振幅超限触发告警，带冷却防刷屏
package monitor

import (
	"context"
	"log"
	"sync"
	"time"

	"killer/internal/config"
	"killer/internal/model"
	"killer/internal/source"
	"killer/internal/store"
)

// tradingHours 判断北京时间是否在交易时段（含集合竞价）。
// 上午 9:15–11:35，下午 13:00–15:05（留收盘缓冲），周一至周五。
func tradingHours(t time.Time) bool {
	loc, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		loc = time.FixedZone("CST", 8*3600) // 兜底
	}
	bj := t.In(loc)
	if bj.Weekday() == time.Saturday || bj.Weekday() == time.Sunday {
		return false
	}
	mins := bj.Hour()*60 + bj.Minute()
	return (mins >= 9*60+15 && mins <= 11*60+35) || (mins >= 13*60 && mins <= 15*60+5)
}

// Watchdog 大盘监控看门狗。
type Watchdog struct {
	cfg     *config.Config
	primary source.Source
	fallback source.Source
	store   *store.Store

	mu      sync.Mutex
	latest  map[string]model.Quote // secid → 最新行情
	alerts  []model.Alert          // 内存中最近告警（供面板即时读取）
	lastErr string

	stopCh chan struct{}
	wg     sync.WaitGroup
}

// New 创建看门狗。
func New(cfg *config.Config, st *store.Store) (*Watchdog, error) {
	primary, err := source.New(cfg.Datasource.Primary)
	if err != nil {
		return nil, err
	}
	var fallback source.Source
	if cfg.Datasource.Fallback != "" && cfg.Datasource.Fallback != cfg.Datasource.Primary {
		if fallback, err = source.New(cfg.Datasource.Fallback); err != nil {
			return nil, err
		}
	}
	return &Watchdog{
		cfg:      cfg,
		primary:  primary,
		fallback: fallback,
		store:    st,
		latest:   make(map[string]model.Quote),
		stopCh:   make(chan struct{}),
	}, nil
}

// secids 返回监控指数列表。
func (w *Watchdog) secids() []string {
	out := make([]string, 0, len(w.cfg.Indices))
	for _, ix := range w.cfg.Indices {
		out = append(out, ix.SecID)
	}
	return out
}

// nameOf secid → 配置中的显示名。
func (w *Watchdog) nameOf(secid string) string {
	for _, ix := range w.cfg.Indices {
		if ix.SecID == secid {
			return ix.Name
		}
	}
	return secid
}

// fetchQuotes 主源失败切备用源。
func (w *Watchdog) fetchQuotes(ctx context.Context) ([]model.Quote, error) {
	quotes, err := w.primary.FetchQuotes(ctx, w.secids())
	if err == nil {
		return quotes, nil
	}
	log.Printf("[Watchdog] 主源 %s 失败: %v，切换备用源", w.primary.Name(), err)
	if w.fallback == nil {
		return nil, err
	}
	return w.fallback.FetchQuotes(ctx, w.secids())
}

// CheckOnce 执行一轮巡检：拉行情 → 落库 → 阈值比对 → 触发告警。
// 返回本轮新产生的告警。
func (w *Watchdog) CheckOnce(ctx context.Context) []model.Alert {
	secids := w.secids()
	if len(secids) == 0 {
		return nil
	}

	quotes, err := w.fetchQuotes(ctx)
	w.mu.Lock()
	if err != nil {
		w.lastErr = err.Error()
		w.mu.Unlock()
		log.Printf("[Watchdog] 拉取行情失败: %v", err)
		return nil
	}
	w.lastErr = ""
	for _, q := range quotes {
		w.latest[q.SecID] = q
	}
	w.mu.Unlock()

	// 快照落库（尽力而为，失败不中断告警）
	for _, q := range quotes {
		if err := w.store.SaveQuote(q); err != nil {
			log.Printf("[Watchdog] 快照落库失败 %s: %v", q.SecID, err)
		}
	}

	// 非交易时段且配置要求仅交易时段 → 只存快照不告警
	if w.cfg.Monitoring.TradingHoursOnly && !tradingHours(time.Now()) {
		return nil
	}

	alerts := w.evaluate(quotes)
	alerts = append(alerts, w.checkWatchStocks(ctx)...)
	return alerts
}

// checkWatchStocks 巡检自选个股：涨跌幅超过各自阈值触发告警（类型 stock_change）。
func (w *Watchdog) checkWatchStocks(ctx context.Context) []model.Alert {
	watch, err := w.store.ListWatchStocks()
	if err != nil || len(watch) == 0 {
		return nil
	}
	secids := make([]string, 0, len(watch))
	for _, item := range watch {
		secids = append(secids, item.SecID)
	}
	// A股/美股混合批量行情（东财 ulist 通吃，失败则本轮放弃）
	stocks, err := source.FetchUSQuotes(ctx, secids)
	if err != nil {
		log.Printf("[Watchdog] 自选个股行情失败: %v", err)
		return nil
	}
	bySecid := map[string]model.Stock{}
	for _, s := range stocks {
		bySecid[s.SecID] = s
	}

	cooldown := w.cfg.Cooldown()
	now := time.Now()
	var fired []model.Alert
	for _, item := range watch {
		s, ok := bySecid[item.SecID]
		if !ok || item.Threshold <= 0 {
			continue
		}
		if abs(s.Pct) < item.Threshold {
			continue
		}
		alertType := "stock_change_up"
		if s.Pct < 0 {
			alertType = "stock_change_down"
		}
		last, err := w.store.LastAlertTime(item.SecID, alertType)
		if err == nil && !last.IsZero() && now.Sub(last) < cooldown {
			continue
		}
		a := model.Alert{
			SecID: item.SecID, Name: s.Name, Type: alertType,
			Value: abs(s.Pct), Threshold: item.Threshold,
			Price: s.Price, TriggeredAt: now,
		}
		if a.Name == "" {
			a.Name = item.Name
		}
		id, err := w.store.SaveAlert(a)
		if err != nil {
			log.Printf("[Watchdog] 自选告警落库失败 %s: %v", item.SecID, err)
			continue
		}
		a.ID = id
		fired = append(fired, a)
		log.Printf("[Watchdog] ⚠️ %s", a.String())
	}
	return fired
}

func abs(v float64) float64 {
	if v < 0 {
		return -v
	}
	return v
}

// evaluate 阈值比对与冷却判断。
func (w *Watchdog) evaluate(quotes []model.Quote) []model.Alert {
	ac := w.cfg.Monitoring.Alert
	cooldown := w.cfg.Cooldown()
	now := time.Now()

	var fired []model.Alert
	for _, q := range quotes {
		type check struct {
			alertType string
			value     float64
			threshold float64
		}
		checks := []check{
			{"change_up", q.Pct, ac.ChangePct},
			{"change_down", -q.Pct, ac.ChangePct},
			{"amplitude", q.Amplitude, ac.AmplitudePct},
		}
		for _, c := range checks {
			if c.threshold <= 0 || c.value < c.threshold {
				continue
			}
			// 冷却判断
			last, err := w.store.LastAlertTime(q.SecID, c.alertType)
			if err == nil && !last.IsZero() && now.Sub(last) < cooldown {
				continue
			}
			a := model.Alert{
				SecID: q.SecID, Name: w.nameOf(q.SecID),
				Type: c.alertType, Value: c.value, Threshold: c.threshold,
				Price: q.Price, TriggeredAt: now,
			}
			id, err := w.store.SaveAlert(a)
			if err != nil {
				log.Printf("[Watchdog] 告警落库失败 %s: %v", q.SecID, err)
				continue
			}
			a.ID = id
			fired = append(fired, a)
			log.Printf("[Watchdog] ⚠️ %s", a.String())
		}
	}

	if len(fired) > 0 {
		w.mu.Lock()
		w.alerts = append(w.alerts, fired...)
		if len(w.alerts) > 200 {
			w.alerts = w.alerts[len(w.alerts)-200:]
		}
		w.mu.Unlock()
	}
	return fired
}

// Start 启动定时巡检（立即执行一轮，随后按间隔轮询）。
func (w *Watchdog) Start() {
	w.wg.Add(1)
	go func() {
		defer w.wg.Done()
		interval := w.cfg.CheckInterval()
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		w.CheckOnce(ctx)
		cancel()

		for {
			select {
			case <-w.stopCh:
				return
			case <-ticker.C:
				ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
				w.CheckOnce(ctx)
				cancel()
			}
		}
	}()
	log.Printf("[Watchdog] 定时巡检已启动 (间隔=%s, 指数=%d 个)", w.cfg.CheckInterval(), len(w.cfg.Indices))
}

// Stop 停止巡检。
func (w *Watchdog) Stop() {
	close(w.stopCh)
	w.wg.Wait()
	log.Println("[Watchdog] 已停止")
}

// Latest 返回所有指数最新行情快照。
func (w *Watchdog) Latest() []model.Quote {
	w.mu.Lock()
	defer w.mu.Unlock()
	out := make([]model.Quote, 0, len(w.latest))
	for _, ix := range w.cfg.Indices {
		if q, ok := w.latest[ix.SecID]; ok {
			out = append(out, q)
		}
	}
	return out
}

// Status 返回巡检状态（供面板展示）。
func (w *Watchdog) Status() map[string]any {
	w.mu.Lock()
	defer w.mu.Unlock()
	return map[string]any{
		"primary":       w.primary.Name(),
		"fallback":      w.fallbackName(),
		"last_error":    w.lastErr,
		"interval_sec":  w.cfg.Monitoring.CheckIntervalSec,
		"trading_hours": tradingHours(time.Now()),
		"monitored":     len(w.cfg.Indices),
	}
}

func (w *Watchdog) fallbackName() string {
	if w.fallback == nil {
		return ""
	}
	return w.fallback.Name()
}
