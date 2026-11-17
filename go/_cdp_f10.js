// 无头 Chrome CDP 验证：打开 stocks.html → 点首行开详情抽屉 → 等 F10 加载 → 截图顶部与底部
const { spawn } = require("child_process");
const fs = require("fs");
const CHROME = "C:\\Program Files\\Google\\Chrome\\Application\\chrome.exe";
const OUT_TOP = __dirname + "\\_f10_shot_biz.png";
const OUT_BOT = __dirname + "\\_f10_shot_fin.png";

const sleep = ms => new Promise(r => setTimeout(r, ms));

async function main() {
  const chrome = spawn(CHROME, [
    "--headless=new", "--remote-debugging-port=9334",
    "--window-size=1440,900", "--no-first-run", "about:blank",
  ], { stdio: "ignore" });

  let targets;
  for (let i = 0; i < 30; i++) {
    await sleep(500);
    try {
      targets = await (await fetch("http://127.0.0.1:9334/json")).json();
      if (targets.length) break;
    } catch {}
  }
  const page = targets.find(t => t.type === "page");
  const ws = new WebSocket(page.webSocketDebuggerUrl);
  let id = 0;
  const pending = new Map();
  const send = (method, params = {}) => new Promise((res, rej) => {
    const i = ++id;
    pending.set(i, { res, rej });
    ws.send(JSON.stringify({ id: i, method, params }));
  });
  ws.onmessage = e => {
    const m = JSON.parse(e.data);
    if (m.id && pending.has(m.id)) { pending.get(m.id).res(m.result); pending.delete(m.id); }
  };
  await new Promise(r => ws.onopen = r);

  await send("Page.enable");
  await send("Page.navigate", { url: "http://localhost:8080/stocks.html" });
  await sleep(6000); // 等个股列表

  const clickRes = await send("Runtime.evaluate", {
    returnByValue: true,
    expression: `(() => {
      const tr = document.querySelector("#body tr[data-secid]");
      if (!tr) return "NO_ROW";
      const r = tr.getBoundingClientRect();
      const x = r.left + 120, y = r.top + r.height / 2;
      for (const type of ["pointerdown","mousedown","pointerup","mouseup","click"]) {
        const Ev = type.startsWith("pointer") ? PointerEvent : MouseEvent;
        tr.dispatchEvent(new Ev(type, {bubbles:true, clientX:x, clientY:y, pointerId:1, isPrimary:true}));
      }
      return "CLICKED " + tr.dataset.secid;
    })()`,
  });
  console.log("click:", clickRes.result.value);

  await sleep(6000); // 等 /api/f10（东财三接口）

  const check = await send("Runtime.evaluate", {
    returnByValue: true,
    expression: `(() => {
      const d = document.getElementById("detail");
      const f10 = document.getElementById("dF10");
      const fin = document.getElementById("dFin");
      const r = id => (document.getElementById(id) || {}).textContent || "";
      const vis = el => getComputedStyle(el).display !== "none";
      return JSON.stringify({
        open: d.classList.contains("open"),
        name: r("dName"),
        tabBizOn: document.getElementById("dBiz").classList.contains("on"),
        bizVisible: vis(f10),
        finHidden: !vis(fin),
        f10meta: r("dF10Meta"),
        f10text: f10.innerText.slice(0, 60).replace(/\\s+/g, " "),
        f10opRows: f10.querySelectorAll("tbody tr").length,
        finRows: fin.querySelectorAll("tbody tr").length,
        overflowX: d.scrollWidth > d.clientWidth,
      });
    })()`,
  });
  console.log("check(biz tab):", check.result.value);

  // 截图：主营业务 tab（滚到 tab 行）
  await send("Runtime.evaluate", {
    expression: `document.querySelector(".f10-head").scrollIntoView({block:"start"});`,
  });
  await sleep(400);
  let shot = await send("Page.captureScreenshot", { format: "png" });
  fs.writeFileSync(OUT_TOP, Buffer.from(shot.data, "base64"));

  // 切到财报摘要 tab
  await send("Runtime.evaluate", {
    expression: `(() => {
      const btn = document.getElementById("dFinTab");
      const r = btn.getBoundingClientRect();
      for (const type of ["pointerdown","mousedown","pointerup","mouseup","click"]) {
        const Ev = type.startsWith("pointer") ? PointerEvent : MouseEvent;
        btn.dispatchEvent(new Ev(type, {bubbles:true, clientX:r.left+10, clientY:r.top+5, pointerId:1, isPrimary:true}));
      }
      return "TAB_CLICKED";
    })()`,
  });
  await sleep(500);

  const check2 = await send("Runtime.evaluate", {
    returnByValue: true,
    expression: `(() => {
      const f10 = document.getElementById("dF10");
      const fin = document.getElementById("dFin");
      const vis = el => getComputedStyle(el).display !== "none";
      return JSON.stringify({
        finTabOn: document.getElementById("dFinTab").classList.contains("on"),
        bizOn: document.getElementById("dBiz").classList.contains("on"),
        finVisible: vis(fin),
        bizHidden: !vis(f10),
        finMeta: document.getElementById("dF10Meta").textContent,
        finRows: fin.querySelectorAll("tbody tr").length,
        finCols: fin.querySelectorAll("thead th").length,
        finFirst: fin.querySelector("tbody tr") ? fin.querySelector("tbody tr").innerText.replace(/\\s+/g, " ") : "",
        overflowX: document.getElementById("detail").scrollWidth > document.getElementById("detail").clientWidth,
      });
    })()`,
  });
  console.log("check(fin tab):", check2.result.value);

  // 截图：财报摘要 tab（滚到 tab 行）
  const preShot = await send("Runtime.evaluate", {
    returnByValue: true,
    expression: `JSON.stringify({
      thead: (document.querySelector("#dFin thead")||{}).innerText || "",
      meta: document.getElementById("dF10Meta").textContent,
      href: location.href,
      t: Date.now(),
    })`,
  });
  console.log("pre-shot:", preShot.result.value);
  await send("Runtime.evaluate", {
    expression: `document.querySelector(".f10-head").scrollIntoView({block:"start"});`,
  });
  await sleep(400);
  shot = await send("Page.captureScreenshot", { format: "png" });
  fs.writeFileSync(OUT_BOT, Buffer.from(shot.data, "base64"));

  console.log("saved", OUT_TOP, OUT_BOT);
  ws.close();
  chrome.kill();
}
main().catch(e => { console.error(e); process.exit(1); });
