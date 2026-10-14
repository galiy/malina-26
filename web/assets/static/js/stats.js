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

  var tabsEl = document.getElementById("stats-tabs");
  var extEl = document.getElementById("stats-extremes");
  var tableEl = document.getElementById("stats-table");
  var upBtn = document.getElementById("stats-up");
  var downBtn = document.getElementById("stats-down");

  function url(q) { return "/api/stats/table?" + q; }

  async function api(u) {
    var r = await fetch(u, { headers: { "Accept": "application/json" } });
    if (!r.ok) throw new Error("http " + r.status);
    return await r.json();
  }

  // Время — по локали клиента, 24-часовой формат (без AM/PM).
  function fmtTime(ms) {
    try { return new Date(ms).toLocaleString(navigator.language || undefined, { hour12: false }); }
    catch (e) { return new Date(ms).toLocaleString(); }
  }
  function fmtRange(mm) {
    if (mm[0] < 0 || mm[1] < 0) return "—";
    return fmtTime(mm[0]) + " … " + fmtTime(mm[1]);
  }

  function renderTabs() {
    tabsEl.innerHTML = "";
    devs.forEach(function (d, i) {
      var b = document.createElement("button");
      b.className = "stats-tab" + (active === d ? " active" : "");
      b.textContent = d.type + " " + d.id; // заголовок в одну строку
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
    try {
      active.ex = await api("/api/stats/extremes?type=" + encodeURIComponent(active.type) + "&id=" + encodeURIComponent(active.id));
    } catch (e) {
      active.ex = {};
    }
    renderExtremes();
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

  function renderTable(rows, cols) {
    if (!rows.length) { tableEl.innerHTML = '<div class="muted">' + T("stats.no_data") + "</div>"; edge.top = edge.bottom = 0; return; }
    // колонки — по контракту (имя поля); fallback — union field_id
    var names = {};
    (cols || []).forEach(function (c) { names[c.id] = c.name; });
    var colIds = (cols || []).map(function (c) { return String(c.id); });
    if (!colIds.length) {
      var s = {}; rows.forEach(function (r) { Object.keys(r.f || {}).forEach(function (k) { s[k] = true; }); });
      colIds = Object.keys(s).sort(function (a, b) { return (+a) - (+b); });
    }
    var html = '<table class="tbl"><thead><tr><th>' + T("stats.time") + "</th><th>" + T("stats.version") + "</th>";
    colIds.forEach(function (c) { html += "<th>" + (names[c] || "f" + c) + "</th>"; });
    html += "</tr></thead><tbody>";
    rows.forEach(function (r) {
      html += "<tr><td>" + fmtTime(r.t) + "</td><td>" + r.v + "</td>";
      colIds.forEach(function (c) { html += "<td>" + (r.f && c in r.f ? r.f[c] : "—") + "</td>"; });
      html += "</tr>";
    });
    html += "</tbody></table>";
    tableEl.innerHTML = html;
    edge.top = rows[0].t;
    edge.bottom = rows[rows.length - 1].t;
  }

  async function query(from, to, dir) {
    if (!active) return;
    last = { from: from, to: to, dir: dir };
    var cb = document.getElementById("stats-detail");
    var detail = cb && cb.checked ? 1 : 0;
    try {
      var j = await api(url("type=" + encodeURIComponent(active.type) + "&id=" + encodeURIComponent(active.id) +
        "&from=" + from + "&to=" + to + "&dir=" + dir + "&limit=" + LIMIT +
        "&rawmin=" + (active.ex && active.ex.raw && active.ex.raw.found ? active.ex.raw.min : 0) + "&detail=" + detail));
      renderTable(j.rows || [], j.columns || []);
      // дисейбл раздельно по границам БД: «Вверх» — есть данные раньше верхней строки,
      // «Вниз» — есть позже нижней (§4.3).
      var om = overallMin();
      upBtn.disabled = !(om >= 0 && edge.top > om);
      downBtn.disabled = false; // в БД идёт запись — снизу всегда могут быть новые данные
    } catch (e) {
      tableEl.innerHTML = '<div class="muted">' + T("stats.no_data") + "</div>";
      upBtn.disabled = downBtn.disabled = true;
    }
  }

  function select(i) {
    active = devs[i];
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
    if (f && t) query(new Date(f).getTime(), new Date(t).getTime(), "from");
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

  api("/api/stats/devices").then(function (j) {
    devs = j.devices || [];
    renderTabs();
    if (devs.length) select(0);
    else tableEl.innerHTML = '<div class="muted">' + T("stats.no_data") + "</div>";
  }).catch(function () {
    tableEl.innerHTML = '<div class="muted">' + T("stats.no_data") + "</div>";
  });
})();
