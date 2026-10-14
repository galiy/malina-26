// API e2e для malina-web: авторизация (cookie || Bearer), скользящие сессии,
// /api/token, /api/map/{devices,raw,write}, legacy /write_eeprom.php, смена пароля.
//
// Запуск: MALINA_PASS=<пароль> node run-api-e2e.js [baseURL]
//   baseURL — напр. http://<адрес-устройства> или https://<внешний-хост>
//   (по умолчанию $MALINA_WEB или http://malina.local)
//
// Мутирующие проверки — по умолчанию ВЫКЛЮЧЕНЫ, включаются явно:
//   MALINA_NEWPASS=<пароль>  — полный тест смены пароля (смена и возврат назад);
//   MALINA_TEST_WRITE=1      — реальная запись байта в МАП через /api/map/write.
//
// Непокрываемое здесь (ручные проверки, см. docs/api-auth.md):
//   - скользящий TTL сессии/токена (idle 1 ч) — проверяется по времени/перезапуску;
//   - переживание рестарта web (состояние в /dev/shm/malina-auth) — нужен systemctl.
const BASE = (process.argv[2] || process.env.MALINA_WEB || "http://malina.local").replace(/\/+$/, "");
const PASS = process.env.MALINA_PASS || "";
const NEWPASS = process.env.MALINA_NEWPASS || "";
const TEST_WRITE = process.env.MALINA_TEST_WRITE === "1";
if (!PASS) { console.error("задайте MALINA_PASS=<пароль>"); process.exit(2); }

let fails = 0;
function ok(cond, msg) {
  if (cond) { console.log("ok:", msg); } else { fails++; console.error("FAIL:", msg); }
}

function sessionCookie(res) {
  let list = [];
  if (typeof res.headers.getSetCookie === "function") list = res.headers.getSetCookie();
  else if (res.headers.raw) list = res.headers.raw()["set-cookie"] || [];
  for (const c of list) {
    const m = /(?:^|\s)malina_session=([^;]+)/.exec(c);
    if (m) return "malina_session=" + m[1];
  }
  return "";
}

async function req(path, { method = "GET", cookie, bearer, form, json, basic } = {}) {
  const headers = {};
  if (cookie) headers["Cookie"] = cookie;
  if (bearer) headers["Authorization"] = "Bearer " + bearer;
  if (basic) headers["Authorization"] = "Basic " + Buffer.from(basic, "utf8").toString("base64");
  let body;
  if (form) { headers["Content-Type"] = "application/x-www-form-urlencoded"; body = new URLSearchParams(form).toString(); }
  else if (json !== undefined) { headers["Content-Type"] = "application/json"; body = JSON.stringify(json); }
  return fetch(BASE + path, { method, headers, body, redirect: "manual" });
}

async function login(password) {
  const res = await req("/login", { method: "POST", form: { password } });
  return { status: res.status, cookie: sessionCookie(res) };
}

(async () => {
  console.log("base:", BASE);

  // 1. Без авторизации: API отдаёт 401
  for (const p of ["/api/system", "/api/processes", "/api/map/devices", "/api/map/raw?id=0&gen=0"]) {
    const r = await req(p, { method: p.includes("raw") ? "GET" : "GET" });
    ok(r.status === 401, "no auth " + p + " -> 401 (got " + r.status + ")");
  }
  {
    const r = await req("/api/map/write", { method: "POST", json: {} });
    ok(r.status === 401, "no auth POST /api/map/write -> 401 (got " + r.status + ")");
  }
  {
    const r = await req("/api/token", { method: "DELETE" });
    ok(r.status === 401, "no auth DELETE /api/token -> 401 (got " + r.status + ")");
  }

  // 2. Битый Bearer -> 401
  {
    const r = await req("/api/map/devices", { bearer: "deadbeef-not-a-token" });
    ok(r.status === 401, "bad Bearer -> 401 (got " + r.status + ")");
  }

  // 3. Логин: неверный -> 401, верный -> 303 + cookie
  {
    const bad = await login("wrong-" + Date.now());
    ok(bad.status === 401, "login wrong password -> 401 (got " + bad.status + ")");
    const good = await login(PASS);
    ok(good.status === 303 && good.cookie !== "", "login ok -> 303 + session cookie (got " + good.status + ")");
    globalThis.cookie = good.cookie;
  }

  // 4. Cookie-сессия: /api/system и /api/map/devices
  {
    const r = await req("/api/system", { cookie: globalThis.cookie });
    ok(r.status === 200, "cookie /api/system -> 200 (got " + r.status + ")");
    const r2 = await req("/api/map/devices", { cookie: globalThis.cookie });
    ok(r2.status === 200, "cookie /api/map/devices -> 200 (got " + r2.status + ")");
    let dev = [];
    try { dev = await r2.json(); } catch (e) { /* ignore */ }
    ok(Array.isArray(dev), "devices is JSON array");
    globalThis.dev = Array.isArray(dev) && dev.length ? dev[0] : null;
  }

  // 5. /api/map/raw (cookie) — только при наличии устройства
  if (globalThis.dev) {
    const r = await req("/api/map/raw?id=" + globalThis.dev.id + "&gen=" + globalThis.dev.gen, { cookie: globalThis.cookie });
    ok(r.status === 200, "cookie /api/map/raw(id,gen) -> 200 (got " + r.status + ")");
    const bad = await req("/api/map/raw?id=" + globalThis.dev.id + "&gen=0", { cookie: globalThis.cookie });
    ok(bad.status === 404, "cookie /api/map/raw wrong gen -> 404 (got " + bad.status + ")");
    const nog = await req("/api/map/raw?id=" + globalThis.dev.id, { cookie: globalThis.cookie });
    ok(nog.status === 400, "cookie /api/map/raw without gen -> 400 (got " + nog.status + ")");
  } else {
    console.log("skip: устройств типа map нет — /api/map/raw не проверяем");
  }

  // 6. /api/token: выпуск, работа по Bearer, отзыв
  {
    const bad = await req("/api/token", { method: "POST", json: { password: "wrong-" + Date.now() } });
    ok(bad.status === 401, "POST /api/token wrong password -> 401 (got " + bad.status + ")");

    const r = await req("/api/token", { method: "POST", json: { password: PASS } });
    ok(r.status === 200, "POST /api/token -> 200 (got " + r.status + ")");
    let tok = "";
    try { tok = (await r.json()).token || ""; } catch (e) { /* ignore */ }
    ok(tok.length > 0, "token issued");
    globalThis.token = tok;

    const d = await req("/api/map/devices", { bearer: tok });
    ok(d.status === 200, "Bearer /api/map/devices -> 200 (got " + d.status + ")");

    if (globalThis.dev) {
      const raw = await req("/api/map/raw?id=" + globalThis.dev.id + "&gen=" + globalThis.dev.gen, { bearer: tok });
      ok(raw.status === 200, "Bearer /api/map/raw -> 200 (got " + raw.status + ")");
    }

    const del = await req("/api/token", { method: "DELETE", bearer: tok });
    ok(del.status === 200, "DELETE /api/token -> 200 (got " + del.status + ")");
    const after = await req("/api/map/devices", { bearer: tok });
    ok(after.status === 401, "revoked Bearer -> 401 (got " + after.status + ")");
  }

  // 7. /api/map/write: 401 без auth, 405 на GET, 400 на кривой JSON
  {
    const g = await req("/api/map/write", { cookie: globalThis.cookie });
    ok(g.status === 405, "cookie GET /api/map/write -> 405 (got " + g.status + ")");
    const badj = await req("/api/map/write", { method: "POST", cookie: globalThis.cookie, json: { id: "x" } });
    ok([400, 404, 422, 502].includes(badj.status), "cookie POST /api/map/write bad body -> 4xx/5xx (got " + badj.status + ")");
    if (TEST_WRITE && globalThis.dev) {
      // Реальная запись: читаем текущее значение регистра и пишем то же (идемпотентно).
      const mem = await req("/read_memory.php?offset=27&count=1"); // адрес 0x1B как пример
      const arr = await mem.json();
      const cur = arr[0];
      const w = await req("/api/map/write", {
        method: "POST", cookie: globalThis.cookie,
        json: { id: globalThis.dev.id, gen: globalThis.dev.gen, ops: [{ op: "reg", addr: "1B", data: ("0" + (cur & 0xff).toString(16)).slice(-2).toUpperCase() }] },
      });
      ok(w.status === 200, "cookie POST /api/map/write real (same value) -> 200 (got " + w.status + ")");
    } else {
      console.log("skip: реальная запись /api/map/write (включить MALINA_TEST_WRITE=1)");
    }
  }

  // 8. legacy /write_eeprom.php (Basic+PAM): без/с неверным Basic -> 401, кривой data -> error
  {
    const r = await req("/write_eeprom.php", { method: "POST", form: { data: "[[1,2]]" } });
    ok(r.status === 401, "write_eeprom no Basic -> 401 (got " + r.status + ")");
    const ctype = r.headers.get("content-type") || "";
    ok(/text\/html/.test(ctype), "write_eeprom 401 content-type text/html (" + ctype + ")");

    const wb = await req("/write_eeprom.php", { method: "POST", basic: "malina:wrong-" + Date.now(), form: { data: "[[1,2]]" } });
    ok(wb.status === 401, "write_eeprom wrong Basic -> 401 (got " + wb.status + ")");

    const okd = await req("/write_eeprom.php", { method: "POST", basic: "malina:" + PASS, form: { data: "not-json" } });
    const body = await okd.text();
    ok(okd.status === 200 && body.trim() === "error", "write_eeprom bad data -> 200 'error' (got " + okd.status + " '" + body.trim() + "')");

    const g = await req("/write_eeprom.php", { method: "GET" });
    ok(g.status === 405, "write_eeprom GET -> 405 (got " + g.status + ")");
  }

  // 8.5. Статистика: /api/stats/devices, /api/stats/table (docs/ui-stat.md)
  {
    let r = await req("/api/stats/devices");
    ok(r.status === 401, "no auth /api/stats/devices -> 401 (got " + r.status + ")");
    r = await req("/api/stats/table?type=map&id=1&from=0&to=1&dir=from&limit=2");
    ok(r.status === 401, "no auth /api/stats/table -> 401 (got " + r.status + ")");
    r = await req("/api/stats/extremes?type=map&id=1");
    ok(r.status === 401, "no auth /api/stats/extremes -> 401 (got " + r.status + ")");

    const dv = await req("/api/stats/devices", { cookie: globalThis.cookie });
    ok(dv.status === 200, "cookie /api/stats/devices -> 200 (got " + dv.status + ")");
    let devs = [];
    try { devs = (await dv.json()).devices || []; } catch (e) { /* ignore */ }
    ok(Array.isArray(devs), "/api/stats/devices returns devices[]");

    if (devs.length) {
      const d = devs[0];
      const now = Date.now();
      const t = await req("/api/stats/table?type=" + encodeURIComponent(d.type) + "&id=" + encodeURIComponent(d.id) +
        "&from=0&to=" + now + "&dir=from&limit=2", { cookie: globalThis.cookie });
      ok(t.status === 200, "cookie /api/stats/table -> 200 (got " + t.status + ")");
      let body = {};
      try { body = await t.json(); } catch (e) { /* ignore */ }
      ok(Array.isArray(body.rows) && Array.isArray(body.columns), "/api/stats/table rows[]+columns[]");
      ok((body.columns || []).every((c) => typeof c.unit === "string"), "/api/stats/table columns have unit");
      ok((body.columns || []).every((c) => typeof c.raw === "boolean"), "/api/stats/table columns have raw");
      const bd = await req("/api/stats/table?type=" + encodeURIComponent(d.type) + "&id=" + encodeURIComponent(d.id) +
        "&from=200&to=100&dir=from&limit=2", { cookie: globalThis.cookie });
      ok(bd.status === 400, "/api/stats/table from>to -> 400 (got " + bd.status + ")");
      const ex = await req("/api/stats/extremes?type=" + encodeURIComponent(d.type) + "&id=" + encodeURIComponent(d.id),
        { cookie: globalThis.cookie });
      ok(ex.status === 200, "cookie /api/stats/extremes -> 200 (got " + ex.status + ")");
      let eb = {};
      try { eb = await ex.json(); } catch (e) { /* ignore */ }
      ok(eb && typeof eb.raw === "object" && typeof eb.avg === "object", "/api/stats/extremes returns {raw,avg}");
    } else {
      console.log("skip: устройств в tsdb нет — /api/stats/table не проверяем");
    }
  }

  // 9. Смена пароля -> PurgeAll (мутирующий; только при MALINA_NEWPASS)
  if (NEWPASS) {
    const ch = await req("/system/password", { method: "POST", cookie: globalThis.cookie, form: { password_new: NEWPASS, password_confirm: NEWPASS } });
    ok(ch.status === 303, "change password -> 303 (got " + ch.status + ")");
    const old = await req("/api/system", { cookie: globalThis.cookie });
    ok(old.status === 401, "old cookie purged after change -> 401 (got " + old.status + ")");
    const relogin = await login(NEWPASS);
    ok(relogin.status === 303 && relogin.cookie !== "", "login with new password -> 303");
    const back = await req("/system/password", { method: "POST", cookie: relogin.cookie, form: { password_new: PASS, password_confirm: PASS } });
    ok(back.status === 303, "restore password -> 303 (got " + back.status + ")");
    const fin = await login(PASS);
    ok(fin.status === 303, "login with restored password -> 303 (got " + fin.status + ")");
  } else {
    // Не мутируем: проверяем только несовпадение паролей (без изменения)
    const mm = await req("/system/password", { method: "POST", cookie: globalThis.cookie, form: { password_new: "aaaa1", password_confirm: "bbbb2" } });
    ok(mm.status === 303 && /err=password_mismatch/.test(mm.headers.get("location") || ""),
      "password mismatch -> 303 err=password_mismatch (got " + mm.status + " " + (mm.headers.get("location") || "") + ")");
    console.log("skip: полный тест смены пароля (включить MALINA_NEWPASS=<пароль>)");
  }

  console.log(fails ? ("API E2E: FAIL (" + fails + ")") : "API E2E: PASS");
  process.exitCode = fails ? 1 : 0;
})().catch((e) => { console.error("ERR:", e && e.message ? e.message : e); process.exit(1); });
