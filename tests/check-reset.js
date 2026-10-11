// Проверка тройного подтверждения кнопки Reset на вкладке «Управление» (Playwright).
// Первые два диалога — «ОК», третий — «Отмена» (реальный Reset НЕ отправляем).
// Запуск: MALINA_PASS=<пароль> node check-reset.js [baseURL] [mapId]
const { chromium } = require("playwright");

const BASE = process.argv[2] || process.env.MALINA_WEB || "http://malina.local";
const MAPID = process.argv[3] || "0";
const PASS = process.env.MALINA_PASS || "";
if (!PASS) { console.error("задайте MALINA_PASS=<пароль>"); process.exit(2); }

(async () => {
  const browser = await chromium.launch({ channel: "chrome" });
  const ctx = await browser.newContext();
  const page = await ctx.newPage();

  let n = 0;
  const seen = [];
  page.on("dialog", async (d) => {
    n++;
    seen.push(d.message());
    if (n < 3) { await d.accept(); } else { await d.dismiss(); } // 3-й — отмена
  });
  page.on("console", (m) => { if (m.type() === "error") console.log("console.error:", m.text()); });

  await page.goto(BASE + "/login", { waitUntil: "domcontentloaded" });
  await page.fill('input[name="password"]', PASS);
  await page.click('button[type="submit"]');
  await page.waitForLoadState("networkidle");

  await page.goto(BASE + "/map?id=" + MAPID, { waitUntil: "domcontentloaded" });
  await page.click('.map-tab[data-tab="control"]');
  await page.waitForSelector('#tab-control button[data-cmd="reset"]', { timeout: 10000 });
  await page.click('#tab-control button[data-cmd="reset"]');
  await page.waitForTimeout(1500);

  console.log("диалогов:", n);
  seen.forEach((m) => console.log(" -", m));
  console.log(n === 3 ? "RESET: 3 переспроса — OK" : "RESET: FAIL (ожидали 3, получили " + n + ")");
  await browser.close();
  process.exitCode = (n === 3) ? 0 : 1;
})().catch((e) => { console.error("ERR:", e.message); process.exit(1); });
