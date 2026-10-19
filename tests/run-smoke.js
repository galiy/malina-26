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

// Подпункты меню МАП/КЭС/БМС должны быть корневыми абсолютными на любой
// странице, в т.ч. вложенной (/stats/tables, /bms/settings). Относительный
// href разрешается от каталога страницы и даёт /stats/bms, /bms/bms и 404
// (docs/ui-navigation.md, N1/N7).
async function assertMenuPaths(page, where) {
  const expected = { map: "/map", kes: "/kes", bms: "/bms" };
  const fixed = await page.$$eval(
    'a[href="/map/settings"], a[href="/kes/settings"], a[href="/bms/settings"]',
    as => as.map(a => a.getAttribute("href"))
  );
  assert(fixed.length >= 3, where + ": статические ссылки настроек абсолютные");
  const counts = await page.evaluate(async () => {
    const out = {};
    for (const type of ["map", "kes", "bms"]) {
      const r = await fetch("/api/" + type + "/devices", { cache: "no-store" });
      out[type] = r.ok ? (await r.json()).length : 0;
    }
    return out;
  });
  for (const type of ["map", "kes", "bms"]) {
    // Ждём именно клиентскую перерисовку nav.js, а не серверный рендер:
    // маркер data-nav-loaded выставляется только после fetch+render.
    await page.waitForSelector("#" + type + "MenuItems[data-nav-loaded='1']", {
      state: "attached",
      timeout: 5000
    });
    if (!counts[type]) {
      console.warn("skip:", where, type + " devices not found");
      continue;
    }
    await page.waitForFunction(
      ([t, n]) => document.querySelectorAll("#" + t + "MenuItems a").length >= n,
      [type, counts[type]],
      { timeout: 5000 }
    );
    const links = await page.$$eval("#" + type + "MenuItems a", as => as.map(a => ({
      path: new URL(a.href).pathname,
      href: a.getAttribute("href")
    })));
    for (const l of links) {
      assert(l.href.charAt(0) === "/", where + ": " + type + " menu href is absolute (" + l.href + ")");
      assert(l.path === expected[type], where + ": " + type + " menu path " + l.path + " (ожидали " + expected[type] + ")");
      assert(/[?&]id=/.test(l.href) && /[?&]gen=/.test(l.href), where + ": " + type + " menu has id/gen");
      if (type === "bms") {
        assert(/[?&]serial=/.test(l.href), where + ": bms menu has serial");
      }
    }
  }
}

// Переход по пункту меню БМС с вложенной страницы: раньше он вёл на
// /stats/bms и давал 404. Нужен реальный ответ 200 и путь /bms.
async function assertBmsLinkOpens(page, where) {
  const link = await page.$("#bmsMenuItems a");
  if (!link) {
    console.warn("skip:", where, "no BMS device for click-through");
    return;
  }
  const [resp] = await Promise.all([
    page.waitForResponse(
      r => r.request().isNavigationRequest() && new URL(r.url()).pathname === "/bms",
      { timeout: 7000 }
    ),
    // Пункт лежит в закрытом dropdown (display:none); DOM-клик проверяет
    // навигацию без требования видимости, как это делает пользователь после
    // раскрытия меню.
    page.$eval("#bmsMenuItems a", a => a.click())
  ]);
  await page.waitForURL(u => new URL(u).pathname === "/bms", { timeout: 7000 });
  assert(resp.status() === 200, where + ": BMS menu link opens without 404 (" + resp.status() + ")");
  assert(new URL(page.url()).pathname === "/bms", where + ": BMS menu click -> /bms (" + page.url() + ")");
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
  await assertMenuPaths(page, "/");

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
  await assertMenuPaths(page, "/stats/tables");
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
  await assertMenuPaths(page, "/stats/charts");

  // Вложенная страница настроек: раньше здесь появлялись /bms/bms.
  await page.goto(BASE + "/bms/settings", { waitUntil: "domcontentloaded" });
  assert(new URL(page.url()).pathname === "/bms/settings", "bms/settings page (" + page.url() + ")");
  await assertMenuPaths(page, "/bms/settings");

  // Точный сценарий дефекта: с /stats/tables клик по БМС должен открыть /bms.
  await page.goto(BASE + "/stats/tables", { waitUntil: "domcontentloaded" });
  await assertMenuPaths(page, "/stats/tables#click");
  await assertBmsLinkOpens(page, "/stats/tables");

  await browser.close();
  console.log(process.exitCode ? "SMOKE: FAIL" : "SMOKE: PASS");
})().catch(e => { console.error("ERR:", e.message); process.exit(1); });
