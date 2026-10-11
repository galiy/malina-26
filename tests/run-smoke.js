// Smoke-тест через headless Chrome (Playwright) на системном Chrome.
// Запуск: node run-smoke.js [baseURL]   (по умолчанию http://192.168.13.60)
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
  assert(/\/map(\?|$)/.test(page.url()), "login ok -> /map (" + page.url() + ")");

  // Меню
  const html = await page.content();
  assert(/Сеть|Network|Настройка сети/.test(html), "menu present");
  assert(/Информация о системе|System information/.test(html), "info menu present");

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

  await browser.close();
  console.log(process.exitCode ? "SMOKE: FAIL" : "SMOKE: PASS");
})().catch(e => { console.error("ERR:", e.message); process.exit(1); });
