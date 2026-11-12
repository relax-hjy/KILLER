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
  { href: "/us.html", label: "美股" },
  { href: "/ndx.html", label: "纳指100" },
  { href: "/datasource.html", label: "数据源" },
];
