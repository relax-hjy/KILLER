# KILLER 技术设计

## 架构概览

```
config.yaml ──→ config 包（YAML 加载，默认值兜底）
                    │
                    ▼
cmd/killer/main.go ── 装配
        │
        ├── store（SQLite: quote_snap / alert）
        │
        ├── monitor.Watchdog ── 定时轮询（time.Ticker）
        │       │  拉行情 → 快照落库 → 阈值比对 → 冷却判断 → 告警落库
        │       └── source 接口：eastmoney（主）→ 失败自动切 sina（备）
        │
        └── web.Server ── net/http（Go 1.22+ 路由模式）
                ├── /api/* JSON 接口（读 store + watchdog 内存态）
                └── / 内嵌单页面板（//go:embed static，10 秒轮询刷新）
```

核心数据流：**看门狗轮询 → 行情快照入库 + 阈值告警 → 前端定时拉取 API 渲染**。所有指数列表、阈值、间隔均来自 config.yaml，代码不硬编码。

## 技术栈
- 前端：原生 HTML/CSS/JS 单文件，`//go:embed` 内嵌，无构建步骤、无第三方 JS 库（图表为手写 canvas 折线）
- 后端：Go 1.27，标准库 net/http / encoding/json / context，无 Web 框架
- 数据库：SQLite（modernc.org/sqlite 纯 Go 驱动，WAL + busy_timeout，MaxOpenConns=1）
- 配置：gopkg.in/yaml.v3
- 编码：golang.org/x/text（新浪 GBK 解码）
- 部署：单二进制（`go build` 产物自带配置外全部资源），Windows/Linux 皆可

## 数据模型

### quote_snap（行情快照）
| 字段 | 类型 | 说明 |
|---|---|---|
| id | INTEGER PK | 自增 |
| secid | TEXT | 指数 secid（1.x=上交所 0.x=深交所） |
| name | TEXT | 指数名称 |
| price | REAL | 点位 |
| pct | REAL | 涨跌幅（%） |
| amplitude | REAL | 振幅（%） |
| ts | INTEGER | Unix 秒；索引 (secid, ts) |

### alert（告警记录）
| 字段 | 类型 | 说明 |
|---|---|---|
| id | INTEGER PK | 自增 |
| secid / name | TEXT | 指数/个股标识 |
| type | TEXT | change_up / change_down / amplitude / stock_change_up / stock_change_down |
| value | REAL | 触发值（%） |
| threshold | REAL | 阈值（%） |
| price | REAL | 触发时点位 |
| triggered_at | INTEGER | Unix 秒；索引 (secid, type, triggered_at) 供冷却查询 |

### watch_stock（自选个股）
| 字段 | 类型 | 说明 |
|---|---|---|
| secid | TEXT PK | "1.600519" / "105.AAPL" |
| code / name | TEXT | 代码、名称 |
| threshold | REAL | 日涨跌告警阈值（%），默认 5–7 |
| added_at | INTEGER | Unix 秒 |
| notes | TEXT | 备注 |

### 内存结构（model 包）
- `Quote`：指数实时行情（含振幅，供告警）
- `Kline`：日 K / 分时分钟线（date/open/close/high/low/volume）
- `Alert`：告警（含 String() 格式化）
- `Board`：A股板块（名称/点位/涨跌幅/板块内涨跌家数；新浪兜底时家数为 -1）
- `Stock`：个股行情通用结构（A股/美股，含成交额/换手/PE/PB/市值）
- `WatchStock`：自选个股（含独立阈值）

## API 设计

### 行情模块
| 方法 | 路径 | 说明 |
|---|---|---|
| GET | /api/quotes | 全部指数最新行情（按配置顺序） |
| GET | /api/history?secid=&hours= | 分时快照（本地轮询落库，默认 4h，上限 7 天，升序；现为辅助/调试用） |
| GET | /api/intraday?secid= | 当日分时（数据源 1 分钟 K，全天 9:31–15:00，休市/重启可看；分时图数据源） |
| GET | /api/kline?secid=&limit= | 日 K（默认 120，上限 500，东财独占） |

### 监控模块
| 方法 | 路径 | 说明 |
|---|---|---|
| GET | /api/alerts?n= | 最近告警（默认 50，上限 200，降序） |
| GET | /api/status | 巡检状态：数据源、最近错误、间隔、交易时段 |
| GET | / | 内嵌大盘面板 |

### 市场模块（2026-11-11 新增）
| 方法 | 路径 | 说明 |
|---|---|---|
| GET | /api/boards?kind=industry\|concept&sort= | A股板块（东财→新浪兜底，缓存 60s） |
| GET | /api/stocks?page=&sort=&asc= | A股个股分页（每页50，缓存 15s） |
| GET | /api/us/overview | 美股三大指数 + SPDR 11 板块一次批量（缓存 10s） |
| GET | /api/us/stocks?page=&sort=&market= | 美股活跃榜分页（东财独占） |
| GET | /api/ndx | 纳指100 成分+批量行情（成分缓存 6h，行情 10s） |
| GET/POST/DELETE | /api/watch[?secid=] | 自选个股增删查（附最新行情） |

前端页面：`/`（大盘）、`/boards.html`、`/stocks.html`、`/us.html`、`/ndx.html`，统一导航 market.css + common.js。

## 模块依赖

```
cmd/killer → config, monitor, store, web
monitor     → config, model, source, store
web         → cache, config, model, monitor(只读), source, store
source      → model（东财主源 + 新浪兜底 + NDX 成分外部源）
store       → model
cache       → （无依赖，TTL 缓存，保护上游接口）
config / model → （无内部依赖）
```

单向依赖，无环；web 通过 watchdog 的 `Latest()/Status()` 只读访问监控态，不反向调用。

## 监控逻辑要点（monitor 包）

- **轮询**：`time.Ticker`（默认 60s），启动立即执行一轮；单轮 context 超时 30s
- **告警条件**（三类并行比对）：
  - `change_up`：pct ≥ +change_pct（默认 1.0%）
  - `change_down`：-pct ≥ change_pct
  - `amplitude`：振幅 ≥ amplitude_pct（默认 1.5%，按 (high-low)/prev_close 计）
- **冷却**：同 secid + 同类型，距上次告警 < cooldown（默认 30min）则跳过（查 alert 表）
- **交易时段**：`trading_hours_only=true` 时仅在北京时间周一至五 9:15–11:35 / 13:00–15:05 触发告警（快照照常落库）；时区加载失败兜底 UTC+8
- **容灾**：主源失败记入 `last_error` 并切备源；备源新浪 s_ 接口无开高低昨收，振幅置 0（尽力而为）
- **自选个股巡检**（2026-11-11 新增）：每轮随大盘巡检一并检查 `watch_stock`，|涨跌幅| ≥ 个股独立阈值 → `stock_change_up/down` 告警，冷却与指数共用（同 secid+类型）

## 技术约束
- 禁止 CGO：必须保持 `go build` 免 C 编译器（驱动选型已保证）
- 单进程单写者：SQLite 连接数固定为 1，避免 SQLITE_BUSY
- 免费公开接口：东财/新浪均无 SLA，需容灾与降级，接口结构变动需先 curl 实测再改代码
- **上游限流教训**：2026-11-11 密集 curl 探测触发东财 IP 级限流（push2 全线 EOF 超 1 小时），因此新增 `cache` 包对上游请求做 TTL 节流（板块 60s / 个股 15s / 行情 10s / 成分 6h），前端轮询不直接放大到上游
- 国内网络：Go 模块拉取走 goproxy.cn（已 `go env -w` 设置）；海外源仅 GitHub Pages（yfiua）经实测可达，nasdaq/wikipedia/yahoo 等均不可达

## 第三方集成
- **东方财富 push2 / push2his**：主数据源（实时行情 ulist、列表 clist、日 K/分钟 K kline），2026-11-11 实测通过；限流期兜底
- **新浪（兜底层 sina_ext.go）**：
  - `hq.sinajs.cn` 批量行情 GBK（sh/sz 个股全字段 + gb_ 美股），A股/美股/ETF 混排实测通过
  - `Market_Center.getHQNodeData` A股列表（排序+分页，5341 只，注意数值字段 JSON 数字/字符串混排需 flexF 兼容）
  - `q/view/newSinaHy.php`（行业板块）与 `newFLJK.php?param=class`（概念板块），GBK JS 赋值，无板块内涨跌家数
  - `hq.sinajs.cn` 的 s_ 指数接口：大盘监控备用源
- **yfiua/index-constituents（GitHub Pages）**：纳斯达克100 成分表（101 只，月度更新，Symbol 为 Yahoo 风格）
- 注：均为免费公开接口，无鉴权、无配额保证，仅用于学习研究；东财限流期三个独占接口（美股指数行情/活跃榜/日K）优雅降级（占位提示/502/图表报错），恢复后自动可用
