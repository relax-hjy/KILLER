// Package store 封装 SQLite 存储（modernc 纯 Go 驱动，无 CGO 依赖）。
//
// 两张表：
//   - quote_snap: 指数行情快照（时间序列，供走势与回看）
//   - alert:      告警记录（含冷却判断依据）
package store

import (
	"database/sql"
	"fmt"
	"time"

	"killer/internal/model"

	_ "modernc.org/sqlite" // 纯 Go SQLite 驱动
)

type Store struct {
	db *sql.DB
}

// Open 打开（或创建）SQLite 数据库并建表。
func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path+"?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)")
	if err != nil {
		return nil, fmt.Errorf("打开数据库失败: %w", err)
	}
	// SQLite 单写者：限制为 1 连接避免 SQLITE_BUSY
	db.SetMaxOpenConns(1)

	schema := `
CREATE TABLE IF NOT EXISTS quote_snap (
	id         INTEGER PRIMARY KEY AUTOINCREMENT,
	secid      TEXT    NOT NULL,
	name       TEXT    NOT NULL,
	price      REAL    NOT NULL,
	pct        REAL    NOT NULL,
	amplitude  REAL    NOT NULL DEFAULT 0,
	ts         INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_snap_secid_ts ON quote_snap(secid, ts);

CREATE TABLE IF NOT EXISTS alert (
	id           INTEGER PRIMARY KEY AUTOINCREMENT,
	secid        TEXT    NOT NULL,
	name         TEXT    NOT NULL,
	type         TEXT    NOT NULL,
	value        REAL    NOT NULL,
	threshold    REAL    NOT NULL,
	price        REAL    NOT NULL,
	triggered_at INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_alert_secid_type_ts ON alert(secid, type, triggered_at);

CREATE TABLE IF NOT EXISTS watch_stock (
	secid     TEXT PRIMARY KEY,
	code      TEXT    NOT NULL,
	name      TEXT    NOT NULL DEFAULT '',
	threshold REAL    NOT NULL DEFAULT 5.0,
	added_at  INTEGER NOT NULL,
	notes     TEXT    NOT NULL DEFAULT ''
);

CREATE TABLE IF NOT EXISTS setting (
	key   TEXT PRIMARY KEY,
	value TEXT NOT NULL
);
`
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("建表失败: %w", err)
	}
	return &Store{db: db}, nil
}

func (s *Store) Close() error { return s.db.Close() }

// SaveQuote 落库一条行情快照。
func (s *Store) SaveQuote(q model.Quote) error {
	_, err := s.db.Exec(
		`INSERT INTO quote_snap(secid,name,price,pct,amplitude,ts) VALUES(?,?,?,?,?,?)`,
		q.SecID, q.Name, q.Price, q.Pct, q.Amplitude, q.UpdateTime.Unix(),
	)
	return err
}

// RecentQuotes 返回某指数最近 limit 条快照（按时间升序）。
func (s *Store) RecentQuotes(secid string, since time.Time, limit int) ([]model.Quote, error) {
	rows, err := s.db.Query(
		`SELECT secid,name,price,pct,amplitude,ts FROM quote_snap
		 WHERE secid=? AND ts>=? ORDER BY ts DESC LIMIT ?`,
		secid, since.Unix(), limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []model.Quote
	for rows.Next() {
		var q model.Quote
		var ts int64
		if err := rows.Scan(&q.SecID, &q.Name, &q.Price, &q.Pct, &q.Amplitude, &ts); err != nil {
			return nil, err
		}
		q.UpdateTime = time.Unix(ts, 0)
		out = append(out, q)
	}
	// 升序返回
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out, rows.Err()
}

// SaveAlert 落库一条告警，返回自增 ID。
func (s *Store) SaveAlert(a model.Alert) (int64, error) {
	res, err := s.db.Exec(
		`INSERT INTO alert(secid,name,type,value,threshold,price,triggered_at) VALUES(?,?,?,?,?,?,?)`,
		a.SecID, a.Name, a.Type, a.Value, a.Threshold, a.Price, a.TriggeredAt.Unix(),
	)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// LastAlertTime 返回某指数某类告警最后一次触发时间（无记录返回零值）。
// 用于告警冷却判断。
func (s *Store) LastAlertTime(secid, alertType string) (time.Time, error) {
	var ts int64
	err := s.db.QueryRow(
		`SELECT triggered_at FROM alert
		 WHERE secid=? AND type=? ORDER BY triggered_at DESC LIMIT 1`,
		secid, alertType,
	).Scan(&ts)
	if err == sql.ErrNoRows {
		return time.Time{}, nil
	}
	if err != nil {
		return time.Time{}, err
	}
	return time.Unix(ts, 0), nil
}

// RecentAlerts 返回最近 n 条告警（按时间降序）。
func (s *Store) RecentAlerts(n int) ([]model.Alert, error) {
	rows, err := s.db.Query(
		`SELECT id,secid,name,type,value,threshold,price,triggered_at
		 FROM alert ORDER BY triggered_at DESC LIMIT ?`, n,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []model.Alert
	for rows.Next() {
		var a model.Alert
		var ts int64
		if err := rows.Scan(&a.ID, &a.SecID, &a.Name, &a.Type, &a.Value, &a.Threshold, &a.Price, &ts); err != nil {
			return nil, err
		}
		a.TriggeredAt = time.Unix(ts, 0)
		out = append(out, a)
	}
	return out, rows.Err()
}

// ==========================================
// WatchStock 自选个股
// ==========================================

// AddWatchStock 添加/更新自选个股。
func (s *Store) AddWatchStock(w model.WatchStock) error {
	if w.Threshold <= 0 {
		w.Threshold = 5.0
	}
	_, err := s.db.Exec(
		`INSERT INTO watch_stock(secid,code,name,threshold,added_at,notes) VALUES(?,?,?,?,?,?)
		 ON CONFLICT(secid) DO UPDATE SET name=excluded.name, threshold=excluded.threshold, notes=excluded.notes`,
		w.SecID, w.Code, w.Name, w.Threshold, time.Now().Unix(), w.Notes,
	)
	return err
}

// RemoveWatchStock 删除自选个股。
func (s *Store) RemoveWatchStock(secid string) error {
	_, err := s.db.Exec(`DELETE FROM watch_stock WHERE secid=?`, secid)
	return err
}

// ListWatchStocks 全部自选个股（按添加时间升序）。
func (s *Store) ListWatchStocks() ([]model.WatchStock, error) {
	rows, err := s.db.Query(
		`SELECT secid,code,name,threshold,added_at,notes FROM watch_stock ORDER BY added_at ASC`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.WatchStock
	for rows.Next() {
		var w model.WatchStock
		var ts int64
		if err := rows.Scan(&w.SecID, &w.Code, &w.Name, &w.Threshold, &ts, &w.Notes); err != nil {
			return nil, err
		}
		w.AddedAt = time.Unix(ts, 0)
		out = append(out, w)
	}
	return out, rows.Err()
}

// ==========================================
// Setting 键值设置（数据源路由等持久化）
// ==========================================

// GetSetting 读取设置值（不存在返回空串）。
func (s *Store) GetSetting(key string) (string, error) {
	var v string
	err := s.db.QueryRow(`SELECT value FROM setting WHERE key=?`, key).Scan(&v)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return v, err
}

// SetSetting 写入设置值（upsert）。
func (s *Store) SetSetting(key, value string) error {
	_, err := s.db.Exec(
		`INSERT INTO setting(key,value) VALUES(?,?)
		 ON CONFLICT(key) DO UPDATE SET value=excluded.value`,
		key, value,
	)
	return err
}
