// Smoke-тест через headless Chrome (Playwright) на системном Chrome.
// Запуск: node run-smoke.js [baseURL]   (по умолчанию http://malina.local или $MALINA_WEB)
const { chromium } = require("playwright");

const BASE = process.argv[2] || process.env.MALINA_WEB || "http://malina.local";
const PASS = process.env.MALINA_PASS || "";
if (!PASS) {
  console.error("задайте пароль веб-логина: MALINA_PASS=<пароль> node run-smoke.js");
  process.exit(2);
}

function assert(cond, msg) {
  if (!cond) { console.error("FAIL:", msg); process.exitCode = 1; } else { console.log("ok:", msg); }
}

(async () => {
  const browser = await chromium.launch({ channel: "chrome" });
  const ctx = await browser.newContext();
  const page = await ctx.newPage();
  page.on("console", m => { if (m.type() === "error") console.log("console.error:", m.text()); });

  // / -> редирект на /login
  let resp = await page.goto(BASE + "/", { waitUntil: "domcontentloaded" });
  assert(page.url().endsWith("/login"), "/ redirect to /login (" + page.url() + ")");

  // Неверный пароль
  await page.fill('input[name="password"]', "wrong");
  await page.click('button[type="submit"]');
  await page.waitForLoadState("domcontentloaded");
  assert((await page.content()).includes("Invalid") || (await page.content()).includes("Неверн"), "wrong password shows error");

  // Верный пароль
  await page.fill('input[name="password"]', PASS);
  await page.click('button[type="submit"]');
  await page.waitForLoadState("networkidle");
  assert(new URL(page.url()).pathname === "/", "login ok -> / (" + page.url() + ")");

  // Главная: пустая, но меню на месте.
  const home = await page.content();
  assert(/Сеть|Network|Настройка сети/.test(home), "menu present");
  assert(/Информация о системе|System information/.test(home), "info menu present");

  // /info: страница «Информация о системе».
  await page.goto(BASE + "/info", { waitUntil: "domcontentloaded" });
  assert(new URL(page.url()).pathname === "/info", "info page at /info (" + page.url() + ")");
  const infoHtml = await page.content();
  assert(infoHtml.includes('id="sysinfo"') && infoHtml.includes('id="autoref"'), "/info has sysinfo block");

  // /api/system (через fetch в контексте, с cookie)
  const api = await page.evaluate(async () => {
    const r = await fetch("/api/system", { cache: "no-store" });
    return { status: r.status, body: await r.json() };
  });
  assert(api.status === 200 && api.body && api.body.net, "/api/system returns net section: " + JSON.stringify(api.body.net));

  // /network: поля новой формы
  await page.goto(BASE + "/network", { waitUntil: "domcontentloaded" });
  const nh = await page.content();
  for (const n of ["m1_lan_mask", "m1_ap_channel", "m1_ap_country", "m2_eth_mask", "class=\"pw\"", "static-row"]) {
    assert(nh.includes(n), "/network has " + n);
  }

  // Статистика -> Таблицы: страница, закладки (из /api/stats/devices), таблица.
  await page.goto(BASE + "/stats/tables", { waitUntil: "domcontentloaded" });
  assert(new URL(page.url()).pathname === "/stats/tables", "stats/tables page (" + page.url() + ")");
  const stHtml = await page.content();
  assert(stHtml.includes('id="stats-tabs"') && stHtml.includes('id="stats-table"') && stHtml.includes("stats.js"),
    "stats/tables has tabs/table/stats.js");
  page.on("pageerror", e => console.log("pageerror:", e.message));
  try { await page.waitForSelector("#stats-tabs .stats-tab", { timeout: 6000 }); } catch (e) { /* нет устройств/ошибка */ }
  const tabs = await page.$$("#stats-tabs .stats-tab");
  assert(tabs.length >= 1, "stats tabs rendered (" + tabs.length + ")");
  // Статистика -> Графики: каркас
  await page.goto(BASE + "/stats/charts", { waitUntil: "domcontentloaded" });
  assert(new URL(page.url()).pathname === "/stats/charts", "stats/charts page (" + page.url() + ")");

  await browser.close();
  console.log(process.exitCode ? "SMOKE: FAIL" : "SMOKE: PASS");
})().catch(e => { console.error("ERR:", e.message); process.exit(1); });
