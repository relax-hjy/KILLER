# KILLER — 大盘监控系统（Go 版）

本项目是原 **Fund Killer（基金筛选与监控系统）** 的 Go 重写版。
核心功能从「基金筛选 + 净值监控」改为 **股票大盘指数的实时监控与阈值告警**。

## 功能

- **大盘指数实时监控** — 上证指数、深证成指、创业板指、沪深300、科创50（可在 config.yaml 增删）
- **双数据源容灾** — 主源东方财富 push2，失败自动切换新浪 hq（GBK 解析）
- **阈值告警** — 涨跌幅超限、当日振幅超限触发告警；同类告警带冷却时间防刷屏
- **仅交易时段告警** — 北京时间 9:15–11:35 / 13:00–15:05（含集合竞价），周末休市
- **行情快照落库** — SQLite（modernc 纯 Go 驱动，免 CGO），支撑分时走势回看
- **Web 面板** — 单文件 HTML（内嵌于二进制），指数卡片、分时/日 K 切换、告警记录

## 快速开始

```bash
cd go
go build ./...
./killer.exe                 # Windows；Linux/macOS 为 ./killer
```

默认监听 <http://localhost:8080>，配置文件 `config.yaml`（缺失时用内置默认值）。

## 配置要点（config.yaml）

```yaml
server:
  addr: ":8080"
monitoring:
  check_interval_seconds: 60   # 轮询间隔
  trading_hours_only: true     # 仅交易时段告警
  alert:
    change_pct: 1.0            # 涨跌幅阈值 ±1%
    amplitude_pct: 1.5         # 振幅阈值 1.5%
    cooldown_minutes: 30       # 同类告警冷却
indices:                       # 监控列表
  - { secid: "1.000001", name: "上证指数" }
```

`secid` 规则：`1.xxxxxx` = 上交所，`0.xxxxxx` = 深交所。

## API

| 路径 | 说明 |
|------|------|
| `GET /api/quotes` | 全部指数最新行情 |
| `GET /api/history?secid=&hours=` | 分时快照走势 |
| `GET /api/kline?secid=&limit=` | 日 K 线 |
| `GET /api/alerts?n=` | 最近告警 |
| `GET /api/status` | 巡检状态（数据源/错误/交易时段） |

## 项目结构

```
go/
├── config.yaml                  # 全局配置
├── cmd/killer/main.go           # 入口：装配 config→store→watchdog→web
├── internal/
│   ├── config/                  # YAML 配置加载
│   ├── model/                   # Quote / Kline / Alert 数据结构
│   ├── source/                  # eastmoney（主）、sina（备）
│   ├── store/                   # SQLite：quote_snap、alert 两张表
│   ├── monitor/                 # 看门狗：轮询、阈值比对、冷却
│   └── web/                     # HTTP API + 内嵌单页面板
└── tests 在各包 *_test.go
```

## 与 Python 原版的对应关系

| 原版 (Python) | Go 版 |
|---------------|-------|
| AkShare 基金数据源 | eastmoney/sina 大盘数据源 |
| APScheduler 定时巡检 | time.Ticker 轮询 |
| fund/nav_record/watch_item 三表 | quote_snap/alert 两表 |
| Streamlit 面板 | 内嵌 HTML + JSON API |
| 基金净值 ±2% 告警 | 指数涨跌幅/振幅阈值告警（含冷却） |

## License

MIT
