package web

import (
	"encoding/json"
	"net/http"
	"strconv"

	"killer/internal/model"
	"killer/internal/trading"
)

// ============ 交易 API ============

// handleTradeAccount GET /api/trade/account — 账户概览。
func (s *Server) handleTradeAccount(w http.ResponseWriter, r *http.Request) {
	acc, err := s.trade.Account(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, acc)
}

// handleTradePositions GET /api/trade/positions — 持仓列表。
func (s *Server) handleTradePositions(w http.ResponseWriter, r *http.Request) {
	positions, err := s.trade.Positions(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if positions == nil {
		positions = []model.Position{}
	}
	writeJSON(w, positions)
}

// handleTradeOrders GET /api/trade/orders — 订单列表（?status=pending 仅挂单）。
func (s *Server) handleTradeOrders(w http.ResponseWriter, r *http.Request) {
	status := r.URL.Query().Get("status")
	if status == "pending" {
		orders, err := s.trade.PendingOrders()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if orders == nil {
			orders = []model.Order{}
		}
		writeJSON(w, orders)
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	orders, err := s.trade.Orders(limit)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if orders == nil {
		orders = []model.Order{}
	}
	writeJSON(w, orders)
}

// handleTradePlace POST /api/trade/orders — 下单。
// body: {secid, code, name, side, order_type, price, quantity}
func (s *Server) handleTradePlace(w http.ResponseWriter, r *http.Request) {
	var req model.Order
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "非法请求体: "+err.Error(), http.StatusBadRequest)
		return
	}
	id, status, err := s.trade.PlaceOrder(r.Context(), req)
	if err != nil {
		// 业务拒绝返回 200 + 状态，便于前端展示原因
		writeJSON(w, map[string]any{"id": id, "status": status, "error": err.Error()})
		return
	}
	writeJSON(w, map[string]any{"id": id, "status": status})
}

// handleTradeCancel DELETE /api/trade/orders?id=123 — 撤销挂单。
func (s *Server) handleTradeCancel(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(r.URL.Query().Get("id"), 10, 64)
	if id <= 0 {
		http.Error(w, "missing id", http.StatusBadRequest)
		return
	}
	if err := s.trade.CancelOrder(id); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, map[string]any{"ok": true})
}

// handleTradeTrades GET /api/trade/trades — 成交记录。
func (s *Server) handleTradeTrades(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	trades, err := s.trade.Trades(limit)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if trades == nil {
		trades = []model.Trade{}
	}
	writeJSON(w, trades)
}

// handleTradeSignal GET /api/trade/signal?strategy=ma_cross&secid=1.600519&name=贵州茅台
// 生成单个标的策略信号。
func (s *Server) handleTradeSignal(w http.ResponseWriter, r *http.Request) {
	strategy := r.URL.Query().Get("strategy")
	secid := r.URL.Query().Get("secid")
	name := r.URL.Query().Get("name")
	if strategy == "" || secid == "" {
		http.Error(w, "missing strategy or secid", http.StatusBadRequest)
		return
	}
	sig, err := trading.Generate(r.Context(), strategy, secid, name)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	writeJSON(w, sig)
}

// handleTradeBacktest POST /api/trade/backtest — 运行回测。
// body: {strategy, secid, name, initial_cash}
func (s *Server) handleTradeBacktest(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Strategy    string  `json:"strategy"`
		SecID       string  `json:"secid"`
		Name        string  `json:"name"`
		InitialCash float64 `json:"initial_cash"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "非法请求体: "+err.Error(), http.StatusBadRequest)
		return
	}
	if req.Strategy == "" || req.SecID == "" {
		http.Error(w, "missing strategy or secid", http.StatusBadRequest)
		return
	}
	result, err := s.trade.Backtest(r.Context(), req.Strategy, req.SecID, req.Name, req.InitialCash)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	writeJSON(w, result)
}

// handleTradeStrategies GET /api/trade/strategies — 策略列表。
func (s *Server) handleTradeStrategies(w http.ResponseWriter, r *http.Request) {
	out := make([]map[string]string, 0, len(trading.Strategies))
	for _, name := range trading.Strategies {
		out = append(out, map[string]string{"name": name, "label": trading.StrategyName(name)})
	}
	writeJSON(w, out)
}
