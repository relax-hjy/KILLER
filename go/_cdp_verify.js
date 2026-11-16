// 无头 Chrome CDP 验证：打开 etfs.html → 点击首行开抽屉 → 等成分股加载 → 截图
const { spawn } = require("child_process");
const fs = require("fs");
const CHROME = "C:\\Program Files\\Google\\Chrome\\Application\\chrome.exe";
const OUT = __dirname + "\\_etf_detail_shot.png";

const sleep = ms => new Promise(r => setTimeout(r, ms));

async function main() {
  const chrome = spawn(CHROME, [
    "--headless=new", "--remote-debugging-port=9333",
    "--window-size=1440,900", "--no-first-run", "about:blank",
  ], { stdio: "ignore" });

  // 等调试端口
  let targets;
  for (let i = 0; i < 30; i++) {
    await sleep(500);
    try {
      targets = await (await fetch("http://127.0.0.1:9333/json")).json();
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
  const events = [];
  ws.onmessage = e => {
    const m = JSON.parse(e.data);
    if (m.id && pending.has(m.id)) { pending.get(m.id).res(m.result); pending.delete(m.id); }
    else if (m.method) events.push(m.method);
  };
  await new Promise(r => ws.onopen = r);

  await send("Page.enable");
  await send("Page.navigate", { url: "http://localhost:8080/etfs.html" });
  await sleep(6000); // 等列表加载

  // 点击自监控 ETF 卡片（腾讯批量行情，限流期也有数据）
  const clickRes = await send("Runtime.evaluate", {
    returnByValue: true,
    expression: `(() => {
      const card = document.querySelector("#watchCards .card[data-secid]");
      if (!card) return "NO_CARD";
      const r = card.getBoundingClientRect();
      const x = r.left + 20, y = r.top + r.height / 2;
      for (const type of ["pointerdown","mousedown","pointerup","mouseup","click"]) {
        const Ev = type.startsWith("pointer") ? PointerEvent : MouseEvent;
        card.dispatchEvent(new Ev(type, {bubbles:true, clientX:x, clientY:y, pointerId:1, isPrimary:true}));
      }
      return "CLICKED " + card.dataset.secid;
    })()`,
  });
  console.log("click:", clickRes.result.value);

  await sleep(4000); // 等成分股接口

  // 检查成分股区状态
  const check = await send("Runtime.evaluate", {
    returnByValue: true,
    expression: `(() => {
      const d = document.getElementById("dHoldings");
      const p = document.getElementById("dHoldPeriod");
      const rows = d ? d.querySelectorAll("tbody tr").length : -1;
      const open = document.getElementById("detail").classList.contains("open");
      const firstRow = d && d.querySelector("tbody tr") ? d.querySelector("tbody tr").innerText.replace(/\\s+/g," ") : "";
      return JSON.stringify({open, rows, period: p ? p.textContent : "", firstRow});
    })()`,
  });
  console.log("holdings:", check.result.value);

  const shot = await send("Page.captureScreenshot", { format: "png" });
  fs.writeFileSync(OUT, Buffer.from(shot.data, "base64"));
  console.log("saved", OUT);

  ws.close();
  chrome.kill();
}
main().catch(e => { console.error(e); process.exit(1); });
