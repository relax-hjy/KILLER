// Package trading 模拟交易引擎：账户、下单、撮合、持仓、成交、持久化。
//
// 数据持久化到独立 SQLite（config.trading.data_dir 下 trading.db），
// 与监控系统的 market.db 分离，便于网盘同步与备份。
package trading

import (
	"database/sql"
	"fmt"
	"time"

	"killer/internal/model"

	_ "modernc.org/sqlite" // 纯 Go SQLite 驱动
)

// Store 交易数据库。
type Store struct {
	db *sql.DB
}

// Open 打开（或创建）交易数据库并建表。
func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path+"?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)")
	if err != nil {
		return nil, fmt.Errorf("打开交易数据库失败: %w", err)
	}
	db.SetMaxOpenConns(1)

	schema := `
CREATE TABLE IF NOT EXISTS account (
	id           INTEGER PRIMARY KEY CHECK (id = 1),
	cash         REAL NOT NULL,
	frozen_cash  REAL NOT NULL DEFAULT 0,
	initial_cash REAL NOT NULL,
	updated_at   INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS position (
	secid      TEXT PRIMARY KEY,
	code       TEXT NOT NULL,
	name       TEXT NOT NULL DEFAULT '',
	quantity   INTEGER NOT NULL DEFAULT 0,
	available  INTEGER NOT NULL DEFAULT 0,
	avg_cost   REAL NOT NULL DEFAULT 0,
	updated_at INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS trade_order (
	id           INTEGER PRIMARY KEY AUTOINCREMENT,
	secid        TEXT NOT NULL,
	code         TEXT NOT NULL,
	name         TEXT NOT NULL DEFAULT '',
	side         TEXT NOT NULL,
	order_type   TEXT NOT NULL,
	price        REAL NOT NULL DEFAULT 0,
	quantity     INTEGER NOT NULL,
	filled_qty   INTEGER NOT NULL DEFAULT 0,
	filled_price REAL NOT NULL DEFAULT 0,
	status       TEXT NOT NULL,
	fee          REAL NOT NULL DEFAULT 0,
	reason       TEXT NOT NULL DEFAULT '',
	created_at   INTEGER NOT NULL,
	updated_at   INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_order_secid ON trade_order(secid);
CREATE INDEX IF NOT EXISTS idx_order_status ON trade_order(status);

CREATE TABLE IF NOT EXISTS trade (
	id         INTEGER PRIMARY KEY AUTOINCREMENT,
	order_id   INTEGER NOT NULL,
	secid      TEXT NOT NULL,
	code       TEXT NOT NULL,
	name       TEXT NOT NULL DEFAULT '',
	side       TEXT NOT NULL,
	price      REAL NOT NULL,
	quantity   INTEGER NOT NULL,
	amount     REAL NOT NULL,
	fee        REAL NOT NULL DEFAULT 0,
	traded_at  INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_trade_secid ON trade(secid);
`
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("建交易表失败: %w", err)
	}
	return &Store{db: db}, nil
}

func (s *Store) Close() error { return s.db.Close() }

// ============ 账户 ============

// GetAccount 读取账户（不存在则创建）。
func (s *Store) GetAccount(initialCash float64) (*model.Account, error) {
	var cash, frozen, initial float64
	var ts int64
	err := s.db.QueryRow(`SELECT cash, frozen_cash, initial_cash, updated_at FROM account WHERE id=1`).
		Scan(&cash, &frozen, &initial, &ts)
	if err == sql.ErrNoRows {
		// 首次：按初始资金建账户
		if initialCash <= 0 {
			initialCash = 1000000
		}
		now := time.Now()
		_, err = s.db.Exec(`INSERT INTO account(id,cash,frozen_cash,initial_cash,updated_at) VALUES(1,?,?,?,?)`,
			initialCash, 0, initialCash, now.Unix())
		if err != nil {
			return nil, err
		}
		return &model.Account{
			InitialCash: initialCash, Cash: initialCash, FrozenCash: 0,
			TotalAsset: initialCash, UpdatedAt: now,
		}, nil
	}
	if err != nil {
		return nil, err
	}
	return &model.Account{
		InitialCash: initial, Cash: cash, FrozenCash: frozen,
		UpdatedAt: time.Unix(ts, 0),
	}, nil
}

// updateCash 更新账户现金（delta 可正可负）。返回更新后账户。
func (s *Store) updateCash(deltaCash, deltaFrozen float64) (*model.Account, error) {
	_, err := s.db.Exec(`UPDATE account SET cash = cash + ?, frozen_cash = frozen_cash + ?, updated_at = ? WHERE id=1`,
		deltaCash, deltaFrozen, time.Now().Unix())
	if err != nil {
		return nil, err
	}
	return s.GetAccount(0)
}

// ============ 持仓 ============

// GetPosition 读取单只持仓（无则返回 nil）。
func (s *Store) GetPosition(secid string) (*model.Position, error) {
	var p model.Position
	var ts int64
	err := s.db.QueryRow(`SELECT secid,code,name,quantity,available,avg_cost,updated_at FROM position WHERE secid=?`, secid).
		Scan(&p.SecID, &p.Code, &p.Name, &p.Quantity, &p.Available, &p.AvgCost, &ts)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	p.UpdatedAt = time.Unix(ts, 0)
	return &p, nil
}

// ListPositions 全部持仓（按市值需调用方填 LastPrice）。
func (s *Store) ListPositions() ([]model.Position, error) {
	rows, err := s.db.Query(`SELECT secid,code,name,quantity,available,avg_cost,updated_at FROM position WHERE quantity > 0 ORDER BY secid`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.Position
	for rows.Next() {
		var p model.Position
		var ts int64
		if err := rows.Scan(&p.SecID, &p.Code, &p.Name, &p.Quantity, &p.Available, &p.AvgCost, &ts); err != nil {
			return nil, err
		}
		p.UpdatedAt = time.Unix(ts, 0)
		out = append(out, p)
	}
	return out, rows.Err()
}

// applyPosition 更新持仓（买入加仓/卖出减仓），quantity<=0 时删除。
func (s *Store) applyPosition(secid, code, name string, deltaQty int64, fillPrice float64) error {
	p, err := s.GetPosition(secid)
	if err != nil {
		return err
	}
	if p == nil {
		if deltaQty <= 0 {
			return fmt.Errorf("无持仓可卖: %s", secid)
		}
		_, err = s.db.Exec(`INSERT INTO position(secid,code,name,quantity,available,avg_cost,updated_at) VALUES(?,?,?,?,?,?,?)`,
			secid, code, name, deltaQty, deltaQty, fillPrice, time.Now().Unix())
		return err
	}
	newQty := p.Quantity + deltaQty
	if newQty < 0 {
		return fmt.Errorf("持仓不足: %s 现有 %d 股", secid, p.Quantity)
	}
	if newQty == 0 {
		_, err = s.db.Exec(`DELETE FROM position WHERE secid=?`, secid)
		return err
	}
	// 买入加仓：更新均价；卖出减仓：均价不变
	newAvg := p.AvgCost
	if deltaQty > 0 {
		newAvg = (p.AvgCost*float64(p.Quantity) + fillPrice*float64(deltaQty)) / float64(newQty)
	}
	_, err = s.db.Exec(`UPDATE position SET quantity=?, available=?, avg_cost=?, name=?, updated_at=? WHERE secid=?`,
		newQty, newQty, newAvg, name, time.Now().Unix(), secid)
	return err
}

// ============ 订单 ============

// CreateOrder 落库新订单，返回自增 ID。
func (s *Store) CreateOrder(o *model.Order) (int64, error) {
	now := time.Now()
	o.CreatedAt = now
	o.UpdatedAt = now
	res, err := s.db.Exec(
		`INSERT INTO trade_order(secid,code,name,side,order_type,price,quantity,filled_qty,filled_price,status,fee,reason,created_at,updated_at)
		 VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		o.SecID, o.Code, o.Name, o.Side, o.OrderType, o.Price, o.Quantity, o.FilledQty, o.FilledPrice, o.Status, o.Fee, o.Reason, now.Unix(), now.Unix(),
	)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// UpdateOrder 更新订单成交状态。
func (s *Store) UpdateOrder(id int64, filledQty int64, filledPrice float64, status string, fee float64, reason string) error {
	_, err := s.db.Exec(
		`UPDATE trade_order SET filled_qty=?, filled_price=?, status=?, fee=?, reason=?, updated_at=? WHERE id=?`,
		filledQty, filledPrice, status, fee, reason, time.Now().Unix(), id,
	)
	return err
}

// GetOrder 读取单笔订单。
func (s *Store) GetOrder(id int64) (*model.Order, error) {
	var o model.Order
	var cts, uts int64
	err := s.db.QueryRow(`SELECT id,secid,code,name,side,order_type,price,quantity,filled_qty,filled_price,status,fee,reason,created_at,updated_at FROM trade_order WHERE id=?`, id).
		Scan(&o.ID, &o.SecID, &o.Code, &o.Name, &o.Side, &o.OrderType, &o.Price, &o.Quantity, &o.FilledQty, &o.FilledPrice, &o.Status, &o.Fee, &o.Reason, &cts, &uts)
	if err != nil {
		return nil, err
	}
	o.CreatedAt = time.Unix(cts, 0)
	o.UpdatedAt = time.Unix(uts, 0)
	return &o, nil
}

// ListOrders 最近订单（按时间降序）。
func (s *Store) ListOrders(limit int) ([]model.Order, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	rows, err := s.db.Query(`SELECT id,secid,code,name,side,order_type,price,quantity,filled_qty,filled_price,status,fee,reason,created_at,updated_at FROM trade_order ORDER BY id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.Order
	for rows.Next() {
		var o model.Order
		var cts, uts int64
		if err := rows.Scan(&o.ID, &o.SecID, &o.Code, &o.Name, &o.Side, &o.OrderType, &o.Price, &o.Quantity, &o.FilledQty, &o.FilledPrice, &o.Status, &o.Fee, &o.Reason, &cts, &uts); err != nil {
			return nil, err
		}
		o.CreatedAt = time.Unix(cts, 0)
		o.UpdatedAt = time.Unix(uts, 0)
		out = append(out, o)
	}
	return out, rows.Err()
}

// ListPendingOrders 全部挂单中订单。
func (s *Store) ListPendingOrders() ([]model.Order, error) {
	return s.listOrdersByStatus(model.OrderStatusPending)
}

func (s *Store) listOrdersByStatus(status string) ([]model.Order, error) {
	rows, err := s.db.Query(`SELECT id,secid,code,name,side,order_type,price,quantity,filled_qty,filled_price,status,fee,reason,created_at,updated_at FROM trade_order WHERE status=? ORDER BY id DESC`, status)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.Order
	for rows.Next() {
		var o model.Order
		var cts, uts int64
		if err := rows.Scan(&o.ID, &o.SecID, &o.Code, &o.Name, &o.Side, &o.OrderType, &o.Price, &o.Quantity, &o.FilledQty, &o.FilledPrice, &o.Status, &o.Fee, &o.Reason, &cts, &uts); err != nil {
			return nil, err
		}
		o.CreatedAt = time.Unix(cts, 0)
		o.UpdatedAt = time.Unix(uts, 0)
		out = append(out, o)
	}
	return out, rows.Err()
}

// ============ 成交 ============

// RecordTrade 落库成交记录。
func (s *Store) RecordTrade(t *model.Trade) (int64, error) {
	if t.TradedAt.IsZero() {
		t.TradedAt = time.Now()
	}
	res, err := s.db.Exec(
		`INSERT INTO trade(order_id,secid,code,name,side,price,quantity,amount,fee,traded_at) VALUES(?,?,?,?,?,?,?,?,?,?)`,
		t.OrderID, t.SecID, t.Code, t.Name, t.Side, t.Price, t.Quantity, t.Amount, t.Fee, t.TradedAt.Unix(),
	)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// ListTrades 最近成交（按时间降序）。
func (s *Store) ListTrades(limit int) ([]model.Trade, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	rows, err := s.db.Query(`SELECT id,order_id,secid,code,name,side,price,quantity,amount,fee,traded_at FROM trade ORDER BY id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.Trade
	for rows.Next() {
		var t model.Trade
		var ts int64
		if err := rows.Scan(&t.ID, &t.OrderID, &t.SecID, &t.Code, &t.Name, &t.Side, &t.Price, &t.Quantity, &t.Amount, &t.Fee, &ts); err != nil {
			return nil, err
		}
		t.TradedAt = time.Unix(ts, 0)
		out = append(out, t)
	}
	return out, rows.Err()
}
