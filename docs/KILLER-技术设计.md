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
        │       └── source 接口：按功能路由（指数 eastmoney/sina 双源互备，板块腾讯独占，失败自动切换）
        │
        └── web.Server ── net/http（Go 1.22+ 路由模式）
                ├── /api/* JSON 接口（读 store + watchdog 内存态）
                └── / 内嵌单页面板（//go:embed static，10 秒轮询刷新）
```

核心数据流：**看门狗轮询 → 行情快照入库 + 阈值告警 → 前端定时拉取 API 渲染**。所有指数列表、阈值、间隔均来自 config.yaml，代码不硬编码。

> 2026-11-13 起，监控标的从「指数 + 板块」扩为「指数 + 板块 + ETF」三类，分别走指数路由（东财/新浪）、腾讯板块（pt 代码）、腾讯批量（ETF），三类独立拉取互不拖累。

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
| secid | TEXT | 指数/板块/ETF secid（1.x=上交所 0.x=深交所 pt0xxx=腾讯板块；ETF 沪 1.5xxxxx 深 0.159xxxx 等） |
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

### setting（键值设置）
| 字段 | 类型 | 说明 |
|---|---|---|
| key | TEXT PK | 设置键（如 `routing`） |
| value | TEXT | JSON 值；`routing` 存功能→数据源映射，启动时恢复到内存 |

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
| GET | /api/boards?kind=industry\|concept&sort= | A股板块（腾讯首选→新浪/东财兜底，缓存 60s） |
| GET | /api/board-members?secid= | 板块成分股（腾讯 pt 代码首选，东财 90.BK 兜底） |
| GET | /api/stocks?page=&sort=&asc= | A股个股分页（每页50，缓存 15s） |
| GET | /api/etfs?page=&sort=&asc= | A股 ETF 分页（东财 clist 独占，每页50，缓存 15s） |
| GET | /api/etf-quotes | 监控 ETF 最新行情（按 config.etfs 顺序，看门狗巡检内存态） |
| GET | /api/etf-groups | 市场 ETF 组合（按 config.etf_groups 分组，汇总 secid 去重后一次腾讯批量拉行情再分组返回，缓存 10s；行情失败降级返回组结构） |
| GET | /api/us/overview | 美股三大指数 + SPDR 11 板块一次批量（缓存 10s） |
| GET | /api/us/stocks?page=&sort=&market= | 美股活跃榜分页（东财独占） |
| GET | /api/ndx | 纳指100 成分+批量行情（成分缓存 6h，行情 10s；2026-11-13 起由 /us.html 个股区「纳指100 成分」tab 调用，不再有独立页面） |
| GET/POST/DELETE | /api/watch[?secid=] | 自选个股增删查（附最新行情） |

### 数据源治理模块（2026-11-12 新增）
| 方法 | 路径 | 说明 |
|---|---|---|
| GET | /api/datasources | 数据源清单 + 9 功能能力矩阵 + 当前路由 + 连通状态（60s 缓存，过期自动重测） |
| POST | /api/datasources/test | 测试连接：空 body 并行探测全部 4 源；`{"id":x}` 只探测指定源（其余状态原样返回）；两种均返回与 GET 同构的完整数据 |
| GET | /api/datasources/routing | 当前功能→数据源映射 |
| POST | /api/datasources/routing | 更新路由：校验源是否支持该功能（非法 400）→ 内存热生效 → setting 表持久化 |

**路由机制**：`source` 包维护路由表（RWMutex 保护，可热更新），各功能入口（看门狗指数行情、板块/个股/ETF/批量行情包装函数、日 K handler）按 `pairFor(feature)` 得到「首选→备选」顺序依次尝试。单一来源功能（当日分时/美股活跃榜/成分表/ETF 列表）锁定；美股日 K 因新浪 `getKLineData` 仅支持 sh/sz 而固定东财。启动时 `web.LoadRouting` 从 setting 表恢复。

**默认路由**（2026-11-13 板块彻底脱离东财后调整，`Features[].Sources[0]` 即默认首选）：板块 → 腾讯（列表/行情/成分股/日K/分时全走 pt 代码），日K/个股列表 → 新浪，批量行情 → 腾讯，指数行情 quotes → 东财（指数 push2 可用性优于板块），当日分时 → 东财首选+腾讯兜底（2026-11-13 加，修复东财限流期分时图 502），ETF 列表 → 东财独占，成分表 → yfiua。

**能力矩阵**（实测边界）：东财 = 除成分表外全部（2026-11-12 实测 push2 连接失败，降为兜底）；新浪 = 指数行情/日K/板块/个股列表/批量行情（无分钟分时、无美股列表、无 100.* 指数、无板块成分股、无 ETF 列表）；腾讯 = 批量行情+板块+分时/K线（列表/行情/成分股/日K/分时，A股/美股/ETF/100.* 指数通吃）；yfiua = 仅纳指100成分。

前端页面：`/`（大盘）、`/boards.html`、`/stocks.html`、`/etfs.html`（ETF 看板）、`/us.html`（美股全景，2026-11-13 起合并原 `/ndx.html`：三大指数卡片 + SPDR 板块 + 个股区双 tab「成交活跃榜 / 纳指100 成分」共用详情抽屉，`?tab=ndx` 直达成分 tab）、`/trading.html`、`/datasource.html`（数据源看板），统一导航 market.css + common.js。原 `/ndx.html` 已删除（旧链接 404），导航不再单列「纳指100」。

## 模块依赖

```
cmd/killer → config, monitor, store, web
monitor     → config, model, source, store
web         → cache, config, model, monitor(只读), source, store
source      → model（东财 + 新浪 + 腾讯三源 + NDX 成分外部源 + 路由/探测治理）
store       → model（含 setting 表：路由持久化）
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
- **阈值分流**：板块用 `board_change_pct`（默认 2.0%）、ETF 用 `etf_change_pct`（默认 2.0%，基金波动小于指数和个股）；不配则沿用 `change_pct`
- **冷却**：同 secid + 同类型，距上次告警 < cooldown（默认 30min）则跳过（查 alert 表）
- **交易时段**：`trading_hours_only=true` 时仅在北京时间周一至五 9:15–11:35 / 13:00–15:05 触发告警（快照照常落库）；时区加载失败兜底 UTC+8
- **容灾**：三类标的分别按路由尝试——指数（东财/新浪路由）、板块（腾讯 pt 代码，固定）、ETF（腾讯批量，固定）；任一类失败不拖累另两类
- **自选个股巡检**（2026-11-11 新增）：每轮随大盘巡检一并检查 `watch_stock`，|涨跌幅| ≥ 个股独立阈值 → `stock_change_up/down` 告警，冷却与指数共用（同 secid+类型）

## 技术约束
- 禁止 CGO：必须保持 `go build` 免 C 编译器（驱动选型已保证）
- 单进程单写者：SQLite 连接数固定为 1，避免 SQLITE_BUSY
- 免费公开接口：东财/新浪/腾讯均无 SLA，需容灾与降级，接口结构变动需先 curl 实测再改代码
- **上游限流教训**：2026-11-11 密集 curl 探测触发东财 IP 级限流（push2 全线 EOF 超 1 小时），因此新增 `cache` 包对上游请求做 TTL 节流（板块 60s / 个股 15s / 行情 10s / 成分 6h），前端轮询不直接放大到上游
- 国内网络：Go 模块拉取走 goproxy.cn（已 `go env -w` 设置）；海外源仅 GitHub Pages（yfiua）经实测可达，nasdaq/wikipedia/yahoo 等均不可达
- 静态资源必须带 `Cache-Control: no-store`：内嵌页面无 ETag/Last-Modified，浏览器曾缓存旧 JS 导致"改了没生效"的假 bug（2026-11-12 实例）

## 第三方集成
- **东方财富 push2 / push2his**：行情 ulist、列表 clist（含 A股/ETF）、日 K/分钟 K kline（2026-11-11 实测通过）；2026-11-12 实测 push2 系列连接失败（http=000/EOF），降为兜底，恢复后自动回到部分首选；ETF 列表 fs=`m:1+t:19,m:0+t:13`（沪 ETF t:19 + 深基金/ETF t:13，深 t:13 含 LOF/开放基金用 IsETFSecid 过滤）
- **新浪（sina.go / sina_ext.go）**：
  - `hq.sinajs.cn` 批量行情 GBK（sh/sz 个股全字段 + gb_ 美股 + sh510xxx/sz159xxx ETF），A股/美股/ETF 混排实测通过
  - `Market_Center.getHQNodeData` A股列表（排序+分页，5341 只，注意数值字段 JSON 数字/字符串混排需 flexF 兼容）；ETF 节点（etf_fund/fund_etf 等）实测返回空，新浪不支持 ETF 列表
  - `q/view/newSinaHy.php`（行业板块）与 `newFLJK.php?param=class`（概念板块），GBK JS 赋值，无板块内涨跌家数
  - `hq.sinajs.cn` 的 s_ 指数接口：大盘监控备用源
  - `CN_MarketDataService.getKLineData` 日K（scale=240；2026-11-12 起 OHLCV 改为字符串返回，sinaKlineBar 用 flexF 兼容两种形态）；symbol 接受 sh510xxx/sz159xxx，ETF 日 K 可拉
- **腾讯财经（tencent.go / tencent_board.go，2026-11-12 新增，2026-11-13 接入板块+分时降级）**：
  - `qt.gtimg.cn/q=` 批量行情 GBK（~ 分隔 88 字段；A股 [35] 三段式成交额、美股 [37] 兜底；`usDJI/usNDX/usINX` 为 100.* 美股指数专用码；`pt0xxx` 为板块代码，直接作 symbol 透传；ETF 直接用 sh510xxx/sz159xxx symbol 通吃）——已接入批量行情路由首选、ETF 行情、探测看板、板块行情
  - `web.ifzq.gtimg.cn` 日K（fqkline，个股 qfqday/指数 day 双键，ETF 同个股走 qfqday）与当日分时（minute/query，date 为空或不足 2 条报错切源；A 股/ETF/板块均支持）——已接线 server.go（指数/板块/个股/ETF 日K与分时），2026-11-13 起作为东财分时失败兜底
  - `proxy.finance.qq.com/cgi/cgi-bin/rank/pt/getRank?board_type=hy|gn` 板块列表（JSON，含 zgb 涨跌家数/领涨股/市值）
  - `proxy.finance.qq.com/cgi/cgi-bin/rank/hs/getBoardRankList?board_code=` 板块成分股（JSON，含现价/涨跌幅/换手/流通市值/总市值/PE）
  - `proxy.finance.qq.com/ifzqgtimg/appstock/app/newfqkline/get` 板块日K（fqkline 对板块仅返回 1 天，newfqkline 才有 120 天历史；每条末尾带额外字段，用 json.RawMessage 兼容）
  - 板块代码命名：行业 `pt0180xxxx`、概念 `pt02GNxxxx`/`pt0210xxxx`，统一用 `pt` 前缀识别（`source.IsBoardSecid`）
  - 响应必须 `io.ReadAll`：单次 `Body.Read` 在 101 只批量（约 50KB）下因 TCP 分段只读到部分（实测只回 84/101）
- **yfiua/index-constituents（GitHub Pages）**：纳斯达克100 成分表（101 只，月度更新，Symbol 为 Yahoo 风格）
- 注：均为免费公开接口，无鉴权、无配额保证，仅用于学习研究；东财限流期四个东财独占接口（美股指数行情/活跃榜/日K、ETF 列表）优雅降级（占位提示/502/图表报错/列表加载失败），恢复后自动可用；A 股/ETF 分时图 2026-11-13 起走东财首选+腾讯兜底，东财限流期不再 502
