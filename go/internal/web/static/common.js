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

function drawTimeSeries(canvas, data, opts) {
  opts = opts || {};
  const { ctx, w, h } = setupCanvas(canvas, opts.height || 260);
  ctx.clearRect(0, 0, w, h);
  const pts = data.map(d => ({ x: d.date, y: d.close }));
  if (pts.length < 2) {
    drawHint(ctx, w, h, opts.emptyText || "暂无数据");
    return;
  }
  const pad = { l: 10, r: 64, t: 14, b: 24 };
  const ys = pts.map(p => p.y);
  let min = Math.min(...ys), max = Math.max(...ys);
  if (opts.base != null) {
    min = Math.min(min, opts.base); max = Math.max(max, opts.base);
  }
  const span = (max - min) || 1;
  const X = i => pad.l + (w - pad.l - pad.r) * i / (pts.length - 1);
  const Y = v => pad.t + (h - pad.t - pad.b) * (1 - (v - min) / span);

  // 网格 + 右侧刻度
  const digits = opts.labelDigits != null ? opts.labelDigits : 1;
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
  if (opts.showBaseLine && opts.base != null) {
    ctx.setLineDash([4, 4]);
    ctx.strokeStyle = cssVar("--text-disabled", "#565a63");
    ctx.beginPath(); ctx.moveTo(pad.l, Y(opts.base)); ctx.lineTo(w - pad.r, Y(opts.base)); ctx.stroke();
    ctx.setLineDash([]);
  }

  // 折线颜色：优先显式指定，否则按首尾相对涨跌（A股红涨绿跌，取主题柔和色）
  const last = pts[pts.length - 1].y, first = pts[0].y;
  const base = opts.base != null ? opts.base : first;
  const upColor = cssVar("--up", "#d96b72"),
    downColor = cssVar("--down", "#65b98f"),
    flatColor = cssVar("--text-tertiary", "#777b85");
  ctx.strokeStyle = opts.lineColor || (last > base ? upColor : (last < base ? downColor : flatColor));
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
}
