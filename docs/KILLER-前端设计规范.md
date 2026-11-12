# KILLER 前端设计规范

> 视觉语言取自 [Aetheris](../../Aetheris) 设计系统，2026-11-13 重塑落地。
> 本规范是 KILLER Web 面板（`go/internal/web/static/`）的前端唯一设计标准，新增页面、改样式都以此为准。

---

## 1. 设计理念

Aetheris 的核心是**单色极简 + 数据密度优先**：

- **单色调色板**：黑白灰为骨架，近白色（`#f0f0f2`）作为主操作色——这是 Aetheris 最具辨识度的签名（主按钮白底黑字，而非传统的品牌色填充）。
- **细边框、无阴影**：卡片与面板用 6.5% 透明度的白色细边框分隔层级，不靠阴影。视觉更安静，信息密度更高。
- **低饱和状态色**：success/warning/danger 取柔和色，避免高饱和的「番茄红 / 翠绿」刺激视觉；金融涨跌色沿用这套柔和色，保持红涨绿跌语义。
- **紧凑排版**：正文 13px、表格 12px、元信息 11px。数字一律 `tabular-nums` 等宽，大数值加 `letter-spacing: -0.03em` 收紧。
- **双层导航 + 下划线激活**：顶导航 48px，激活项用底部 2px 下划线标记，不用色块填充。
- **克制动效**：140ms（快）/ 180ms（标准），`ease` 缓动，无弹跳、无渐入渐出叙事。

### 不做什么

- 不用阴影区分卡片（除抽屉、toast、对话框这类需要「浮起」语义的浮层）。
- 不用品牌色填充按钮（主按钮就是白色）。
- 不用高饱和红绿（涨跌色走 `--danger` / `--success` 柔和色）。
- 不在表格 hover 时覆盖涨跌色（见 §4 表格）。

---

## 2. 令牌系统

所有令牌定义在 `market.css` 的 `:root`。改令牌 = 改全局视觉，务必整体评估。

### 2.1 颜色

| 令牌 | 值 | 用途 |
|---|---|---|
| `--bg-app` | `#101114` | 页面底色 |
| `--bg-nav` | `#131417` | 顶导航 / 抽屉侧栏 |
| `--bg-surface` | `#16171b` | 卡片、面板、抽屉 |
| `--bg-subtle` | `#1a1b20` | 状态条徽章、表头、标签底 |
| `--bg-raised` | `#1e1f24` | toast、悬停提升 |
| `--bg-hover` | `#202126` | 行/卡悬停 |
| `--bg-selected` | `rgba(255,255,255,.08)` | 激活 tab、选中项 |
| `--bg-input` | `#191a1f` | 输入框、下拉 |

| 文字令牌 | 值 | 用途 |
|---|---|---|
| `--text-primary` | `#eeeef1` | 主文字、标题、价格 |
| `--text-secondary` | `#a8abb3` | 次要文字、表格正文 |
| `--text-tertiary` | `#777b85` | 标签、提示、刻度 |
| `--text-disabled` | `#565a63` | 禁用、占位 |

| 边框令牌 | 值 | 用途 |
|---|---|---|
| `--border-subtle` | `rgba(255,255,255,.065)` | 卡片、面板内分割 |
| `--border-default` | `#rgba(255,255,255,.10)` | 输入框、按钮、徽章 |
| `--border-strong` | `rgba(255,255,255,.16)` | hover、激活边框 |

| 语义令牌 | 值 | 用途 |
|---|---|---|
| `--accent` | `#f0f0f2` | **主操作色（近白）**，导航激活下划线 |
| `--accent-hover` | `#ffffff` | 主按钮 hover |
| `--accent-border` | `rgba(255,255,255,.22)` | 选中卡边框 |
| `--focus-ring` | `rgba(255,255,255,.14)` | 输入聚焦光环 |
| `--success` | `#65b98f` | 联通、下跌（A 股绿跌） |
| `--warning` | `#d6a85f` | 星标、振幅告警 |
| `--danger` | `#d96b72` | 断开、上涨（A 股红涨） |

每个状态色配套 `*-subtle`（11% 透明底色），用于徽章背景。

### 2.2 涨跌色约定（重要）

A 股惯例：**红涨绿跌**。KILLER 把涨跌映射到 Aetheris 柔和状态色：

```css
--up:   var(--danger);   /* #d96b72  涨 */
--down: var(--success);  /* #65b98f  跌 */
--flat: var(--text-tertiary);
```

> ⚠️ 这与欧美市场惯例（绿涨红跌）相反。KILLER 面向 A 股用户，必须保持红涨绿跌。美股页（`us.html`）同样用这套色——涨仍红、跌仍绿，不因市场切换而反色。

### 2.3 字体

```css
--font-sans:    "DM Sans", "Noto Sans SC", "PingFang SC", "Microsoft YaHei", system-ui, sans-serif;
--font-display: "Host Grotesk", "DM Sans", "Noto Sans SC", "PingFang SC", system-ui, sans-serif;
--font-mono:    "SFMono-Regular", Consolas, Menlo, monospace;
```

- **DM Sans**：正文、表格、按钮。
- **Host Grotesk**：标题（h1/h2/h3）、价格、大数值。特征是 letter-spacing 收紧（`-0.02em ~ -0.03em`）。
- **mono**：代码、secid、时间戳。

字体通过 Google Fonts CDN 引入，每个 HTML head 必须有：

```html
<link rel="preconnect" href="https://fonts.googleapis.com">
<link rel="preconnect" href="https://fonts.gstatic.com" crossorigin>
<link href="https://fonts.googleapis.com/css2?family=DM+Sans:wght@400;500;600;700&family=Host+Grotesk:wght@400;500;600;700&display=swap" rel="stylesheet">
```

离线时自动回退到系统中文/英文字体栈，不影响可用性。

### 2.4 字号（紧凑排版）

| 令牌 | 值 | 用途 |
|---|---|---|
| `--font-page` | 18px | 抽屉标题 |
| `--font-section` | 14px | 区块标题、面板头 |
| `--font-body` | 13px | 正文（body 默认） |
| `--font-table` | 12px | 表格、输入、按钮 |
| `--font-meta` | 11px | 标签、提示、徽章、刻度 |

### 2.5 几何与动效

| 令牌 | 值 |
|---|---|
| `--topnav-height` | 48px |
| `--radius-sm` / `--radius-md` / `--radius-lg` | 4 / 6 / 8px |
| `--motion-fast` / `--motion-base` | 140ms / 180ms |
| `--shadow-float` | `0 10px 28px rgba(0,0,0,.28)`（toast） |
| `--shadow-dialog` | `0 18px 48px rgba(0,0,0,.38)`（抽屉） |

### 2.6 旧变量别名（兼容层）

页面内联样式和 JS 历史上用了一批短名变量，全部保留为别名，**不要在页面里直接用新长名**，沿用别名即可：

| 别名 | 指向 | 说明 |
|---|---|---|
| `--bg` | `--bg-app` | |
| `--panel` | `--bg-surface` | |
| `--panel2` | `--bg-raised` | |
| `--border` | `--border-default` | |
| `--text` | `--text-primary` | |
| `--muted` | `--text-tertiary` | |
| `--up` / `--down` / `--flat` | danger / success / tertiary | 涨跌色 |

---

## 3. 页面骨架

每个页面必须用这套结构：

```html
<!DOCTYPE html>
<html lang="zh-CN">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<title>KILLER · 页面名</title>
<!-- 字体（见 §2.3） -->
<link rel="stylesheet" href="/market.css">
</head>
<body>
<header>
  <h1>KILLER<span class="en">页面副标题</span></h1>
  <nav></nav>
  <span id="clock"></span>
</header>

<main>
  <!-- 内容 -->
</main>

<footer>数据来源：… · 仅供学习研究，不构成投资建议</footer>

<script src="/common.js"></script>
<script>
renderNav(NAV, "/当前页.html");
startClock();
// 页面逻辑
</script>
</body>
</html>
```

- `header` 由 `common.js` 的 `renderNav(NAV, active)` 填充 `<nav>`，导航项在 `common.js` 的 `NAV` 数组统一定义。
- `startClock()` 启动右上角时钟。
- **新增页面必须**：① 在 `common.js` 的 `NAV` 加导航项；② header 用上述结构；③ 引 `market.css` + `common.js`。

---

## 4. 组件规范

### 4.1 顶导航（header / topnav）

- 高 48px，`--bg-nav` 底，底部 `--border-default` 分割线，sticky。
- 品牌：`h1` 用 Host Grotesk 16px / weight 660 / `letter-spacing: -0.02em`；`.en` 副标题 11px tertiary。
- 导航项：12.5px / weight 520，默认 tertiary，hover secondary + 3% 白底，**激活项** primary + 底部 2px `--accent` 下划线（`::after`）。
- 时钟：12px tertiary，`tabular-nums`。

### 4.2 状态条（status-badge）

用于大盘页的交易时段 / 数据源 / 错误提示。每个 `<span>` 是一枚徽章：

```html
<div class="status">
  <span><span class="dot ok"></span>交易时段</span>
  <span>数据源: 东财</span>
</div>
```

- 22px 高，`--border-default` + `--bg-subtle` 底，4px 圆角。
- `.dot` 是 6px 圆点：`.ok`=success、`.off`=disabled、`.err`=danger。
- **空 span 自动隐藏**（`.status > span:empty { display:none }`），避免渲染空框。

### 4.3 卡片（card）

行情卡片网格：

```css
.cards { grid-template-columns: repeat(auto-fill, minmax(230px, 1fr)); gap: 12px; }
.card { border: 1px solid var(--border-subtle); border-radius: var(--radius-lg); padding: 14px 16px; }
```

- `.card .price`：Host Grotesk 24px / weight 620 / `letter-spacing: -0.03em` / tabular-nums。
- hover / `.active`：边框升到 `--border-strong`，背景 `--bg-hover`。无阴影、无缩放。
- 涨跌靠 `.up` / `.down` / `.flat` 类着色，不要自己写颜色。

### 4.4 面板（panel / data-surface）

承载表格、图表、表单的容器：

```css
.panel { background: var(--bg-surface); border: 1px solid var(--border-subtle);
         border-radius: var(--radius-lg); overflow: hidden; }
.panel > h2 { padding: 12px 16px; border-bottom: 1px solid var(--border-subtle);
              font-size: var(--font-section); color: var(--text-secondary); }
```

- 面板头用 `section-header` 模式：14px / weight 600 / secondary 色，底部细分隔线。
- 无阴影、无内边距留白叙事，靠细边框分组。

### 4.5 表格

```css
th { background: var(--bg-subtle); color: var(--text-tertiary);
     font-size: var(--font-meta); font-weight: 500; padding: 8px 12px; }
td { padding: 8px 12px; color: inherit; font-variant-numeric: tabular-nums; }
tbody tr { color: var(--text-secondary); }
tbody tr:hover { background: var(--bg-hover); color: var(--text-primary); }
```

**关键约定**：`td { color: inherit }` + 行 hover 改 `tr` 的 color，让 td 继承变亮——这样 `.up` / `.down` 涨跌色不会被 hover 覆盖（类选择器优先级高于继承）。**不要写 `tbody tr:hover td { color: ... }`**，那会盖掉涨跌色。

- 默认右对齐数字，前两列左对齐（代码、名称）。
- `th` sticky 在 `--topnav-height`（48px）下方。
- 空状态用 `.empty` / `.loading`（tertiary 色、居中、28px padding）。

### 4.6 标签切换（tabs / chips）

Aetheris subnav 芯片模式，不是连体分段控件：

```css
.tabs button { height: 28px; padding: 0 12px; border-radius: var(--radius-md);
               color: var(--text-tertiary); background: var(--bg-subtle); }
.tabs button.on { color: var(--text-primary); background: var(--bg-selected);
                  border-color: var(--border-strong); font-weight: 620; }
```

- 激活态用 `--bg-selected`（8% 白）+ 加粗，**不用 accent 填充**。
- `.toolbar` 里的按钮、分页按钮共用这套样式。

### 4.7 按钮

```css
.btn { background: var(--accent); color: #101114;  /* 白底黑字，签名色 */
       border: 1px solid var(--accent); border-radius: var(--radius-md);
       font-weight: 600; padding: 6px 16px; }
.btn:hover { background: var(--accent-hover); }
```

- 主按钮（下单、回测、测试连接）用 `.btn`。
- 次级切换用 `.tabs button` / `.chip`。
- **禁止** `color:#fff` 配 `background:var(--accent)`——accent 是近白色，白字白底不可见（datasource 页曾有此 bug）。

### 4.8 表单

```css
input, select { background: var(--bg-input); border: 1px solid var(--border-default);
                padding: 7px 10px; border-radius: var(--radius-md); }
input:focus { border-color: var(--border-strong); box-shadow: 0 0 0 2px var(--focus-ring); }
```

- 输入底色比面板更深（`--bg-input` #191a1f），形成凹陷感。
- 聚焦用 2px 光环，不用改边框颜色到品牌色。

### 4.9 徽章（badge / status-badge）

告警、信号、连通状态：

```css
.badge { border: 1px solid transparent; border-radius: var(--radius-sm);
         padding: 1px 8px; font-size: var(--font-meta); }
.badge.change_up   { background: var(--danger-subtle);  color: var(--up);
                     border-color: rgba(217,107,114,.24); }
```

- 底色用对应状态色的 `*-subtle`（11% 透明），边框用 24% 透明同色。
- 三类告警：`change_up`（红）/ `change_down`（绿）/ `amplitude`（黄）。
- 交易信号：`sig-buy` / `sig-sell` / `sig-hold`（定义在 trading.html 页内样式）。

### 4.10 热度条（heat）

板块涨幅条：

```css
.heat { height: 12px; border-radius: 3px; opacity: 0.85; min-width: 2px; }
```

- 颜色由 JS 内联 `background:var(--up)` / `var(--down)` 决定，宽度按涨幅占比。
- 不要用硬编码 `#f0453e`，用变量。

### 4.11 详情抽屉（#detail）

板块成分股、个股详情的侧滑抽屉：

```css
#detail { width: min(560px, 92vw); background: var(--bg-surface);
          border-left: 1px solid var(--border-default);
          box-shadow: var(--shadow-dialog);  /* 唯一允许的阴影场景 */
          transform: translateX(100%); transition: transform var(--motion-base) ease; }
#detail.open { transform: translateX(0); }
```

- 抽屉是**少数允许用阴影的组件**（dialog 语义需要「浮起」）。
- 标题 18px Host Grotesk weight 640。
- KPI 网格（`.kpis`）3 列，每格 `.kpi` 用 `--bg-subtle` + `--border-subtle`。

### 4.12 Toast

```css
#toast { background: var(--bg-raised); border: 1px solid var(--border-default);
         box-shadow: var(--shadow-float); border-radius: var(--radius-md); }
```

- `common.js` 的 `toast(msg)` 自动创建/复用 `#toast`，2.5s 自动消失。

### 4.13 星标（自选）

```css
.star { color: var(--text-disabled); }
.star.on, .star:hover { color: var(--warning); }  /* 用 warning 黄，不用 #f5c518 */
```

---

## 5. 图表（canvas）

所有折线图走 `common.js` 的 `drawTimeSeries(canvas, data, opts)`，**不要在页面里手写 canvas 绘图**。

- 配色从 CSS 变量读取（`cssVar(name, fallback)`），自动跟随主题：
  - 网格线 `--border-subtle`
  - 刻度文字 `--text-tertiary` / mono 字体
  - 昨收中轴虚线 `--text-disabled`
  - 折线：涨 `--up`、跌 `--down`、平 `--text-tertiary`（按首尾相对涨跌判定）
- `opts`：`{ base, showBaseLine, lineColor, labelDigits, height, emptyText }`
  - 分时图传 `base: 昨收` + `showBaseLine: true`
  - K 线传 `base: null`
- 空数据/加载中用 `drawHint(ctx, w, h, text)`。

> 新页面要画图，直接调 `drawTimeSeries`。若需新图表类型（K 线柱、成交量），扩展 `common.js`，配色继续走 `cssVar`。

---

## 6. 页面级样式约定

- **能用 `market.css` 的类就不要写页内 `<style>`**。
- 必须写页内样式时（如 trading 的 `.chip`、datasource 的 `.src-card`），变量一律用别名（`var(--muted)` 等），不要硬编码颜色。
- 禁止内联 `style="color:#fff;background:var(--accent)"` 这类组合——accent 是近白色，会白字白底。
- 行内 `style` 只用于一次性布局微调（宽度、显隐），不用于着色。

---

## 7. 构建与部署

### go:embed 注意事项

静态文件通过 `//go:embed static` 嵌入二进制（`server.go`）。**改了 `static/` 目录后必须重新 `go build`**，否则服务跑的是旧嵌入文件。

```bash
cd go && go build -o killer.exe ./cmd/killer
```

本环境配置了 build 后自动重启 killer.exe（PID 会变）。手动部署时记得先停旧进程再启动新二进制。

### 静态资源禁缓存

服务端对静态文件设了 `no-store`（`server.go` 的 `noStore` 中间件），改版后浏览器不会拿到缓存旧 JS/CSS。

---

## 8. 新增页面 Checklist

- [ ] `common.js` 的 `NAV` 数组加导航项
- [ ] HTML head 引字体 + `market.css`
- [ ] header 用标准结构（h1 + `<nav></nav>` + clock）
- [ ] `renderNav(NAV, "/新页.html")` + `startClock()`
- [ ] 表格走 §4.5，卡片走 §4.3，面板走 §4.4
- [ ] 涨跌色用 `.up` / `.down` 类，不硬编码
- [ ] 图表调 `drawTimeSeries`，不手写 canvas
- [ ] `go build` 重新编译（embed 才会更新）

---

## 9. 参考实现

- 设计令牌源头：`go/internal/web/static/market.css` 的 `:root`
- 组件样式源头：同文件的各 `.` 选择器
- 图表实现：`go/internal/web/static/common.js` 的 `drawTimeSeries` / `cssVar`
- 视觉范本：Aetheris `apps/web/src/styles/tokens.css` + `theme.ts`
