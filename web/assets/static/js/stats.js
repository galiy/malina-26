// stats.js — клиентский рендер страницы «Статистика → Таблицы» (docs/ui-stat.md).
// Сервер лишь отдаёт JSON; закладки/таблица/кнопки рисуются здесь.
(function () {
  "use strict";
  var I = window.I18N || {};
  function T(k) { return I[k] || k; }

  var devs = [], active = null;
  var edge = { top: 0, bottom: 0 }; // времена крайних показанных строк
  var last = null;                   // {from,to,dir} для повторного запроса (чекбокс)
  var LIMIT = 200;

  // «Показать неактуальные устройства»: по умолчанию в закладках только устройства
  // с обновлением телеметрии за последний час. Флаг — в памяти модуля (не персистентен
  // при перезагрузке страницы, сохраняется при переходах между закладками).
  var showStale = false;
  var STALE_MS = 3600 * 1000;
  var staleCb = document.getElementById("stats-show-stale");

  function visibleDevs() {
    var now = Date.now();
    return devs.filter(function (d) {
      return showStale || (d.last && (now - d.last) < STALE_MS);
    });
  }

  var tabsEl = document.getElementById("stats-tabs");
  var extEl = document.getElementById("stats-extremes");
  var tableEl = document.getElementById("stats-table");
  var upBtn = document.getElementById("stats-up");
  var downBtn = document.getElementById("stats-down");

  function esc(s) {
    return String(s == null ? "" : s).replace(/[&<>"']/g, function (c) {
      return { "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" }[c];
    });
  }

  // Всплывающая подсказка заголовка колонки (по щелчку): i18n-ключ описания,
  // fallback — имя поля. Открывается возле заголовка, повторный клик/клик вне — закрыть.
  function hintText(key, fallback) {
    if (!key) return "";
    var v = I[key];
    return (v && v !== key) ? v : (fallback || "");
  }
  var hintEl = null, hintTh = null;
  function hideHint() { if (hintEl) { hintEl.remove(); hintEl = null; hintTh = null; } }
  function toggleHint(th) {
    var txt = hintText(th.getAttribute("data-desc"), th.getAttribute("data-name"));
    if (!txt) return;
    if (hintEl && hintTh === th) { hideHint(); return; }
    hideHint();
    hintEl = document.createElement("div");
    hintEl.className = "stats-hint";
    hintEl.textContent = txt;
    document.body.appendChild(hintEl);
    var r = th.getBoundingClientRect();
    var left = Math.min(Math.max(4, r.left), Math.max(4, window.innerWidth - hintEl.offsetWidth - 4));
    hintEl.style.left = left + "px";
    hintEl.style.top = (r.bottom + 4) + "px";
    hintTh = th;
  }
  document.addEventListener("click", function (e) {
    if (hintEl && !(e.target.closest && e.target.closest(".stats-hint"))) hideHint();
  });

  function url(q) { return "/api/stats/table?" + q; }

  async function api(u) {
    var r = await fetch(u, { headers: { "Accept": "application/json" } });
    if (!r.ok) throw new Error("http " + r.status);
    return await r.json();
  }

  // Время — 24-часовой формат (без AM/PM).
  function pad(x, n) { var s = String(x); n = n || 2; while (s.length < n) s = "0" + s; return s; }
  // Значения полей «С/по» (datetime-local): локальное время с миллисекундами.
  function toLocalInputMS(ms) {
    var d = new Date(ms);
    return d.getFullYear() + "-" + pad(d.getMonth() + 1) + "-" + pad(d.getDate()) + "T" +
      pad(d.getHours()) + ":" + pad(d.getMinutes()) + ":" + pad(d.getSeconds()) + "." + pad(d.getMilliseconds(), 3);
  }
  // Время в колонке «Время» и в диапазонах крайних точек — с миллисекундами.
  function fmtTimeMS(ms) {
    var d = new Date(ms);
    return pad(d.getDate()) + "." + pad(d.getMonth() + 1) + "." + d.getFullYear() + " " +
      pad(d.getHours()) + ":" + pad(d.getMinutes()) + ":" + pad(d.getSeconds()) + "." + pad(d.getMilliseconds(), 3);
  }
  function fmtRange(mm) {
    if (mm[0] < 0 || mm[1] < 0) return "—";
    return fmtTimeMS(mm[0]) + " … " + fmtTimeMS(mm[1]);
  }

  // Наименования сетевых инверторов из конфига grid-reader (по serial).
  var gridNames = {};
  function gridName(id) {
    return gridNames[id] || id;
  }
  function devLabel(d) {
    return d.type === "grid" ? gridName(d.id) : d.type + " " + d.id;
  }
  function renderTabs() {
    tabsEl.innerHTML = "";
    visibleDevs().forEach(function (d, i) {
      var b = document.createElement("button");
      b.className = "stats-tab" + (active === d ? " active" : "");
      b.textContent = devLabel(d); // заголовок в одну строку
      b.onclick = function () { select(i); };
      tabsEl.appendChild(b);
    });
  }

  function exRange(e) {
    if (!e || !e.found) return [-1, -1];
    return [e.min, e.max];
  }
  function renderExtremes() {
    if (!active) { extEl.textContent = ""; return; }
    var ex = active.ex || {};
    extEl.textContent = T("stats.raw") + ": " + fmtRange(exRange(ex.raw)) + "   |   " + T("stats.avg") + ": " + fmtRange(exRange(ex.avg));
  }
  // Крайние точки — отдельным запросом (из памяти tsdb), точечно на активной вкладке.
  async function refreshExtremes() {
    if (!active) return;
    var act = active;
    try {
      act.ex = await api("/api/stats/extremes?type=" + encodeURIComponent(act.type) + "&id=" + encodeURIComponent(act.id));
    } catch (e) {
      act.ex = {};
    }
    if (active === act) renderExtremes();
  }

  function detailOn() {
    var cb = document.getElementById("stats-detail");
    return !!(cb && cb.checked);
  }
  // Границы БД для навигации: при выключенных «Детальных» (только avg) — по avg,
  // иначе — по объединению raw+avg.
  function overallMin() {
    var ex = (active && active.ex) || {};
    var src = detailOn() ? [exRange(ex.raw)[0], exRange(ex.avg)[0]] : [exRange(ex.avg)[0]];
    var m = -1;
    src.forEach(function (x) { if (x >= 0 && (m < 0 || x < m)) m = x; });
    return m;
  }

  // Человеческое значение: сервер уже пересчитал в физические единицы; здесь — округление
  // по единице (В/А/Гц — 2 знака, °C — 1, Вт и коды — целые). null/нет поля → «—».
  function fmtVal(unit, v) {
    if (v === null || v === undefined) return "—";
    if (typeof v === "string") return v; // символ (напр. режим заряда КЭС)
    if (typeof v !== "number" || !isFinite(v)) return "—";
    var p = unit === "°C" ? 1 : (unit === "В" || unit === "А" || unit === "Гц") ? 2 : 0;
    var f = Math.pow(10, p);
    return String(Math.round(v * f) / f);
  }

  function renderTable(rows, cols) {
    if (!rows.length) { tableEl.innerHTML = '<div class="muted">' + T("stats.no_data") + "</div>"; edge.top = edge.bottom = 0; return; }
    // колонки — по контракту (имя поля + единица второй строкой); fallback — union field_id
    var names = {}, units = {}, raws = {}, descs = {};
    (cols || []).forEach(function (c) {
      var nm = c.name;
      if (active && active.type) {
        var k = "stats.f." + active.type + "." + c.name;
        var tv = T(k);
        if (tv && tv !== k) { nm = tv; }
      }
      if (active && active.type === "grid" && (+c.id === 16 || +c.id === 17)) {
        var sofar = active.kind === 1;
        nm = T(+c.id === 16 ? (sofar ? "home.grid.module" : "home.grid.igbt")
          : (sofar ? "home.grid.inner" : "home.grid.radiator"));
      }
      names[c.id] = nm; units[c.id] = c.unit || ""; raws[c.id] = !!c.raw; descs[c.id] = c.desc || "";
    });
    var colIds = (cols || []).map(function (c) { return String(c.id); });
    if (!colIds.length) {
      var s = {}; rows.forEach(function (r) { Object.keys(r.f || {}).forEach(function (k) { s[k] = true; }); });
      colIds = Object.keys(s).sort(function (a, b) { return (+a) - (+b); });
    }
    // У BMS «версия» — это число ячеек (Vn); подписываем колонку соответственно.
    var isCells = !!(active && active.type === "mbms");
    var verHdr = T(isCells ? "stats.cells" : "stats.version");
    var timeHdr = T("stats.time");
    var html = '<table class="tbl"><thead><tr>' +
      '<th class="has-hint" data-desc="stats.hint.time" data-name="' + esc(timeHdr) + '">' + timeHdr + "</th>" +
      '<th class="has-hint" data-desc="' + (isCells ? "stats.hint.cells" : "stats.hint.version") +
      '" data-name="' + esc(verHdr) + '">' + verHdr + "</th>";
    colIds.forEach(function (c) {
      // «сырое» поле (без пересчёта) помечаем звёздочкой с подсказкой (R6)
      var mark = raws[c] ? ' <sup title="' + esc(T("stats.raw_field")) + '">*</sup>' : "";
      var nm = names[c] || ("f" + c);
      html += '<th class="has-hint" data-desc="' + esc(descs[c] || "") + '" data-name="' + esc(nm) + '">' +
        nm + mark + (units[c] ? '<div class="unit">' + units[c] + "</div>" : "") + "</th>";
    });
    html += "</tr></thead><tbody>";
    rows.forEach(function (r) {
      html += "<tr><td>" + fmtTimeMS(r.t) + "</td><td>" + r.v + "</td>";
      colIds.forEach(function (c) {
        var v = r.f && c in r.f ? r.f[c] : null;
        if (active && active.type === "grid" && +c >= 19 && +c <= 24 && typeof v === "number") {
          var hx = (v >>> 0).toString(16).toUpperCase(); html += "<td>0x" + ("0000" + hx).slice(-4) + "</td>";
        } else {
          html += "<td>" + fmtVal(units[c], v) + "</td>";
        }
      });
      html += "</tr>";
    });
    html += "</tbody></table>";
    tableEl.innerHTML = html;
    edge.top = rows[0].t;
    edge.bottom = rows[rows.length - 1].t;
    // Актуализируем поля «С/по» фактическим окном полученных данных (с мс).
    var tmin = rows[0].t, tmax = rows[0].t;
    rows.forEach(function (r) { if (r.t < tmin) tmin = r.t; if (r.t > tmax) tmax = r.t; });
    var fEl = document.getElementById("st-from"), tEl = document.getElementById("st-to");
    if (fEl) fEl.value = toLocalInputMS(tmin);
    if (tEl) tEl.value = toLocalInputMS(tmax);
  }

  // Хинт открывается по щелчку на заголовке колонки (таблица перерисовывается —
  // слушатель на контейнере, не на ячейках).
  tableEl.addEventListener("click", function (e) {
    var th = e.target.closest && e.target.closest("th.has-hint");
    if (th) { e.stopPropagation(); toggleHint(th); }
  });

  var querySeq = 0;
  async function query(from, to, dir) {
    if (!active) return;
    var act = active;
    var seq = ++querySeq;
    last = { from: from, to: to, dir: dir };
    var cb = document.getElementById("stats-detail");
    var detail = cb && cb.checked ? 1 : 0;
    try {
      var j = await api(url("type=" + encodeURIComponent(act.type) + "&id=" + encodeURIComponent(act.id) +
        "&from=" + from + "&to=" + to + "&dir=" + dir + "&limit=" + LIMIT +
        "&rawmin=" + (act.ex && act.ex.raw && act.ex.raw.found ? act.ex.raw.min : 0) + "&detail=" + detail));
      if (seq !== querySeq || active !== act) return;
      renderTable(j.rows || [], j.columns || []);
      // дисейбл раздельно по границам БД: «Вверх» — есть данные раньше верхней строки,
      // «Вниз» — есть позже нижней (§4.3).
      var om = overallMin();
      upBtn.disabled = !(om >= 0 && edge.top > om);
      downBtn.disabled = false; // в БД идёт запись — снизу всегда могут быть новые данные
    } catch (e) {
      if (seq === querySeq && active === act) {
        tableEl.innerHTML = '<div class="muted">' + T("stats.no_data") + "</div>";
        upBtn.disabled = downBtn.disabled = true;
      }
    }
  }

  function select(i) {
    var vis = visibleDevs();
    if (i < 0 || i >= vis.length) { i = 0; }
    active = vis[i];
    if (!active) return;
    active.ex = active.ex || {};
    renderTabs();
    refreshExtremes();
    var to = Date.now(), from = to - 24 * 3600 * 1000;
    query(from, to, "from");
  }

  function period(name) {
    var now = Date.now(), d = new Date();
    var dayStart = new Date(d.getFullYear(), d.getMonth(), d.getDate()).getTime();
    switch (name) {
      case "24h": return query(now - 24 * 3600 * 1000, now, "from");
      case "today": return query(dayStart, now, "from");
      case "yesterday": return query(dayStart - 24 * 3600 * 1000, dayStart, "from");
      case "7d": return query(now - 7 * 24 * 3600 * 1000, now, "from");
      case "month": return query(now - 30 * 24 * 3600 * 1000, now, "from"); // «Месяц» = 30 дней (MVP; не календарный месяц)
      case "last": return query(0, now, "to"); // хвост БД
    }
  }

  document.querySelectorAll("[data-period]").forEach(function (b) {
    b.onclick = function () { period(b.getAttribute("data-period")); };
  });
  document.getElementById("st-show").onclick = function () {
    var f = document.getElementById("st-from").value, t = document.getElementById("st-to").value;
    var fn = f ? new Date(f).getTime() : NaN, tn = t ? new Date(t).getTime() : NaN;
    if (isFinite(fn) && isFinite(tn) && fn <= tn) query(fn, tn, "from");
  };
  document.getElementById("st-day-show").onclick = function () {
    var v = document.getElementById("st-day").value;
    if (!v) return;
    var s = new Date(v + "T00:00").getTime();
    query(s, s + 24 * 3600 * 1000, "from");
  };
  // Включительно (to = крайняя верхняя, from = крайняя нижняя): крайняя строка
  // захватывается в новую выборку (перекрытие на одну строку), без провала в пустоту.
  upBtn.onclick = function () { if (edge.top > 0) query(0, edge.top, "to"); };
  downBtn.onclick = function () { if (edge.bottom > 0) query(edge.bottom, Date.now(), "from"); };

  // Чекбокс «Детальные данные»: при изменении — обновление текущего представления.
  var detailCb = document.getElementById("stats-detail");
  if (detailCb) detailCb.onchange = function () { if (last) query(last.from, last.to, last.dir); };

  // «Показать неактуальные устройства»: перерисовка закладок. Если активное
  // устройство оказалось скрыто фильтром — выбираем первое видимое.
  if (staleCb) staleCb.onchange = function () {
    showStale = staleCb.checked;
    select(visibleDevs().indexOf(active));
  };

  Promise.all([
    api("/api/stats/devices"),
    api("/api/grid/status").catch(function () { return { inverters: [] }; })
  ]).then(function (res) {
    var j = res[0], st = res[1];
    ((st && st.inverters) || []).forEach(function (x) {
      if (x && x.serial && x.name) gridNames[x.serial] = x.name;
    });
    devs = j.devices || [];
    renderTabs();
    var vis = visibleDevs();
    if (vis.length) select(0);
    else tableEl.innerHTML = '<div class="muted">' + T("stats.no_data") + "</div>";
  }).catch(function () {
    tableEl.innerHTML = '<div class="muted">' + T("stats.no_data") + "</div>";
  });
})();
