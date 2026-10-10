// Захват вкладки «Информация» (обе подзакладки) на разных языках через headless Chrome
// и сбор «кандидатов на локализацию» — строк с кириллицей (RU-артефакты на др. языках).
// Запуск: MALINA_PASS=<пароль> node capture-info.js [baseURL] [outDir]
const { chromium } = require("playwright");
const fs = require("fs");
const path = require("path");

const BASE = process.argv[2] || "http://192.168.13.60";
const OUT = process.argv[3] || "/tmp/kilo/info";
const PASS = process.env.MALINA_PASS || "";
if (!PASS) { console.error("MALINA_PASS required"); process.exit(2); }

const CYR = /[\u0400-\u04FF]/; // кириллица

async function capture(lang) {
  const browser = await chromium.launch({ channel: "chrome" });
  const ctx = await browser.newContext();
  const page = await ctx.newPage();

  await page.goto(BASE + "/login", { waitUntil: "domcontentloaded" });
  await page.fill('input[name="password"]', PASS);
  await Promise.all([page.waitForLoadState("networkidle"), page.click('button[type="submit"]')]);

  // язык
  await page.goto(BASE + "/lang?set=" + lang + "&next=/map%3Fid%3D0", { waitUntil: "domcontentloaded" });
  await page.waitForSelector('[data-tab="info"]', { timeout: 15000 });
  // вкладка «Информация»
  await page.click('[data-tab="info"]');
  await page.waitForTimeout(1500); // ждём первый tick и рендер

  const result = { lang: lang, shots: {}, strings: {} };
  for (const sub of ["main", "system"]) {
    await page.click('[data-subtab="' + sub + '"]');
    await page.waitForTimeout(400);
    const file = path.join(OUT, "info_" + lang + "_" + sub + ".png");
    await page.locator("#tab-info").screenshot({ path: file }).catch(async () => { await page.screenshot({ path: file, fullPage: true }); });
    result.shots[sub] = file;
    // собираем видимые строки ячеек
    const items = await page.$$eval("#tab-info .mi-name, #tab-info .mi-group, #tab-info .mi-subhdr, #tab-info .mi-desc, #tab-info td",
      els => els.map(e => e.textContent.trim()).filter(Boolean));
    result.strings[sub] = items;
  }
  await browser.close();
  return result;
}

(async () => {
  fs.mkdirSync(OUT, { recursive: true });
  const out = {};
  for (const lang of ["en", "zh"]) {
    out[lang] = await capture(lang);
    console.log("captured", lang, "->", JSON.stringify(Object.values(out[lang].shots)));
  }
  // кандидаты: строки с кириллицей (для en/zh это не переведено)
  const cand = new Set();
  for (const lang of ["en", "zh"]) {
    for (const sub of ["main", "system"]) {
      for (const s of out[lang].strings[sub]) {
        if (CYR.test(s)) { cand.add(s.replace(/\s+/g, " ").trim()); }
      }
    }
  }
  const list = [...cand].sort();
  fs.writeFileSync(path.join(OUT, "candidates.txt"), list.join("\n") + "\n", "utf8");
  fs.writeFileSync(path.join(OUT, "capture.json"), JSON.stringify(out, null, 1), "utf8");
  console.log("candidates:", list.length, "->", path.join(OUT, "candidates.txt"));
})().catch(e => { console.error("ERR:", e.message); process.exit(1); });
