// KILLER 市场页共享工具
"use strict";

const $ = id => document.getElementById(id);
const cls = v => v > 0 ? "up" : (v < 0 ? "down" : "flat");
const pctStr = v => (v > 0 ? "+" : "") + Number(v).toFixed(2) + "%";
const fmt = (v, d = 2) => (v == null || v === 0 && d === 0) ? "—" : Number(v).toFixed(d);
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
