// Проверка пулинга nav.js без живого устройства: запросы перехватываются,
// время управляется Playwright Clock. Требования — docs/ui-navigation.md,
// N4.1–N4.3.
//
// Запуск (нужен Node 18+ и установленный Playwright):
//   node run-nav-poll.js
const { chromium } = require("playwright");
const fs = require("fs");
const path = require("path");

const NAV_SRC = fs.readFileSync(
  path.join(__dirname, "../web/assets/static/nav.js"),
  "utf8"
);

function assert(cond, msg) {
  if (!cond) {
    console.error("FAIL:", msg);
    process.exitCode = 1;
  } else {
    console.log("ok:", msg);
  }
}

(async () => {
  const browser = await chromium.launch({ channel: "chrome" });
  const page = await browser.newPage();

  const counts = { map: 0, kes: 0, bms: 0 };
  let blockBms = false;
  let heldBms = null;

  await page.route("**/api/*/devices", async route => {
    const u = new URL(route.request().url());
    const type = u.pathname.split("/")[2];
    counts[type] = (counts[type] || 0) + 1;
    if (type === "bms" && blockBms) {
      heldBms = route;
      return;
    }
    await route.fulfill({
      status: 200,
      contentType: "application/json",
      body: "[]"
    });
  });

  await page.route("http://nav.test/", route => route.fulfill({
    status: 200,
    contentType: "text/html",
    body: '<div id="mapMenuItems"></div><div id="kesMenuItems"></div><div id="bmsMenuItems"></div>'
  }));
  await page.goto("http://nav.test/");
  await page.clock.install();
  await page.addScriptTag({ content: NAV_SRC });
  await page.waitForTimeout(50);

  const total = () => counts.map + counts.kes + counts.bms;
  const step = async (ms) => {
    await page.clock.fastForward(ms);
    await page.waitForTimeout(40);
  };

  assert(counts.map === 1 && counts.kes === 1 && counts.bms === 1,
    "старт: по одному запросу на эндпоинт");

  for (let i = 0; i < 6; i++) { await step(5000); }
  assert(total() === 21, "первые 30 с: 7 опросов = 21 запрос (получено " + total() + ")");

  await page.evaluate(() => Object.defineProperty(document, "hidden", {
    configurable: true,
    get: () => true
  }));
  await step(30000);
  assert(total() === 21, "в фоне опроса нет");

  await page.evaluate(() => Object.defineProperty(document, "hidden", {
    configurable: true,
    get: () => false
  }));
  await page.evaluate(() => document.dispatchEvent(new Event("visibilitychange")));
  await page.waitForTimeout(50);
  assert(total() === 24, "возврат вкладки: один опрос (получено " + total() + ")");

  await page.evaluate(() => window.dispatchEvent(new Event("focus")));
  await page.waitForTimeout(30);
  assert(total() === 24, "focus в пределах 2 с подавлен");

  await step(2000);
  await page.evaluate(() => window.dispatchEvent(new Event("focus")));
  await page.waitForTimeout(50);
  assert(total() === 27, "focus после 2 с опрашивает (получено " + total() + ")");

  blockBms = true;
  await step(2000);
  await page.evaluate(() => window.dispatchEvent(new Event("focus")));
  await page.waitForTimeout(50);
  const bmsBefore = counts.bms;
  assert(heldBms !== null, "bms-запрос удержан для проверки in-flight");

  await step(2000);
  await page.evaluate(() => window.dispatchEvent(new Event("focus")));
  await page.waitForTimeout(50);
  assert(counts.bms === bmsBefore, "второй bms-запрос не стартует, пока первый в полёте");

  blockBms = false;
  await heldBms.fulfill({ status: 200, contentType: "application/json", body: "[]" });
  await page.waitForTimeout(50);
  await step(2000);
  await page.evaluate(() => window.dispatchEvent(new Event("focus")));
  await page.waitForTimeout(50);
  assert(counts.bms === bmsBefore + 1, "после завершения bms опрос возобновляется");

  // Зависший запрос не должен блокировать эндпоинт навсегда: guard 10 с
  // снимает in-flight, следующий опрос снова уходит.
  blockBms = true;
  heldBms = null;
  await step(2000);
  await page.evaluate(() => window.dispatchEvent(new Event("focus")));
  await page.waitForTimeout(50);
  const bmsTimeoutBefore = counts.bms;
  assert(heldBms !== null, "bms-запрос удержан для проверки таймаута");
  await page.clock.fastForward(11000);
  await page.waitForTimeout(50);
  await step(2000);
  await page.evaluate(() => window.dispatchEvent(new Event("focus")));
  await page.waitForTimeout(50);
  assert(counts.bms === bmsTimeoutBefore + 1, "после таймаута bms-запрос повторяется");

  // Удержанный route после abort страницей мог остаться незавершённым.
  if (heldBms) { await heldBms.abort().catch(() => {}); }

  await browser.close();
  console.log(process.exitCode ? "NAV-POLL: FAIL" : "NAV-POLL: PASS");
})().catch(e => {
  console.error("ERR:", e.message);
  process.exit(1);
});
