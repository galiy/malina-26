// Проверка мобильной вёрстки страницы МАП (панель 1) и отсутствия
// горизонтального переполнения. Запуск: MALINA_PASS=... node check-mobile.js [base]
const { chromium } = require("playwright");
const BASE = process.argv[2] || "http://192.168.13.60";
const PASS = process.env.MALINA_PASS;
if (!PASS) { console.error("MALINA_PASS required"); process.exit(2); }

async function run(label, viewport) {
  const browser = await chromium.launch({ channel: "chrome" });
  const ctx = await browser.newContext({ viewport: viewport });
  const page = await ctx.newPage();
  await page.goto(BASE + "/login", { waitUntil: "domcontentloaded" });
  await page.fill('input[name="password"]', PASS);
  await Promise.all([page.waitForLoadState("networkidle"), page.click('button[type="submit"]')]);
  await page.goto(BASE + "/lang?set=ru&next=/map%3Fid%3D0", { waitUntil: "domcontentloaded" });
  await page.waitForSelector("#mpPanel .p1", { timeout: 15000 });
  await page.waitForTimeout(1200);

  async function metrics(tag) {
    return await page.evaluate(() => {
      const de = document.documentElement;
      const over = [];
      document.querySelectorAll("#tab-monitor *").forEach(function (el) {
        if (el.scrollWidth > el.clientWidth + 2 && el.clientWidth > 0 && getComputedStyle(el).overflowX === "visible") {
          over.push((el.className || el.tagName) + " " + el.scrollWidth + ">" + el.clientWidth);
        }
      });
      return { docW: de.scrollWidth, winW: window.innerWidth, over: over.slice(0, 8) };
    });
  }
  const m = await metrics();
  await page.screenshot({ path: "/tmp/kilo/map_" + label + "_monitor.png", fullPage: true });
  // вкладка «Информация»
  await page.click('[data-tab="info"]');
  await page.waitForTimeout(600);
  const off = await page.evaluate(() => ({ docW: document.documentElement.scrollWidth, winW: window.innerWidth }));
  await page.screenshot({ path: "/tmp/kilo/map_" + label + "_info.png", fullPage: true });
  await browser.close();
  return { label, monitor: m, info: off };
}

(async () => {
  const r1 = await run("mob", { width: 390, height: 844 });
  const r2 = await run("desk", { width: 1280, height: 900 });
  for (const r of [r1, r2]) {
    console.log("==", r.label, "monitor:", JSON.stringify(r.monitor));
    console.log("  info:", JSON.stringify(r.info));
  }
})().catch(e => { console.error("ERR:", e.message); process.exit(1); });
