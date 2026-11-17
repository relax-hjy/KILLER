// 无头 Chrome CDP 截图：交易指导页「持仓应对预案」面板 + 首页告警表（三种新告警类型）
const { spawn } = require("child_process");
const fs = require("fs");
const CHROME = "C:\\Program Files\\Google\\Chrome\\Application\\chrome.exe";

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

  // 1) 交易指导页：等预案卡片生成
  await send("Page.navigate", { url: "http://localhost:8080/trading.html" });
  let planInfo = "";
  for (let i = 0; i < 20; i++) {
    await sleep(1000);
    const r = await send("Runtime.evaluate", {
      returnByValue: true,
      expression: `(() => {
        const cards = document.querySelectorAll(".plan-card");
        if (!cards.length) return "";
        document.getElementById("guidePlan").scrollIntoView({block:"center"});
        const first = cards[0].innerText.replace(/\\s+/g, " ");
        return JSON.stringify({n: cards.length, first});
      })()`,
    });
    if (r.result.value) { planInfo = r.result.value; break; }
  }
  console.log("plan:", planInfo || "NONE");
  let shot = await send("Page.captureScreenshot", { format: "png" });
  fs.writeFileSync(__dirname + "\\_shot_plan.png", Buffer.from(shot.data, "base64"));
  console.log("saved _shot_plan.png");

  // 2) 首页告警表
  await send("Page.navigate", { url: "http://localhost:8080/index.html" });
  await sleep(5000);
  const alertInfo = await send("Runtime.evaluate", {
    returnByValue: true,
    expression: `(() => {
      const rows = [...document.querySelectorAll("#alertBody tr")];
      const want = rows.filter(r => /止损触发|止盈到位|大盘转弱/.test(r.innerText));
      if (want.length) want[0].scrollIntoView({block:"center"});
      return JSON.stringify({total: rows.length, matched: want.length,
        labels: want.map(r => r.innerText.replace(/\\s+/g," ").slice(0,120))});
    })()`,
  });
  console.log("alerts:", alertInfo.result.value);
  shot = await send("Page.captureScreenshot", { format: "png" });
  fs.writeFileSync(__dirname + "\\_shot_alerts.png", Buffer.from(shot.data, "base64"));
  console.log("saved _shot_alerts.png");

  ws.close();
  chrome.kill();
}
main().catch(e => { console.error(e); process.exit(1); });
