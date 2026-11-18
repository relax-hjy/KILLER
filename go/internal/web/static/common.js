// KILLER 市场页共享工具
"use strict";

const $ = id => document.getElementById(id);
const cls = v => v > 0 ? "up" : (v < 0 ? "down" : "flat");
const pctStr = v => (v > 0 ? "+" : "") + Number(v).toFixed(2) + "%";
const fmt = (v, d = 2) => (v == null || v === 0 && d === 0) ? "—" : Number(v).toFixed(d);
// 价位：四舍五入到 3 位（ETF 最小变动 0.001），第 3 位为 0 时只显示 2 位，兼顾低价 ETF 与普通股票
const fmtPrice = v => {
  if (v == null) return "—";
  const r = Math.round(v * 1000) / 1000;
  return Math.abs(r * 100 - Math.round(r * 100)) < 1e-9 ? r.toFixed(2) : r.toFixed(3);
};
const fmtCap = v => { // 市值：亿
  if (!v) return "—";
  if (v >= 1e12) return (v / 1e12).toFixed(2) + "万亿";
  return (v / 1e8).toFixed(0) + "亿";
};
const fmtAmt = v => {
  if (!v) return "—";
  if (v >= 1e12) return (v / 1e12).toFixed(2) + "万亿";
  if (v >= 1e8) return (v / 1e8).toFixed(1) + "亿";
  return (v / 1e4).toFixed(0) + "万";
};

async function getJSON(url) {
  const r = await fetch(url);
  if (!r.ok) throw new Error(url + " → HTTP " + r.status);
  return r.json();
}

let _toastTimer;
function toast(msg) {
  let t = $("toast");
  if (!t) {
    t = document.createElement("div");
    t.id = "toast";
    document.body.appendChild(t);
  }
  t.textContent = msg;
  t.classList.add("show");
  clearTimeout(_toastTimer);
  _toastTimer = setTimeout(() => t.classList.remove("show"), 2500);
}

// 导航渲染：items=[{href,label}], active=当前页
function renderNav(items, active) {
  const el = document.querySelector("nav");
  if (!el) return;
  el.innerHTML = items.map(i =>
    `<a href="${i.href}" class="${i.href === active ? "on" : ""}">${i.label}</a>`).join("");
}

function startClock() {
  const el = $("clock");
  if (!el) return;
  const tick = () => el.textContent = new Date().toLocaleTimeString("zh-CN", { hour12: false });
  tick();
  setInterval(tick, 1000);
}

// 统一导航项
const NAV = [
  { href: "/", label: "大盘监控" },
  { href: "/boards.html", label: "A股板块" },
  { href: "/stocks.html", label: "A股个股" },
  { href: "/etfs.html", label: "A股 ETF" },
  { href: "/us.html", label: "美股" }, // 2026-11-13 起含板块 + 个股（成交活跃榜 / 纳指100 成分 tab 切换）
  { href: "/analysis.html", label: "分析" },
  { href: "/trading.html", label: "交易" },
  { href: "/datasource.html", label: "数据源" },
];

// 从 CSS 变量读取主题色（与 market.css 令牌一致，取不到时回退）
function cssVar(name, fallback) {
  const v = getComputedStyle(document.documentElement).getPropertyValue(name).trim();
  return v || fallback;
}

/* ---------- 分析器科普（点卡片展开"分析详情"） ----------
 * 给不熟技术指标的用户看：每张卡片点开解释"这是什么/每个子项啥意思/怎么看结论"。
 * terms 的键要与 analysis.SubScore.Name 对应（有单测保证子项齐全）。
 */
const ANALYZER_GUIDE = {
  trend: {
    weight: "30%",
    what: "看趋势的方向和强度。均线（MA）是最近 N 天收盘价的平均：MA5≈一周、MA20≈一月、MA60≈一季、MA250（年线）≈一年。短期均线排在长期之上、价格站在均线上方，就是上升趋势。",
    terms: {
      "均线排列": "MA5/MA10/MA20 的上下次序：多头排列=短在上、长在下（趋势向上），空头排列反之",
      "价与MA20": "价格在 20 日均线上方=中期趋势健康",
      "MA20斜率": "20 日均线最近 5 天在抬升还是下沉（趋势在加速还是减速）",
      "价与MA60": "季度成本线；价格在其上=中期趋势向上",
      "年线MA250": "一年成本线，牛熊分界；上市不足 250 个交易日时不打分（没有年线可比）",
      "周线共振": "把日K合成周K后 MA5/MA10 与价格是否同向——大周期也在走强才可靠",
    },
    how2: "偏多=趋势向上，顺势做胜率高；偏空=下降趋势，别轻易接飞刀。它的弱点是滞后：横盘震荡时会反复给假信号，要配合动量和支撑压力一起看。",
  },
  momentum: {
    weight: "25%",
    what: "看涨跌的“劲头”还在不在。MACD 是快慢均线的差值（金叉=短期动能转强、死叉=转弱），RSI 衡量最近涨跌的力度（0~100：>70 超买、<30 超卖）。",
    terms: {
      "MACD状态": "DIF 是否在 DEA 之上：金叉状态=动能偏强",
      "动能方向": "MACD 柱子（DIF−DEA）比上一根在变长还是变短：红柱走强=加速",
      "RSI强弱": "50~70 强势区间加分；>70 短期透支（超买）；<30 跌过头（超卖，有反弹空间）",
    },
    how2: "偏多=上涨动能还在；超买=短期涨太多，追高容易吃回撤；超卖=可能反弹，但也可能是下跌中继，别单看这一项。",
  },
  volume: {
    weight: "20%",
    what: "看成交量和价格是否配合。放量上涨=真金白银进场（可信）；缩量上涨=没人跟涨（容易掉头）；放量下跌=抛压重。",
    terms: {
      "量价配合": "量比（5 日均量/20 日均量）与近 5 日涨跌的组合结论",
    },
    how2: "量价配合趋势才可靠。这一项是单项打分（不是多子项加总），权重 20%。",
  },
  support: {
    weight: "15%",
    what: "看价格在近期波动区间里的位置。支撑=近 20 日最低价（跌到附近常有买盘），压力=近 120 日最高价（解套抛压区）。买得便宜与否就是这一项。",
    terms: {
      "区间位置": "现价在近 20 日高低区间中的相对位置：越靠下越便宜、越靠上越贵；突破 20 日/120 日高点单独加分",
    },
    how2: "这是“买点位置”的维度，和趋势互补：追强势看趋势，等回踩看支撑。注意低点刚刷新（支撑还在塌）时不给低吸分。",
  },
  volatility: {
    weight: "10%",
    what: "看这只股票平时波动多大。ATR（平均真实波幅）是近 14 日平均每天的振幅，占现价百分比；再和它自己过去一年比，是偏安静还是偏躁动。",
    terms: {
      "波动水平": "当前 ATR% 在自身一年里的分位：低位=拿得住，高位=容易被洗出去",
      "波动趋势": "近 5 日振幅是否比近 20 日放大 30% 以上（波动开始变大）",
    },
    how2: "波动越大，仓位越要小、止损越要宽。同一评分下，波动大的股票实际风险更高——看置信度和这项分位再决定买多少。",
  },
};

// explainFindingHTML 单个分析器的展开详情：这是什么 / 子项得分表 / 怎么看。
function explainFindingHTML(f) {
  const g = ANALYZER_GUIDE[f.analyzer] || {};
  const rows = (f.subs || []).map(s => `
    <tr>
      <td><b>${s.name}</b><div class="fd-x-term">${(g.terms && g.terms[s.name]) || ""}</div></td>
      <td class="fd-x-score ${s.score > 0 ? "up" : (s.score < 0 ? "down" : "flat")}">${s.score > 0 ? "+" : ""}${s.score.toFixed(0)}</td>
      <td>${s.note || "—"}</td>
    </tr>`).join("");
  return `
    <div class="fd-x-sec"><b>这是什么：</b>${g.what || ""}</div>
    <div class="fd-x-sec"><b>得分明细</b>（子项合计 ${f.score > 0 ? "+" : ""}${f.score.toFixed(0)} 分，该项在综合评分里占 ${g.weight || ""}）</div>
    <table class="fd-x-table">
      <thead><tr><th>子项</th><th>得分</th><th>当前读数</th></tr></thead>
      <tbody>${rows}</tbody>
    </table>
    <div class="fd-x-sec"><b>怎么看：</b>${g.how2 || ""}</div>
    <div class="fd-x-sec"><b>本项结论：</b>${f.reason || "—"}<span class="fd-x-term">（${f.detail || ""}）</span></div>`;
}

// bindFindingCards 给分析器卡片绑"点击展开/收起详情"。
function bindFindingCards(container) {
  if (!container) return;
  container.addEventListener("click", e => {
    const card = e.target.closest(".fd-card, .asm-fd > div");
    if (!card || !card.dataset.analyzer) return;
    const open = card.querySelector(".fd-explain");
    if (open) { open.remove(); card.classList.remove("open"); return; }
    // 一次只展开一张
    container.querySelectorAll(".fd-explain").forEach(x => x.remove());
    container.querySelectorAll(".open").forEach(x => x.classList.remove("open"));
    const div = document.createElement("div");
    div.className = "fd-explain";
    div.innerHTML = explainFindingHTML(JSON.parse(decodeURIComponent(card.dataset.finding)));
    card.appendChild(div);
    card.classList.add("open");
  });
}

/* ---------- 图表绘制（共享） ----------
 * 绘制一条收盘价时间序列折线。所有页面共用，避免多处重复实现。
 * opts: { base, showBaseLine, lineColor, labelDigits }
 *   - base: 基准价（分时图传昨收，K线传 null）
 *   - showBaseLine: 是否画昨收中轴虚线
 */
function setupCanvas(c, h) {
  const dpr = window.devicePixelRatio || 1;
  const w = c.clientWidth || 500;
  h = h || 260;
  c.width = w * dpr; c.height = h * dpr;
  c.style.height = h + "px";
  const ctx = c.getContext("2d");
  ctx.setTransform(dpr, 0, 0, dpr, 0, 0);
  return { ctx, w, h };
}

// 在画布中央画提示文字（加载中 / 空数据 / 错误）
function drawHint(ctx, w, h, text) {
  ctx.fillStyle = cssVar("--text-tertiary", "#777b85");
  ctx.font = "13px " + cssVar("--font-sans", "sans-serif");
  ctx.textAlign = "center";
  ctx.fillText(text, w / 2, h / 2);
}

/* ---------- 十字光标（共享，由 drawTimeSeries 自动绑定） ----------
 * 指针在画布上滑动时：竖线吸附到最近数据点并在底部显示该点日期，
 * 横线穿过该点并在右侧刻度栏显示对应价格。
 */
function chipText(ctx, text, cx, cy) {
  ctx.font = "11px " + cssVar("--font-mono", "monospace");
  const wpx = ctx.measureText(text).width + 10;
  const hpx = 16;
  const x = cx - wpx / 2, y = cy - hpx / 2;
  ctx.fillStyle = cssVar("--bg-input", "#191a1f");
  ctx.strokeStyle = cssVar("--border-strong", "rgba(255,255,255,.18)");
  ctx.lineWidth = 1;
  ctx.beginPath();
  // 老浏览器无 roundRect 时退化为直角矩形
  if (ctx.roundRect) ctx.roundRect(x, y, wpx, hpx, 3); else ctx.rect(x, y, wpx, hpx);
  ctx.fill(); ctx.stroke();
  ctx.fillStyle = cssVar("--text-primary", "#e8eaed");
  ctx.textAlign = "center";
  ctx.fillText(text, cx, cy + 4);
}

function drawCrosshair(ctx, st, hover) {
  const { pts, X, Y, pad, w, h, digits } = st;
  if (!pts.length) return;
  const i = Math.max(0, Math.min(pts.length - 1, hover.i | 0));
  const px = X(i), py = Y(pts[i].y);
  ctx.save();
  ctx.setLineDash([3, 3]);
  ctx.strokeStyle = cssVar("--border-strong", "rgba(255,255,255,.18)");
  ctx.lineWidth = 1;
  ctx.beginPath(); ctx.moveTo(px, pad.t); ctx.lineTo(px, h - pad.b); ctx.stroke();
  ctx.beginPath(); ctx.moveTo(pad.l, py); ctx.lineTo(w - pad.r, py); ctx.stroke();
  ctx.setLineDash([]);
  ctx.fillStyle = st.color;
  ctx.beginPath(); ctx.arc(px, py, 2.6, 0, Math.PI * 2); ctx.fill();
  // 日期标签（底部，越界时向内收）
  const date = String(pts[i].x).slice(5, 16).replace("T", " ");
  ctx.font = "11px " + cssVar("--font-mono", "monospace");
  const half = ctx.measureText(date).width / 2 + 6;
  chipText(ctx, date, Math.max(pad.l + half, Math.min(w - pad.r - half, px)), h - pad.b / 2 - 1);
  // 价格标签（右侧刻度栏，跟随横线）
  chipText(ctx, pts[i].y.toFixed(digits), w - pad.r / 2, Math.max(pad.t + 8, Math.min(h - pad.b - 8, py)));
  ctx.restore();
}

function bindCrosshair(canvas) {
  if (canvas._chBound) return;
  canvas._chBound = true;
  let raf = 0;
  const schedule = () => {
    if (raf) return;
    raf = requestAnimationFrame(() => {
      raf = 0;
      const st = canvas._chart;
      if (st) st.render(st.hover);
    });
  };
  const move = e => {
    const st = canvas._chart;
    if (!st) return;
    const rect = canvas.getBoundingClientRect();
    const px = e.clientX - rect.left;
    const i = Math.round((px - st.pad.l) / (st.plotW || 1) * (st.pts.length - 1));
    st.hover = { i: Math.max(0, Math.min(st.pts.length - 1, i)) };
    schedule();
  };
  canvas.addEventListener("pointermove", move);
  canvas.addEventListener("pointerdown", move);
  canvas.addEventListener("pointerup", move);
  canvas.addEventListener("pointerleave", () => {
    const st = canvas._chart;
    if (!st) return;
    st.hover = null;
    schedule();
  });
}

function drawTimeSeries(canvas, data, opts) {
  opts = opts || {};
  const { ctx, w, h } = setupCanvas(canvas, opts.height || 260);
  const pts = data.map(d => ({ x: d.date, y: d.close }));
  if (pts.length < 2) {
    canvas._chart = null;
    ctx.clearRect(0, 0, w, h);
    drawHint(ctx, w, h, opts.emptyText || "暂无数据");
    return;
  }
  const pad = { l: 10, r: 64, t: 14, b: 24 };
  // 基准价（分时昨收）：休市时兜底源会返回 0，需当作无效基准，否则 min 被拉到 0 压塌 Y 轴
  const base = opts.base > 0 ? opts.base : null;
  const ys = pts.map(p => p.y);
  let min = Math.min(...ys), max = Math.max(...ys);
  if (base != null) {
    min = Math.min(min, base); max = Math.max(max, base);
  }
  const span = (max - min) || 1;
  const X = i => pad.l + (w - pad.l - pad.r) * i / (pts.length - 1);
  const Y = v => pad.t + (h - pad.t - pad.b) * (1 - (v - min) / span);
  const digits = opts.labelDigits != null ? opts.labelDigits : 1;

  // 折线颜色：优先显式指定，否则按首尾相对涨跌（A股红涨绿跌，取主题柔和色）
  const last = pts[pts.length - 1].y, first = pts[0].y;
  const ref = base != null ? base : first;
  const upColor = cssVar("--up", "#d96b72"),
    downColor = cssVar("--down", "#65b98f"),
    flatColor = cssVar("--text-tertiary", "#777b85");
  const color = opts.lineColor || (last > ref ? upColor : (last < ref ? downColor : flatColor));

  const st = {
    pts, X, Y, pad, w, h, min, max, span, digits, color, opts,
    plotW: w - pad.l - pad.r,
    hover: null,
  };
  st.render = hover => {
    ctx.clearRect(0, 0, w, h);
    // 网格 + 右侧刻度
    ctx.strokeStyle = cssVar("--border-subtle", "rgba(255,255,255,.065)");
    ctx.fillStyle = cssVar("--text-tertiary", "#777b85");
    ctx.font = "11px " + cssVar("--font-mono", "monospace");
    ctx.textAlign = "left";
    for (let g = 0; g <= 4; g++) {
      const v = min + span * g / 4, y = Y(v);
      ctx.beginPath(); ctx.moveTo(pad.l, y); ctx.lineTo(w - pad.r, y); ctx.stroke();
      ctx.fillText(v.toFixed(digits), w - pad.r + 6, y + 4);
    }
    // 基准中轴（分时昨收）
    if (opts.showBaseLine && base != null) {
      ctx.setLineDash([4, 4]);
      ctx.strokeStyle = cssVar("--text-disabled", "#565a63");
      ctx.beginPath(); ctx.moveTo(pad.l, Y(base)); ctx.lineTo(w - pad.r, Y(base)); ctx.stroke();
      ctx.setLineDash([]);
    }
    // 折线
    ctx.strokeStyle = color;
    ctx.lineWidth = 1.6;
    ctx.beginPath();
    pts.forEach((p, i) => i ? ctx.lineTo(X(i), Y(p.y)) : ctx.moveTo(X(i), Y(p.y)));
    ctx.stroke();
    // X 轴首尾标签
    ctx.fillStyle = cssVar("--text-tertiary", "#777b85");
    ctx.font = "11px " + cssVar("--font-mono", "monospace");
    ctx.textAlign = "left";
    ctx.fillText(String(pts[0].x).slice(5, 16).replace("T", " "), pad.l, h - 6);
    ctx.textAlign = "right";
    ctx.fillText(String(pts[pts.length - 1].x).slice(5, 16).replace("T", " "), w - pad.r, h - 6);
    if (hover) drawCrosshair(ctx, st, hover);
  };

  // 同一份数据刷新时保留光标位置，换标的则重置
  const sig = pts.length + "|" + pts[0].x + "|" + pts[pts.length - 1].x;
  const prev = canvas._chart;
  if (prev && prev.sig === sig && prev.hover) st.hover = prev.hover;
  st.sig = sig;
  canvas._chart = st;
  bindCrosshair(canvas);
  st.render(st.hover);
}
