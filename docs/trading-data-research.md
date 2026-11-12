# 交易系统数据源调研报告

> 调研时间：2026-11-12 | 适用场景：交易/监控系统的行情与数据源选型
> 姊妹篇：[data-research.md](data-research.md)（2025-06，公募基金方向）

---

## 一、调研范围

做交易系统，数据源按链路分四层，缺一层系统就不完整：

| 层 | 用途 | 关键指标 |
|----|------|---------|
| ① 实时行情 | 盘中监控、下单决策 | 延迟、限流政策、推送方式（轮询/WebSocket） |
| ② 历史数据 | 回测、因子计算 | 深度（年）、粒度（日/分钟/tick）、复权 |
| ③ 交易柜台 | 下单、撤单、查持仓 | 券商接入门槛、API 稳定性 |
| ④ 参考数据 | 财务、板块、成分股 | 字段规范度、更新频率 |

本次按 **A股免费源 → 量化数据平台 → 券商柜台 → 期货 → 美股/加密 → 商业源** 六个方向调研。

---

## 二、A股实时行情（免费 HTTP 源）

这是 KILLER 现役所在的层（东财主源+新浪备源）。除这两家外，可选的还有：

### 2.1 腾讯行情 `qt.gtimg.cn`

| 属性 | 说明 |
|------|------|
| 示例 | `https://qt.gtimg.cn/q=sh000001,sz399001` |
| 费用 | 完全免费，无需注册 |
| 数据 | 实时报价（分笔快照）、日K/分钟K（`web.ifzq.gtimg.cn`） |
| 特点 | 老牌源，量化社区（easyquotation 等）广泛使用，字段稳定 |

- ✅ 支持批量查询，一次拉几十个代码
- ✅ 与新浪/东财互为独立基础设施，适合做第三备源
- ❌ 无官方文档，字段含义靠社区逆向
- ❌ 高频轮询同样会触发腾讯侧限流

### 2.2 网易行情 `api.money.126.net`

- 免费、老牌，日K/分钟K历史数据完整（曾是 akshare 早期底层源）
- ❌ 近年接口时有下线，稳定性不如腾讯，只适合做历史数据补充

### 2.3 通达信行情协议（pytdx）

| 属性 | 说明 |
|------|------|
| 协议 | 通达信客户端私有 TCP 协议，Python 实现 pytdx |
| 数据 | 实时报价、1/5分钟K、日K、分笔成交、分时历史 |
| 费用 | 免费（需自备可用行情服务器 IP） |

- ✅ 数据种类最全的免费方案之一，**分笔成交**都有
- ❌ 需要自己维护行情服务器 IP 列表（会失效）
- ❌ 协议逆向产物，无 SLA；Go 生态有对应实现但社区小于 Python
- ❌ 违反通达信软件服务条款的风险自担

### 2.4 交易所官方通道

- **上交所 Level-1/Level-2**：上证所信息网络（sseinfo.com）提供，L2 含逐笔成交/逐笔委托、10 档，上交所 L2 为 3 秒快照推送
- **深交所行情互联网接入服务**：面向"已授权基础行情客户"，有正式接口说明书（LDDS）
- ❌ 面向持牌机构，个人开发者基本无法直接接入；通达信等终端的行情授权是间接途径

### 2.5 本项目实战教训（KILLER）

- **东财 push2 对密集探测会 IP 级限流**（2026-11-12 实测：全线 EOF 超 1 小时才恢复），任何单一免费源都不能当唯一依赖
- 结论：**三源互备**（东财 + 腾讯 + 新浪）+ TTL 缓存 + 指数级退避，是免费方案的可靠形态
- 探测/压测要克制，监控类系统正常轮询频率（≥3s）远低于危险线

---

## 三、量化数据平台（历史 + 分钟 + 部分实时）

### 3.1 Tushare Pro（tushare.pro）

| 维度 | 现状（2026） |
|------|-------------|
| 收费 | 积分制：注册 100 分，做任务/推荐攒分；**实时行情、分钟线等关键接口需要高积分**，完整能力约 2000 元/年 |
| 数据 | 日线 2005 至今、分钟线、实时分钟（1/5/15/30/60，月限 1000 次）、财务、宏观、指数成分 |
| 接口 | HTTP + Python SDK，字段规范，社区教程最多 |

- ✅ 数据标准化程度最高，A股事实标准之一
- ❌ 免费额度形同虚设（实时接口基本要付费）；只有原始行情，技术指标需自算
- Go 接入：HTTP 接口直接调，无障碍

### 3.2 AkShare

- 完全免费开源（MIT），底层是东财/天天基金/新浪等公开页面
- ✅ 接口最全、零成本；❌ 爬虫本质，网站改版即失效，高频被反爬
- 定位：**研究/补数据**，不做生产实时链路

### 3.3 Baostock

- 完全免费、无需注册；5 年以上日线、复权因子完整
- ✅ 回测历史数据补充；❌ 无实时、无分钟级更新、服务多年未大更新

### 3.4 聚宽 JQData / 米筐 RQData

| 平台 | 收费 | 特点 |
|------|------|------|
| JQData（聚宽 jqdatasdk） | 试用约 3 个月（一手机号一次），之后按量收费 | 数据与聚宽研究环境同源，字段全 |
| RQData（米筐 rqdatac） | 标准版约 3000 元/年（分钟线），进阶版约 1 万元/年（含 tick） | 期货 tick 性价比高，支持实时分钟订阅 |

- ✅ 质量高于免费源，有授权保障
- ❌ 与平台策略环境绑定较深，试用到期数据即断；需落库本地化才能脱离平台跑
- Go 接入：RQData/JQData 主打 Python SDK，HTTP 层面需自己封

### 3.5 其他

| 名称 | 费用 | 定位 |
|------|------|------|
| 必盈 API | 按调用量计费 | 有逐笔和实时行情（不含 L2），字段以官网为准 |
| iTick | 免费额度 + 付费 | 全球市场 REST+WebSocket，覆盖美股/港股/A股 |
| 散户量化 API / 智兔数服 | 免费测试 | 小众，字段稳定性待验证 |

---

## 四、券商柜台（下单链路）

交易系统的"交易"部分只有三条主流路：

### 4.1 QMT / xtQuant（迅投）

- 券商采购迅投 QMT，本地客户端 + `xtQuant` 外置 Python 库
- ✅ 灵活性最高，可接第三方数据源，策略本地运行
- ❌ **2026 年最大变动：miniQMT（散户独立接入通道）已全面停止申请**（券商侧 2026-07 起陆续执行），存量逐步清退；新规划不应再走 miniQMT 路线，直接谈 QMT 权限
- ❌ Python 绑定为主，Go 需自行桥接

### 4.2 PTrade（恒生）

- 策略托管在券商服务器端，集成度高、门槛低
- ✅ 适合新手、不用盯本地机器；❌ 灵活性差，外部数据源接入受限

### 4.3 CTP（期货）

- 期货事实标准，行情+交易柜台，所有正规期货公司支持
- vnpy 等开源框架成熟；行情为快照式推送（非逐笔）
- ✅ 稳定、免费、社区大；❌ 仅期货/期权，股票不适用

> 结论：股票程序化实盘 = QMT 或 PTrade（二选一，看券商）；期货 = CTP；**miniQMT 已死，别押注**。

---

## 五、期货专项：天勤 TqSdk

| 维度 | 说明 |
|------|------|
| 费用 | 免费版：限指定期货公司账户实盘；专业版 14888 元/年；企业版 30000 元/年 |
| 数据 | 全部可交易合约上市以来全部 tick + K线，公司级运维 |
| 形态 | 开源 Python 库（信易/快期出品），历史数据+实时行情+实盘交易一体 |

- ✅ 免费版数据已经很全，散户期货首选
- ❌ Python 生态；免费版实盘限指定期货公司

---

## 六、美股 / 加密

### 6.1 美股

| 源 | 费用 | 特点 |
|----|------|------|
| **Alpaca** | 免费（IEX 交易所实时） | 美国券商+行情 API；免费实时仅覆盖 IEX 单一交易所，非全市场 |
| **Polygon.io（已更名 Massive）** | 免费层 + 付费 | 机构级，付费后含 L2、WebSocket；2026 年有品牌变更 |
| **Alpha Vantage** | 免费 5 次/分钟 | 技术指标全，适合研究，不适合实盘 |
| **yfinance** | 免费 | 雅虎财经爬虫封装，研究可用，实时性/稳定性无保障 |
| **Databento** | 按量 | 专业级 tick 数据，面向机构 |

### 6.2 加密

- **交易所原生 API（Binance 等）**：REST + WebSocket，行情延迟 20–80ms 级，IP 维度限流（如每 5 分钟 ≤300 个 WS 连接）；Binance 2026-03 起按数据类别拆分 WS URL（Public/Market/Private）
- ✅ 加密是唯一"个人能直接拿到交易所级实时推送"的市场
- ⚠️ 国内访问与合规风险自担；限流规则要写进客户端退避逻辑

---

## 七、商业源（生产级）

| 源 | 定位 | 费用量级 |
|----|------|---------|
| **Wind（万得）** | 机构事实标准，全字段+SLA | 数万元/年 |
| **同花顺 iFinD** | 机构级，部分场景性价比优于 Wind | 数万元/年 |
| **东方财富 Choice** | 机构级，与终端打通 | 数万元/年 |

- ✅ 有 SLA、合规授权、字段文档
- ❌ 个人/小团队难以承受；做实盘交易系统**涉及商业用途时，免费爬虫源有合规风险**，务必评估

---

## 八、选型结论

### 8.1 按场景决策矩阵

| 场景 | 首选 | 备选 | 理由 |
|------|------|------|------|
| A股实时监控（KILLER 类） | 东财+腾讯+新浪 三源互备 | pytdx 补分笔 | 免费、Go 直接 HTTP；单源必挂 |
| A股历史回测 | Tushare（付费）/ RQData | Baostock、AkShare 免费补 | 复权因子与字段规范是硬需求 |
| A股分钟级实时 | Tushare 实时分钟 / RQData | 东财 klt=1 分钟K | 免费源轮询有频控 |
| 股票实盘下单 | QMT | PTrade | miniQMT 已停发 |
| 期货全链路 | CTP + TqSdk 免费版 | vnpy + RQData | tick 数据免费版已够 |
| 美股研究 | yfinance / Alpha Vantage | — | 免费额度够研究 |
| 美股实盘行情 | Alpaca（免费 IEX）/ Massive | Databento | 看是否需要全市场 L2 |
| 加密实时 | 交易所官方 WebSocket | iTick 聚合 | 官方推送延迟最低 |
| 生产合规兜底 | Wind/Choice/iFinD | — | 商业用途必须评估授权 |

### 8.2 架构建议（对 KILLER 的直接含义）

```
实时层：东财(主) + 腾讯(备1) + 新浪(备2)   ← 建议把腾讯加进备源轮换
         └─ TTL 缓存 + 指数退避 + 健康分自动降级（datasource 看板已有雏形）
历史层：东财日K/分钟K（已有）→ 需要回测时再上 Tushare/RQData
交易层：暂不需要；若将来实盘，走 QMT 而非 miniQMT
```

### 8.3 可靠性通则

1. **任何免费源都没有 SLA**，可靠性来自"多源互备 + 缓存 + 退避"，不来自单个源本身
2. 轮询频率是限流主因：监控类 ≥3s/次、批量接口加 TTL，是安全线
3. 免费源做研究、做个人监控没问题；**收费交易系统必须走有授权的商业源**
4. WebSocket 推送源（加密、iTick、RQData 实时）延迟远优于 HTTP 轮询，但要处理重连与限流

---

## 八、免责声明

- 免费接口仅供个人学习研究；商业用途请采购合规数据服务（Wind、Choice、聚宽等）
- 高频爬取可能违反目标网站服务条款，请合理控制频率
- 各平台收费与政策随时间变化（如 miniQMT 停发、Tushare 积分制），以官网最新公告为准

---

## 参考来源

- [Tushare 积分与频次权限](https://tushare.pro/document/1?doc_id=290) / [平台积分](https://tushare.pro/document/1?doc_id=13)
- [2026 年 6 家主流量化数据源对比（同花顺量化）](https://quant.10jqka.com.cn/view/article/10IKVZSW1P157460EXB49MSUCX)
- [QMT vs PTrade（腾讯云开发者）](https://cloud.tencent.com/developer/article/2659413) / [2026 量化开户 QMT 还是 PTrade（知乎）](https://zhuanlan.zhihu.com/p/2074134771823818694)
- [miniQMT 停发说明（miniqmt.com）](https://www.miniqmt.com/) / [MiniQMT 关停事实核查（2026 版）](https://invest.wtsolutions.cn/posts/miniqmt-termination/)
- [天勤 TqSdk 文档](https://doc.shinnytech.com/tqsdk/latest/intro.html) / [TqSdk 专业版价格](https://doc.shinnytech.com/tqsdk/latest/profession.html)
- [RQData Python API 手册](https://www.ricequant.com/doc/rqdata/python/index-rqdatac)
- [pytdx 文档](https://pytdx-docs.readthedocs.io/zh-cn/latest/pytdx_reader/)
- [上证所 Level-2 行情](https://www.sseinfo.com/services/assortment/level2/) / [深交所行情互联网接入服务说明书](http://www.szsi.cn/cpfw/fwsq/hq/%E6%B7%B1%E4%BA%A4%E6%89%80%E8%A1%8C%E6%83%85%E4%BA%92%E8%81%94%E7%BD%91%E6%8E%A5%E5%85%A5%E6%9C%8D%E5%8A%A1-%E4%B8%8A%E4%BA%A4%E6%89%80LDDS%E8%A1%8C%E6%83%85%E6%8E%A5%E5%8F%A3%E8%AF%B4%E6%98%8E%E4%B9%A6.pdf)
- [通达信行情授权](https://www.tdx.com.cn/article/license.html) / [TdxQuant 通达信量化平台](https://help.tdx.com.cn/quant/)
- [常用股票实时数据源 API 平台对比（知乎）](https://zhuanlan.zhihu.com/p/2018803195330212782)
- [easyquotation（腾讯/新浪行情封装）](https://github.com/shidenggui/easyquotation)
- [2026 免费金融数据 API 横向对比（火山引擎开发者）](https://developer.volcengine.com/articles/7651565441715519494) / [WebSocket 实时行情推送 API 对比](https://developer.volcengine.com/articles/7675623750218055690)
- [2026 美股行情 API 选型指南（腾讯云）](https://developer.cloud.tencent.com/article/2625654) / [Databento vs Massive 对比](https://waylandz.com/quant-book/%E6%95%B0%E6%8D%AE%E6%8F%90%E4%BE%9B%E5%95%86%E5%AF%B9%E6%AF%94/)
- [Binance API](https://www.binance.com/zh-CN/binance-api) / [币安 U 本位合约 WebSocket 升级公告](https://www.binance.com/zh-CN/support/announcement/detail/ebf9b0aa9eca4ff3804eef6fb09ba32a)
- [awesome-quant（thuquant）](https://github.com/thuquant/awesome-quant/blob/master/README.md)
