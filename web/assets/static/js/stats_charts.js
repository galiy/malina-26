'use strict';
// Страница «Графики» (docs/ui-stat.md §3). Исторические ряды из tsdb, карточки
// по предустановленным наборам, Chart.js (локально), синхронные курсор/хинты,
// зум 2 вида, drag-панорама, чипы-легенда, порядок карточек (localStorage по типу).
(function () {
  var I = window.I18N || {};
  function T(k) { return I[k] || k; }
  function esc(s) { return String(s == null ? "" : s).replace(/[&<>"]/g, function (c) { return { "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;" }[c]; }); }
  function fmtTime(ms) { var d = new Date(ms), p = function (x) { return (x < 10 ? "0" : "") + x; }; return d.getFullYear() + "-" + p(d.getMonth() + 1) + "-" + p(d.getDate()) + " " + p(d.getHours()) + ":" + p(d.getMinutes()); }
  function api(q) { return fetch(q, { credentials: "same-origin" }).then(function (r) { return r.ok ? r.json() : null; }); }

  var tabsEl = document.getElementById("stats-tabs");
  var chartsEl = document.getElementById("stats-charts");
  var reqSeq = 0;
  var followSeq = 0;
  var compareMode = false;
  var compareWrap = document.getElementById("ch-compare-wrap");
  var compareCb = document.getElementById("ch-compare");
  if (!chartsEl) return;

  Chart.register(ChartZoom);
  var COLORS = ["#428bca", "#5cb85c", "#f0ad4e", "#d9534f", "#5bc0de", "#9463b8", "#17a2b8", "#e91e63", "#4db6ac", "#8d6e63", "#6d9ee8", "#c3b91c", "#4c9f70", "#9c6bcf", "#f7b32b", "#20a4f3"];

  // --- Наборы (docs/ui-stat.md §3.2) ---
  var SETS = {
    map: [
      { t: "stats.c.v", lines: [
        { n: "stats.f.map.UNET", k: "1" },
        { n: "stats.f.map.UOUTmed", k: "6" },
        { n: "stats.f.map.UAcc_med", k: "8" }] },
      { t: "stats.c.i", lines: [
        { n: "stats.f.map.INET_16_4", k: "3" },
        { n: "stats.f.map.IAcc_med_A_u16", k: "10" },
        { n: "stats.n.Imppt", calc: "imppt" },
        { n: "stats.n.Iout", calc: "iout" }] },
      { t: "stats.c.p", lines: [
        { n: "stats.n.Pnet", calc: "pnet" },
        { n: "stats.n.Pbat", calc: "pbat" },
        { n: "stats.n.Pout", calc: "pout" },
        { n: "stats.n.Pmppt", calc: "pmppt" }] },
      { t: "stats.c.f", lines: [
        { n: "stats.f.map.TFNET", k: "7" },
        { n: "stats.f.map.ThFMAP", k: "28" }] },
      { t: "stats.c.t", lines: [
        { n: "stats.f.map.Temp_Grad0", k: "14" },
        { n: "stats.f.map.Temp_Grad1", k: "15" },
        { n: "stats.f.map.Temp_Grad2", k: "16" }] },
      { t: "stats.c.i_kes", lines: [
        { n: "stats.f.map.I_Akb_MPPT0", k: "29" },
        { n: "stats.f.map.I_Akb_MPPT1", k: "30" },
        { n: "stats.f.map.I_Akb_MPPT2", k: "31" },
        { n: "stats.f.map.I_Akb_MPPT3", k: "32" }] }
    ],
    kes: [
      { t: "stats.c.v", lines: [
        { n: "stats.f.kes.VcPV", k: "1" },
        { n: "stats.f.kes.VBat", k: "5" }] },
      { t: "stats.c.i", lines: [
        { n: "stats.f.kes.IcPV", k: "2" },
        { n: "stats.f.kes.IOut", k: "11" }] },
      { t: "stats.c.p", lines: [
        { n: "stats.f.kes.PPV", k: "3" },
        { n: "stats.f.kes.POut", k: "9" }] },
      { t: "stats.c.t", lines: [
        { n: "stats.f.kes.TempBat", k: "8" },
        { n: "stats.f.kes.TempInt", k: "12" }] }
    ],
    grid: [
      { t: "grid.chart.voltages", u: "home.grid.v", lines: [
        { n: "grid.line.pv1", k: "1", u: "home.grid.v", dec: 1 },
        { n: "grid.line.pv2", k: "4", u: "home.grid.v", dec: 1 },
        { n: "grid.line.l1", k: "10", u: "home.grid.v", dec: 1 },
        { n: "grid.line.l2", k: "12", u: "home.grid.v", dec: 1 },
        { n: "grid.line.l3", k: "14", u: "home.grid.v", dec: 1 }] },
      { t: "grid.chart.currents", u: "home.grid.a", lines: [
        { n: "grid.line.pv1", k: "2", u: "home.grid.a", dec: 2 },
        { n: "grid.line.pv2", k: "5", u: "home.grid.a", dec: 2 },
        { n: "grid.line.l1", k: "11", u: "home.grid.a", dec: 2 },
        { n: "grid.line.l2", k: "13", u: "home.grid.a", dec: 2 },
        { n: "grid.line.l3", k: "15", u: "home.grid.a", dec: 2 }] },
      { t: "grid.chart.powers", u: "grid.unit.wvar", lines: [
        { n: "grid.line.pv1", k: "3", u: "home.grid.w", dec: 0 },
        { n: "grid.line.pv2", k: "6", u: "home.grid.w", dec: 0 },
        { n: "grid.line.ac", k: "7", u: "home.grid.w", dec: 0 },
        { n: "grid.line.q", k: "8", u: "home.grid.var", dec: 0 },
        { n: "grid.line.pvsum", calc: "pvsum", u: "home.grid.w", dec: 0 }] },
      { t: "grid.chart.freq_temp", u: "grid.unit.hz_c", lines: [
        { n: "grid.line.f", k: "9", u: "home.grid.hz", dec: 2 },
        { n: "grid.line.t1", k: "16", u: "home.grid.c", dec: 1 },
        { n: "grid.line.t2", k: "17", u: "home.grid.c", dec: 1 }] }
    ],
    mbms: [
      { t: "stats.c.cells_v", cellGroup: 1 },
      { t: "stats.c.spread", lines: [{ n: "stats.n.spread", calc: "spread" }] },
      { t: "stats.c.pack", lines: [{ n: "stats.n.pack", calc: "pack" }] },
      { t: "stats.c.cells_t", cellGroup: 2 },
      { t: "stats.c.cells_i", cellGroup: 0 }
    ],
    // BMS-Receiver (device_type=bms, v1; docs/bms-reader.md §1). Поля — field_id,
    // значения в контракте масштабированы → приводим делением (div).
    bms: (function () {
      var cells = [], temps = [];
      for (var i = 0; i < 16; i++) cells.push({ n: "bms.chart.cell", cell: i + 1, k: String(4 + i), div: 1000 });
      for (var j = 0; j < 8; j++) temps.push({ n: "bms.chart.temp", cell: j + 1, k: String(20 + j), div: 10 });
      return [
        { t: "bms.chart.soc", lines: [{ n: "bms.chart.soc", k: "30", div: 10 }] },
        { t: "bms.chart.pack", lines: [{ n: "bms.chart.pack", k: "36", div: 100 }] },
        { t: "bms.chart.cur", lines: [{ n: "bms.chart.cur", k: "28", div: 100 }] },
        { t: "bms.chart.pwr", lines: [{ n: "bms.chart.pwr", k: "29" }] },
        { t: "bms.cells", lines: cells },
        { t: "bms.chart.spread", lines: [{ n: "bms.chart.spread", calc: "bmsspread", div: 1000 }] },
        { t: "bms.chart.temp", lines: temps }
      ];
    })()
  };
  function cellGroupLines(group) {
    // group: 1 UakbCell, 2 TakbCell, 0 Q_Cell; id = 3*(i-1)+{1,2,3}
    var out = [];
    for (var i = 1; i <= 32; i++) { // mbms ограничен V1..V32
      var id = 3 * (i - 1) + (group === 0 ? 3 : group);
      out.push({ n: "stats.n.cell", k: String(id), cell: i });
    }
    return out;
  }

  // Поля контракта, нужные графикам: ключи линий наборов (§3.2) + поля,
  // которые читают вычисляемые ряды (calcSeries). Сервер отдаёт только их.
  var CALC_DEPS = { map: ["29", "30", "31", "32"] }; // MPPT-каналы; знак поля 3 берётся из строки сервером
  function fieldsFor(type) {
    var seen = {};
    function add(k) { if (k) seen[k] = 1; }
    (SETS[type] || []).forEach(function (card) {
      if (card.cellGroup !== undefined) cellGroupLines(card.cellGroup).forEach(function (ln) { add(ln.k); });
      else (card.lines || []).forEach(function (ln) { add(ln.k); });
    });
    (CALC_DEPS[type] || []).forEach(add);
    return Object.keys(seen);
  }

  // --- Индекс значений по времени из ответа series ---
  function buildVals(series) {
    var byKey = {}; // key -> Map(t->v)
    var times = null;
    Object.keys(series || {}).forEach(function (k) {
      var m = new Map();
      var arr = series[k] || [];
      arr.forEach(function (p) { m.set(+p[0], +p[1]); });
      byKey[k] = m;
      if (!times || arr.length > times.length) times = arr.map(function (p) { return +p[0]; });
    });
    return { byKey: byKey, times: times || [] };
  }
  function g(vals, k, t) { var m = vals.byKey[k]; return m ? m.get(t) : undefined; }
  function sumKeys(vals, t, ids) { var s = 0, any = false; for (var i = 0; i < ids.length; i++) { var v = g(vals, String(ids[i]), t); if (v != null) { s += v; any = true; } } return any ? s : undefined; }

  // Поле 3 (INET_16_4) уже знаковое: конверсия сервера применяет PNET_Sign_P.
  // Здесь знак повторно НЕ применяем.
  function pnetAt(vals, t) {
    var u = g(vals, "1", t), i = g(vals, "3", t);
    if (u == null || i == null) return undefined;
    return u * i;
  }
  function pbatAt(vals, t) {
    var ua = g(vals, "8", t), ia = g(vals, "10", t);
    return (ua != null && ia != null) ? ua * ia : undefined;
  }
  function poutAt(vals, t) {
    var pn = pnetAt(vals, t), pb = pbatAt(vals, t);
    return (pn != null && pb != null) ? (pn + pb) : undefined;
  }
  function calcSeries(calc, vals) {
    var out = [];
    vals.times.forEach(function (t) {
      var v;
      if (calc === "imppt") v = sumKeys(vals, t, [29, 30, 31, 32]);
      else if (calc === "pnet") v = pnetAt(vals, t);
      else if (calc === "pbat") v = pbatAt(vals, t);
      else if (calc === "pout") v = poutAt(vals, t);
      else if (calc === "iout") { var uo = g(vals, "6", t), po = poutAt(vals, t); v = (po != null && uo) ? po / uo : undefined; }
      else if (calc === "pvsum") v = sumKeys(vals, t, [3, 6]);
      else if (calc === "pmppt") { var ua2 = g(vals, "8", t), im = sumKeys(vals, t, [29, 30, 31, 32]); v = (ua2 != null && im != null) ? ua2 * im : undefined; }
      else if (calc === "spread" || calc === "pack") {
        var vals2 = [], m = vals.byKey;
        Object.keys(m).forEach(function (k) { var id = +k; if (id % 3 === 1) { var x = m[k].get(t); if (x != null) vals2.push(x); } });
        if (!vals2.length) return;
        if (calc === "spread") { v = Math.max.apply(null, vals2) - Math.min.apply(null, vals2); }
        else { v = vals2.reduce(function (a, b) { return a + b; }, 0); }
      }
      else if (calc === "bmsspread") {
        // BMS: разброс по ячейкам (field_id 4..19, в мВ; масштаб — div у линии).
        var cv = [], bm = vals.byKey;
        for (var id = 4; id <= 19; id++) { var m2 = bm[String(id)]; if (m2) { var x2 = m2.get(t); if (x2 != null) cv.push(x2); } }
        if (!cv.length) return;
        v = Math.max.apply(null, cv) - Math.min.apply(null, cv);
      }
      if (v != null && isFinite(v)) out.push([t, v]);
    });
    return out;
  }

  // --- Периоды ---
  var range = { from: 0, to: 0 };
  function preset(p) {
    var to = Date.now(), d = new Date();
    if (p === "24h") return { from: to - 24 * 3600e3, to: to };
    if (p === "today") { d.setHours(0, 0, 0, 0); return { from: d.getTime(), to: to }; }
    if (p === "yesterday") { d.setHours(0, 0, 0, 0); return { from: d.getTime() - 864e5, to: d.getTime() - 1 }; }
    if (p === "7d") return { from: to - 7 * 864e5, to: to };
    if (p === "month") { d.setDate(d.getDate() - 30); return { from: d.getTime(), to: to }; }
    if (p === "last") return { from: 0, to: to };
    return { from: to - 24 * 3600e3, to: to };
  }
  // Значение для <input type="datetime-local" step="1"> (локальная зона, секунды).
  function toLocalInput(ms) { var d = new Date(ms), p = function (x) { return (x < 10 ? "0" : "") + x; }; return d.getFullYear() + "-" + p(d.getMonth() + 1) + "-" + p(d.getDate()) + "T" + p(d.getHours()) + ":" + p(d.getMinutes()) + ":" + p(d.getSeconds()); }

  // --- Режим «Следовать за временем» ---
  // Глубину окна задаёт движок (1..10 мин) — персистентно. Первично/при смене глубины
  // тянем весь диапазон (follow=1&depth), далее раз в секунду — только новые точки
  // (follow=1&after=<время правой крайней точки>); клиент дописывает справа, удаляет
  // устаревшее слева и выравнивает окно. Не персистентен (кроме глубины); выключается
  // при входе, смене вкладки и ручных действиях.
  var followTimer = null, followActive = false, followSeries = null, followFrom = 0, followTo = 0, followDepth = 5;
  function depthKey() { return "statsFollowDepth"; }
  function loadDepth() { var v = parseInt(localStorage.getItem(depthKey()) || "5", 10); return (v >= 1 && v <= 10) ? v : 5; }
  function saveDepth(v) { try { localStorage.setItem(depthKey(), String(v)); } catch (e) {} }
  function depthUI() {
    var r = document.getElementById("ch-depth"); if (r) r.value = String(followDepth);
    var v = document.getElementById("ch-depth-val"); if (v) v.textContent = String(followDepth);
    var w = document.getElementById("ch-depth-wrap"); if (w) w.hidden = !followActive;
    var g = document.getElementById("ch-follow-group"); if (g) g.classList.toggle("active", followActive);
  }
  // В follow ручные контролы периода/дат/показа/обновить неактивны.
  function followControls(dis) {
    Array.prototype.forEach.call(document.querySelectorAll(".stats-controls [data-period], #ch-from, #ch-to, #ch-show, #ch-reload"),
      function (el) { el.disabled = dis; });
    var cc = document.getElementById("ch-compare"); if (cc) cc.disabled = dis;
  }
  function inputsShow() {
    document.getElementById("ch-from").value = toLocalInput(range.from);
    document.getElementById("ch-to").value = toLocalInput(range.to);
  }
  function stopFollow() {
    if (followTimer) { clearInterval(followTimer); followTimer = null; }
    followActive = false; followSeq++; followControls(false); depthUI();
  }
  function setFollowOff() { var cb = document.getElementById("ch-follow"); if (cb) cb.checked = false; stopFollow(); }
  function followURL(extra) {
    return "/api/stats/series?type=" + encodeURIComponent(active.type) + "&id=" + encodeURIComponent(active.id) +
      "&fields=" + encodeURIComponent(fieldsFor(active.type).join(",")) + "&follow=1" + extra;
  }
  function followFull() {
    var act = active, seq = ++followSeq;
    return api(followURL("&depth=" + followDepth)).then(function (j) {
      if (!followActive || active !== act || seq !== followSeq || !j) return;
      followSeries = (j && j.series) || {}; followFrom = j.from; followTo = j.to;
      range = { from: followFrom, to: followTo };
      renderFromSeries(followSeries); inputsShow();
    });
  }
  function followTick() {
    if (!followSeries) return Promise.resolve();
    var act = active, seq = followSeq;
    var lastT = 0;
    Object.keys(followSeries || {}).forEach(function (k) { var a = followSeries[k]; if (a && a.length && a[a.length - 1][0] > lastT) lastT = a[a.length - 1][0]; });
    return api(followURL("&after=" + lastT)).then(function (j) {
      if (!followActive || active !== act || seq !== followSeq || !j) return;
      var ns = (j && j.series) || {};
      Object.keys(ns).forEach(function (k) { var dst = followSeries[k] || (followSeries[k] = []); ns[k].forEach(function (p) { dst.push(p); }); });
      followTo = j.to; followFrom = followTo - followDepth * 60000;
      Object.keys(followSeries).forEach(function (k) { var a = followSeries[k]; while (a.length && a[0][0] < followFrom) a.shift(); });
      range = { from: followFrom, to: followTo };
      renderFromSeries(followSeries); inputsShow();
    });
  }
  function startFollow() {
    stopFollow();
    followActive = true; followDepth = loadDepth();
    followSeries = null; followFrom = followTo = 0;
    depthUI(); followControls(true);
    followFull().catch(function () {});
    followTimer = setInterval(function () { followTick().catch(function () {}); }, 1000);
  }

  // --- Устройства/закладки ---
  var devs = [], active = null;
  // «Показать неактуальные устройства»: по умолчанию в закладках только устройства
  // с обновлением телеметрии за последний час. Флаг — в памяти модуля (не персистентен
  // при перезагрузке страницы, сохраняется при переходах между закладками).
  var showStale = false;
  var STALE_MS = 3600 * 1000;
  var staleCb = document.getElementById("stats-show-stale");
  function devLabel(d) {
    var name = d.type === "map" ? "МАП" : d.type === "kes" ? "КЭС" : d.type === "grid" ? T("grid.device") : "BMS";
    return name + " " + d.id;
  }
  function visibleDevs() {
    var now = Date.now();
    return devs.filter(function (d) {
      return showStale || (d.last && (now - d.last) < STALE_MS);
    });
  }
  function renderTabs() {
    tabsEl.innerHTML = "";
    visibleDevs().forEach(function (d, i) {
      var b = document.createElement("button");
      b.className = "stats-tab" + (active === d ? " active" : "");
      b.textContent = devLabel(d);
      b.addEventListener("click", function () { setFollowOff(); active = d; if (compareCb) { compareCb.checked = false; } renderTabs(); loadCharts(); });
      tabsEl.appendChild(b);
    });
    if (compareWrap) {
      var isGrid = !!(active && active.type === "grid");
      compareWrap.hidden = !isGrid;
      if (!isGrid && compareCb) { compareCb.checked = false; }
    }
  }
  function loadDevs() {
    // Фиксированный режим (страница устройства, напр. /bms): без списка/закладок —
    // графики только для заданного типа+id (наш контракт).
    if (window.STATS_FIXED && window.STATS_FIXED.type) {
      if (tabsEl) { tabsEl.innerHTML = ""; tabsEl.hidden = true; }
      active = { type: window.STATS_FIXED.type, id: window.STATS_FIXED.id };
      loadCharts();
      return Promise.resolve();
    }
    return api("/api/stats/devices").then(function (j) {
      devs = (j && j.devices) || [];
      var vis = visibleDevs();
      if (vis.length) { active = vis[0]; renderTabs(); loadCharts(); }
      else showNoData();
    }).catch(function () {
      showNoData();
    });
  }

  // --- Рендер карточек ---
  var CHARTS = {};
  var chartsType = null;
  function clearCharts() {
    Object.keys(CHARTS).forEach(function (id) { try { CHARTS[id].destroy(); } catch (e) {} delete CHARTS[id]; });
    chartsType = null;
    compareMode = false;
  }
  function showNoData() {
    clearCharts();
    chartsEl.innerHTML = '<p class="muted">' + esc(T("stats.no_data")) + "</p>";
  } // тип устройства, под который построены карточки (in-place только для него)
  var hidden = {};   // type -> [keyStr] (скрытые линии; кэш из localStorage)
  var zoomWin = {};  // id -> {min,max}
  // Персист видимости линий: в разрезе ТИПА устройства, по стабильным ключам
  // (не по локализованным именам). Новые линии не из настройки — включены;
  // отсутствующие на графике записи настройки не удаляются.
  function hiddenKey(type) { return "statsHidden:" + type; }
  function loadHidden(type) {
    var arr = null;
    try { arr = JSON.parse(localStorage.getItem(hiddenKey(type)) || "null"); } catch (e) {}
    return Array.isArray(arr) ? arr : [];
  }
  function ensureHidden(type) { if (!hidden[type]) hidden[type] = loadHidden(type); return hidden[type]; }
  function persistHidden(type, arr) {
    hidden[type] = arr;
    try { localStorage.setItem(hiddenKey(type), JSON.stringify(arr)); } catch (e) {}
  }
  var orderKey = function () { return "statsChartOrder:" + (compareMode ? "grid-compare" : (active ? active.type : "")); };

  function applyOrder() {
    var ids = null;
    try { ids = JSON.parse(localStorage.getItem(orderKey()) || "null"); } catch (e) {}
    if (!Array.isArray(ids) || !ids.length) return;
    var byId = {};
    Array.prototype.forEach.call(chartsEl.querySelectorAll(".stats-card"), function (c) { byId[c.getAttribute("data-cid")] = c; });
    ids.forEach(function (id) { if (byId[id]) chartsEl.appendChild(byId[id]); });
  }
  function saveOrder() {
    var ids = [];
    Array.prototype.forEach.call(chartsEl.querySelectorAll(".stats-card"), function (c) { ids.push(c.getAttribute("data-cid")); });
    try { localStorage.setItem(orderKey(), JSON.stringify(ids)); } catch (e) {}
  }
  // Число карточек в первой строке (колонок сетки) — раскладка общая для всех
  // страниц графиков; считаем по совпадению верхней границы, чтобы up/down/left/right
  // работали и при 1 колонке, и при 2+ (адаптив).
  function colsCount(cards) {
    if (!cards.length) return 1;
    var top = cards[0].getBoundingClientRect().top, n = 0;
    for (var i = 0; i < cards.length; i++) {
      if (Math.abs(cards[i].getBoundingClientRect().top - top) < 2) n++; else break;
    }
    return Math.max(1, n);
  }
  function moveCard(btn, dir) {
    var card = btn.closest(".stats-card");
    var cards = Array.prototype.slice.call(chartsEl.querySelectorAll(".stats-card"));
    var idx = cards.indexOf(card), cols = colsCount(cards), target = -1;
    if (idx < 0) return;
    if (dir === "up") target = idx - cols;
    else if (dir === "down") target = idx + cols < cards.length ? idx + cols : -1;
    else if (dir === "left") target = idx % cols !== 0 ? idx - 1 : -1;
    else if (dir === "right") target = idx % cols !== cols - 1 && idx + 1 < cards.length ? idx + 1 : -1;
    if (target < 0) return;
    var ref = cards[target];
    if (target > idx) chartsEl.insertBefore(card, ref.nextSibling); else chartsEl.insertBefore(card, ref);
    saveOrder();
  }
  function orderBtns() {
    return '<div class="card-order">' +
      '<button class="ord-up" type="button" title="↑">↑</button>' +
      '<button class="ord-left" type="button" title="←">←</button>' +
      '<button class="ord-down" type="button" title="↓">↓</button>' +
      '<button class="ord-right" type="button" title="→">→</button></div>';
  }

  // Синхронные курсор/хинты по общему времени. Само рисование — в syncPlugin.afterDraw
  // (встроенный tooltip отключён), здесь только состояние и перерисовка всех карточек.
  var cursor = { active: false, value: null };
  var syncing = false;
  function syncTooltips(t) {
    if (t == null) { cursor.active = false; } else { cursor.active = true; cursor.value = t; }
    Object.keys(CHARTS).forEach(function (id) { try { CHARTS[id].update("none"); } catch (e) {} });
  }
  // Формат значения и времени для кастомного хинта.
  function fmtVal(v, dec) {
    if (v == null || !isFinite(v)) return "—";
    if (dec !== undefined && dec !== null) return v.toFixed(dec);
    var a = Math.abs(v);
    return v.toFixed(a >= 100 ? 0 : a >= 10 ? 1 : 2);
  }
  function fmtTime(t) {
    var d = new Date(t), p = function (x) { return (x < 10 ? "0" : "") + x; };
    return p(d.getDate()) + "." + p(d.getMonth() + 1) + "." + d.getFullYear() + " " + p(d.getHours()) + ":" + p(d.getMinutes()) + ":" + p(d.getSeconds());
  }
  var syncPlugin = {
    id: "statsSync",
    afterDraw: function (c) {
      try {
        if (cursor.active && isFinite(cursor.value)) {
          var x = c.scales.x.getPixelForValue(cursor.value);
          if (x >= c.chartArea.left && x <= c.chartArea.right) {
            var ctx = c.ctx;
            ctx.save();
            ctx.beginPath(); ctx.moveTo(x, c.scales.y.top); ctx.lineTo(x, c.scales.y.bottom); ctx.strokeStyle = "rgba(15,18,22,.75)"; ctx.lineWidth = 1; ctx.stroke();
            // Хинт: значения видимых линий на ближайшем к общему времени индексе.
            var ds = c.data.datasets || [];
            if (ds.length && ds[0].data && ds[0].data.length) {
              var arr = ds[0].data, lo = 0, hi = arr.length - 1;
              while (lo < hi) { var mid = (lo + hi) >> 1; if (arr[mid][0] < cursor.value) lo = mid + 1; else hi = mid; }
              var rows = [];
              ds.forEach(function (d, i) { if (c.isDatasetVisible(i) && d.data[lo]) rows.push({ col: d.borderColor, s: d.label + ": " + fmtVal(d.data[lo][1], d.dec) }); });
              var title = fmtTime(arr[lo][0]), pad = 5, lh = 13;
              var fontNormal = "11px system-ui, sans-serif", fontBold = "bold 11px system-ui, sans-serif";
              ctx.textBaseline = "top";
              ctx.font = fontNormal;
              var w = ctx.measureText(title).width;
              ctx.font = fontBold; // значения — жирным
              rows.forEach(function (r) { w = Math.max(w, ctx.measureText(r.s).width); });
              w += pad * 2;
              var h = (rows.length + 1) * lh + pad * 2, bx = x + 10, by = c.chartArea.top + 4;
              if (bx + w > c.chartArea.right) bx = x - 10 - w;
              ctx.fillStyle = "#ffffff"; ctx.fillRect(bx, by, w, h);
              ctx.strokeStyle = "rgba(0,0,0,.25)"; ctx.lineWidth = 1; ctx.strokeRect(bx + .5, by + .5, w - 1, h - 1);
              ctx.font = fontNormal; ctx.fillStyle = "#222222"; ctx.fillText(title, bx + pad, by + pad);
              ctx.font = fontBold;
              rows.forEach(function (r, k) { ctx.fillStyle = r.col || "#222222"; ctx.fillText(r.s, bx + pad, by + pad + (k + 1) * lh); });
            }
            ctx.restore();
          }
        }
        if (!syncing) {
          var sx = c.scales.x; if (sx && isFinite(sx.min) && isFinite(sx.max) && sx.max > sx.min) {
            var key = sx.min.toFixed(0) + "," + sx.max.toFixed(0);
            if (zoomWin[c.canvas.id] !== undefined && zoomWin[c.canvas.id] !== key) {
              syncing = true; var mn = sx.min, mx = sx.max;
              Object.keys(CHARTS).forEach(function (oid) { if (oid !== c.canvas.id && CHARTS[oid]) { try { CHARTS[oid].zoomScale("x", { min: mn, max: mx }, "none"); } catch (e) {} } });
              syncing = false;
            }
            zoomWin[c.canvas.id] = key;
          }
        }
      } catch (e) {}
    }
  };
  function chipsBuild(canvasId, chipsId, chart) {
    var row = document.getElementById(chipsId);
    // sync — только вид чипов (без записи настройки). Настройка обновляется
    // в saveState() — только при явном действии пользователя.
    function sync() {
      var cs = row.querySelectorAll(".lg-chip:not(.lg-all)");
      chart.data.datasets.forEach(function (d, i) { if (cs[i]) cs[i].classList.toggle("off", !chart.isDatasetVisible(i)); });
    }
    // Сохраняет текущее состояние в настройку типа: видимые линии убираются из
    // hidden, скрытые — добавляются; отсутствующие на графике записи НЕ удаляются.
    function saveState() {
      var type = active ? active.type : "";
      var cur = {};
      ensureHidden(type).forEach(function (k) { cur[k] = 1; });
      chart.data.datasets.forEach(function (d, i) {
        if (!d.keyStr) return;
        if (chart.isDatasetVisible(i)) delete cur[d.keyStr]; else cur[d.keyStr] = 1;
      });
      persistHidden(type, Object.keys(cur));
    }
    row.innerHTML = "";
    chart.data.datasets.forEach(function (d, i) {
      var b = document.createElement("button"); b.type = "button"; b.className = "lg-chip";
      var sw = document.createElement("i"); sw.style.background = d.borderColor; b.appendChild(sw); b.appendChild(document.createTextNode(d.label));
      var t = null;
      b.addEventListener("click", function () {
        if (t) { // двойной клик — только эта линия
          clearTimeout(t); t = null;
          chart.data.datasets.forEach(function (x, j) { chart.setDatasetVisibility(j, j === i); });
          chart.update("none"); sync(); saveState();
        } else { // одиночный клик — вкл/выкл (с задержкой на распознавание двойного)
          t = setTimeout(function () {
            t = null;
            chart.setDatasetVisibility(i, !chart.isDatasetVisible(i));
            chart.update("none"); sync(); saveState();
          }, 250);
        }
      });
      row.appendChild(b);
    });
    var all = document.createElement("button"); all.type = "button"; all.className = "lg-chip lg-all"; all.textContent = T("stats.all");
    all.addEventListener("click", function () { chart.data.datasets.forEach(function (x, j) { chart.setDatasetVisibility(j, true); }); chart.update("none"); sync(); saveState(); });
    row.appendChild(all);
    sync();
  }

  // Перезапрос под окно при zoom/панораме (§3.5): debounce, окно берётся из осей,
  // bucket на бэкенде пересчитывается от нового окна (детализация растёт при зуме).
  var reloadTimers = {};
  function scheduleReload(cid) {
    setFollowOff(); // zoom/панорама сбрасывают режим «следовать за временем»
    clearTimeout(reloadTimers[cid]);
    reloadTimers[cid] = setTimeout(function () { reloadTimers[cid] = null; reloadFromWindow(); }, 300);
  }
  function reloadFromWindow() {
    // Инвариант: окно по X общее для всех карточек (zoom-синк в syncPlugin),
    // поэтому достаточно окна первого графика.
    var c = null;
    Object.keys(CHARTS).forEach(function (id) { if (!c && CHARTS[id]) c = CHARTS[id]; });
    if (!c || !c.scales || !c.scales.x) return;
    var x = c.scales.x;
    if (!isFinite(x.min) || !isFinite(x.max)) return;
    range.from = Math.round(x.min);
    range.to = Math.round(x.max);
    loadCharts();
  }

  // Отражает фактическое окно осей X в полях «С/по» без перезапроса (сброс зума).
  function syncInputsFromWindow() {
    var c = null;
    Object.keys(CHARTS).forEach(function (id) { if (!c && CHARTS[id]) c = CHARTS[id]; });
    if (!c || !c.scales || !c.scales.x) return;
    var x = c.scales.x;
    if (!isFinite(x.min) || !isFinite(x.max)) return;
    range.from = Math.round(x.min);
    range.to = Math.round(x.max);
    document.getElementById("ch-from").value = toLocalInput(range.from);
    document.getElementById("ch-to").value = toLocalInput(range.to);
  }

  function renderCard(set, cid, datasets) {
    var card = document.createElement("div");
    card.className = "stats-card";
    card.setAttribute("data-cid", cid);
    card.innerHTML = orderBtns() + "<h2>" + esc(T(set.t)) + (set.u ? " (" + esc(T(set.u)) + ")" : "") + '</h2><div class="chart-toolbar"><span></span></div>' +
      '<div class="lg-chips" id="' + cid + 'Lg"></div><div class="chart-wrap"><canvas id="' + cid + '"></canvas></div>';
    chartsEl.appendChild(card);
    var saved = zoomWin[cid];
    var opts = {
      responsive: true, maintainAspectRatio: false, animation: false,
      interaction: { mode: "index", intersect: false },
      plugins: {
        legend: { display: false },
        // Встроенный tooltip отключён: иначе Chart.js показывает его на текущем
        // графике мгновенно по mousemove, мимо нашего таймера. Хинт рисуем сами
        // (см. syncPlugin.afterDraw) — единообразно и с нужной задержкой.
        tooltip: { enabled: false },
        zoom: {
          pan: { enabled: true, mode: "x", onPanComplete: function () { scheduleReload(cid); } },
          zoom: {
            wheel: { enabled: true, speed: .1, modifierKey: "ctrl" },
            drag: { enabled: true, modifierKey: "ctrl", backgroundColor: "rgba(51,122,183,.15)", borderColor: "rgba(51,122,183,.6)", borderWidth: 1 },
            pinch: { enabled: true },
            mode: "x",
            onZoomComplete: function () { scheduleReload(cid); }
          },
          limits: { x: { minRange: 5 * 60e3 } }
        }
      },
      scales: {
        x: {
          type: "time",
          time: {
            tooltipFormat: "dd.MM.yyyy HH:mm:ss",
            displayFormats: { // 24-часовой формат (без AM/PM)
              millisecond: "HH:mm:ss.SSS", second: "HH:mm:ss", minute: "HH:mm", hour: "HH:mm",
              day: "dd.MM", week: "dd.MM", month: "MM.yyyy", quarter: "MM.yyyy", year: "yyyy"
            }
          },
          ticks: { maxRotation: 0 }
        },
        y: {
          beginAtZero: false,
          grid: {
            // Нулевая линия — контрастнее остальных линий сетки.
            color: function (c) { return (c.tick && c.tick.value === 0) ? "rgba(51,74,102,.55)" : "rgba(0,0,0,.08)"; },
            lineWidth: function (c) { return (c.tick && c.tick.value === 0) ? 1.5 : 1; }
          }
        }
      }
    };
    if (saved) { opts.scales.x.min = saved.min; opts.scales.x.max = saved.max; }
    var chart = new Chart(document.getElementById(cid), { type: "line", data: { datasets: datasets }, plugins: [syncPlugin], options: opts });
    CHARTS[cid] = chart;
    var hid = hidden[active.type] || [];
    chart.data.datasets.forEach(function (d, i) { if (d.keyStr && hid.indexOf(d.keyStr) >= 0) chart.setDatasetVisibility(i, false); });
    chipsBuild(cid, cid + "Lg", chart);
    // Тач-хинт: показываем только по НЕПОДВИЖНОМУ тапу > 0.1 с; при любом движении
    // гасим и больше не показываем до следующего тапа (движение = панорама/зум).
    var cv = document.getElementById(cid);
    var tapTimer = null;
    function hideHints() {
      clearTimeout(tapTimer); tapTimer = null;
      if (cursor.active) { cursor.active = false; syncTooltips(null); Object.keys(CHARTS).forEach(function (id) { try { CHARTS[id].update("none"); } catch (x) {} }); }
    }
    function showHintAt(px) {
      var xs = chart.scales.x; if (!xs) return;
      var v = xs.getValueForPixel(px);
      if (!isFinite(v)) return;
      cursor.active = true; cursor.value = v; syncTooltips(v);
      Object.keys(CHARTS).forEach(function (id) { try { CHARTS[id].update("none"); } catch (x) {} });
    }
    cv.addEventListener("touchstart", function (ev) {
      if (!ev.touches || ev.touches.length !== 1) { clearTimeout(tapTimer); tapTimer = null; return; } // пинч — не хинт
      var rect = cv.getBoundingClientRect(), px = ev.touches[0].clientX - rect.left;
      clearTimeout(tapTimer);
      tapTimer = setTimeout(function () { tapTimer = null; showHintAt(px); }, 100);
    }, { passive: true });
    cv.addEventListener("touchmove", function () { hideHints(); }, { passive: true });
    cv.addEventListener("touchend", function () { clearTimeout(tapTimer); tapTimer = null; }, { passive: true });

    // Десктоп-хинт: показываем только если мышь неподвижна > 0.3 с; при любом
    // движении гасим и не показываем до следующей остановки.
    var hoverTimer = null;
    cv.addEventListener("mousemove", function (ev) {
      var rect = cv.getBoundingClientRect(), px = ev.clientX - rect.left;
      clearTimeout(hoverTimer);
      hideHints(); // во время движения хинт скрыт
      hoverTimer = setTimeout(function () { hoverTimer = null; showHintAt(px); }, 300);
    });
    cv.addEventListener("mouseleave", function () { clearTimeout(hoverTimer); hoverTimer = null; hideHints(); });

    // Double-tap reset (touch).
    var lastTap = 0;
    cv.addEventListener("touchend", function () { var now = Date.now(); if (now - lastTap < 320) { try { chart.resetZoom(); } catch (e) {} zoomWin[cid] = undefined; setTimeout(syncInputsFromWindow, 0); } lastTap = now; }, { passive: true });
  }

  // Сборка datasets набора из ответа (ключ линии — стабильный, §3.5).
  function buildDatasets(set, vals, series) {
    var lines = set.cellGroup !== undefined ? cellGroupLines(set.cellGroup) : set.lines;
    var datasets = [];
    lines.forEach(function (ln, li) {
      var data = ln.calc ? calcSeries(ln.calc, vals) : ((series && series[ln.k]) || []).map(function (p) { return [+p[0], +p[1]]; });
      if (ln.div && ln.div !== 1) { data = data.map(function (p) { return [p[0], p[1] / ln.div]; }); }
      if (!data.length) return;
      var name = T(ln.n) + (ln.cell ? " " + ln.cell : "") + (ln.u ? " (" + T(ln.u) + ")" : "");
      var keyStr = ln.n + (ln.cell ? ":" + ln.cell : ""); // стабильный ключ для персиста
      var pr = data.length < 3 ? 3 : 0; // редкие данные — точки видимы
      datasets.push({ label: name, keyStr: keyStr, data: data, unit: ln.u ? T(ln.u) : "", dec: ln.dec, borderColor: COLORS[li % COLORS.length], backgroundColor: COLORS[li % COLORS.length], pointRadius: pr, pointHoverRadius: 3, pointBorderColor: COLORS[li % COLORS.length], borderWidth: 1.5, tension: .35, cubicInterpolationMode: "monotone", fill: false });
    });
    return datasets;
  }

  // Мягкое обновление графика НА МЕСТЕ (без destroy/пересоздания): данные и ось X
  // обновляются, объекты линий сохраняются → плавный сдвиг окна (follow/zoom-reload),
  // без «дёргания» и с сохранением видимости линий.
  function applyInPlace(chart, datasets) {
    var existing = {};
    (chart.data.datasets || []).forEach(function (d) { if (d.keyStr) existing[d.keyStr] = d; });
    var hid = hidden[active.type] || [];
    chart.data.datasets = datasets.map(function (nd) {
      var ex = existing[nd.keyStr];
      if (!ex) return nd; // новая линия
      ex.label = nd.label; ex.data = nd.data;
      ex.borderColor = nd.borderColor; ex.backgroundColor = nd.backgroundColor;
      ex.pointRadius = nd.pointRadius; ex.pointBorderColor = nd.pointBorderColor;
      return ex;
    });
    chart.options.scales.x.min = range.from;
    chart.options.scales.x.max = range.to;
    chart.data.datasets.forEach(function (d, i) { chart.setDatasetVisibility(i, hid.indexOf(d.keyStr) < 0); });
    chart.update("none");
  }

  // Общий рендер наборов из объекта series: мягкое обновление на месте, если
  // карточки уже построены под тот же тип/состав, иначе — полный рендер.
  function renderFromSeries(ser) {
    var vals = buildVals(ser || {});
    var sets = SETS[active.type] || [];
    ensureHidden(active.type);
    var canInPlace = chartsType === active.type && sets.length > 0 &&
      Object.keys(CHARTS).length === sets.length &&
      sets.every(function (_, si) { return CHARTS["ch" + si]; });
    if (canInPlace) {
      sets.forEach(function (set, si) { applyInPlace(CHARTS["ch" + si], buildDatasets(set, vals, ser)); });
      applyOrder();
      return;
    }
    Object.keys(CHARTS).forEach(function (id) { try { CHARTS[id].destroy(); } catch (e) {} delete CHARTS[id]; });
    chartsEl.innerHTML = "";
    sets.forEach(function (set, si) { renderCard(set, "ch" + si, buildDatasets(set, vals, ser)); });
    chartsType = active.type;
    applyOrder();
  }

  function cmpDataset(label, data, idx, unit, dec) {
    var nm = label + (unit ? " (" + T(unit) + ")" : "");
    return { label: nm, keyStr: label, data: data, unit: T(unit || ""), dec: dec, borderColor: COLORS[idx % COLORS.length],
      backgroundColor: COLORS[idx % COLORS.length], pointRadius: 0, pointHoverRadius: 3,
      pointBorderColor: COLORS[idx % COLORS.length], borderWidth: 1.5, tension: .35,
      cubicInterpolationMode: "monotone", fill: false };
  }

  function renderGridCompare() {
    if (!range.to) { range = preset("24h"); }
    if (active.type === "grid" && !range.from) { range = { from: range.to - 3600e3, to: range.to }; }
    var act = active;
    var seq = ++reqSeq;
    var gd = devs.filter(function (d) { return d.type === "grid"; });
    if (!gd.length) { showNoData(); return; }
    var reqs = gd.map(function (d) {
      var url = "/api/stats/series?type=grid&id=" + encodeURIComponent(d.id) +
        "&fields=7,8&from=" + range.from + "&to=" + range.to;
      return api(url).then(function (j) { return { d: d, j: j }; }).catch(function () { return { d: d, j: null }; });
    });
    Promise.all([api("/api/grid/status").catch(function () { return null; })].concat(reqs)).then(function (all) {
      if (seq !== reqSeq || active !== act || !(compareCb && compareCb.checked)) return;
      var st = all[0], results = all.slice(1);
      var place = {};
      ((st && st.inverters) || []).forEach(function (x) { place[x.serial] = x.placement || ""; });
      var activeDS = [], reactiveDS = [], perDev = [], plSet = {};
      results.forEach(function (r, di) {
        var ser = (r.j && r.j.series) || {};
        var a = (ser["7"] || []).map(function (p) { return [+p[0], +p[1]]; });
        var q = (ser["8"] || []).map(function (p) { return [+p[0], +p[1]]; });
        if (a.length) activeDS.push(cmpDataset(r.d.id, a, di, "home.grid.w", 0));
        if (q.length) reactiveDS.push(cmpDataset(r.d.id, q, di, "home.grid.var", 0));
        var pn = place[r.d.id] || "";
        plSet[pn] = true;
        perDev.push({ pn: pn, pts: a });
      });
      function nearest(pts, t, tol) {
        if (!pts.length) return null;
        var lo = 0, hi = pts.length - 1;
        while (lo < hi) {
          var mid = (lo + hi) >> 1;
          if (pts[mid][0] < t) lo = mid + 1; else hi = mid;
        }
        var best = null, bd = Infinity;
        [lo - 1, lo].forEach(function (i) {
          if (i >= 0 && i < pts.length) {
            var d = Math.abs(pts[i][0] - t);
            if (d < bd) { bd = d; best = pts[i][1]; }
          }
        });
        return bd <= tol ? best : null;
      }
      var times = [];
      perDev.forEach(function (d) { d.pts.forEach(function (p) { times.push(p[0]); }); });
      times.sort(function (a, b) { return a - b; });
      var totalData = [], plData = {};
      Object.keys(plSet).forEach(function (pn) { plData[pn] = []; });
      if (times.length) {
        var step = 30000, tol = 120000;
        var startT = times[0], endT = times[times.length - 1];
        for (var t = startT; t <= endT; t += step) {
          var all = perDev.map(function (d) { return nearest(d.pts, t, tol); });
          if (all.every(function (v) { return v !== null; })) {
            var sum = 0;
            all.forEach(function (v) { sum += v; });
            totalData.push([t, sum]);
          }
          Object.keys(plData).forEach(function (pn) {
            var vals = perDev.filter(function (d) { return d.pn === pn; }).map(function (d) { return nearest(d.pts, t, tol); });
            if (vals.length && vals.every(function (v) { return v !== null; })) {
              var s2 = 0;
              vals.forEach(function (v) { s2 += v; });
              plData[pn].push([t, s2]);
            }
          });
        }
      }
      var totalDS = [];
      if (totalData.length) totalDS.push(cmpDataset(T("grid.line.total"), totalData, 0, "home.grid.w", 0));
      Object.keys(plData).sort().forEach(function (pn, i) {
        if (plData[pn].length) totalDS.push(cmpDataset(pn || T("grid.no_placement"), plData[pn], i + 1, "home.grid.w", 0));
      });
      if (!activeDS.length && !reactiveDS.length && !totalDS.length) {
        showNoData();
        return;
      }
      Object.keys(CHARTS).forEach(function (id) { try { CHARTS[id].destroy(); } catch (e) {} delete CHARTS[id]; });
      chartsEl.innerHTML = "";
      compareMode = true;
      renderCard({ t: "stats.cmp.active", u: "home.grid.w" }, "chcmp0", activeDS);
      renderCard({ t: "stats.cmp.reactive", u: "home.grid.var" }, "chcmp1", reactiveDS);
      renderCard({ t: "stats.cmp.total", u: "home.grid.w" }, "chcmp2", totalDS);
      chartsType = "grid-compare";
      applyOrder();
      inputsShow();
    });
  }

  function loadCharts() {
    if (!active) return;
    if (compareCb && compareCb.checked && active.type === "grid") { renderGridCompare(); return; }
    compareMode = false;
    if (!range.to) { range = preset("24h"); }
    if (active.type === "grid" && !range.from) { range = { from: range.to - 3600e3, to: range.to }; }
    var act = active;
    var seq = ++reqSeq;
    var url = "/api/stats/series?type=" + encodeURIComponent(act.type) + "&id=" + encodeURIComponent(act.id) +
      "&fields=" + encodeURIComponent(fieldsFor(act.type).join(",")) +
      "&from=" + range.from + "&to=" + range.to;
    api(url).then(function (j) {
      if (seq !== reqSeq || active !== act) return;
      if (!j) { showNoData(); return; }
      var ser = (j && j.series) || {};
      var any = Object.keys(ser).some(function (k) { return ser[k] && ser[k].length; });
      if (!any) { showNoData(); return; }
      // Выравнивание окна по данным: правый край = конец данных (по всем полям),
      // левый — не левее начала данных (без пустот с обеих сторон).
      var gmax = 0, gmin = 0;
      Object.keys(ser).forEach(function (k) {
        var a = ser[k]; if (!a || !a.length) return;
        if (gmax === 0 || a[a.length - 1][0] > gmax) gmax = a[a.length - 1][0];
        if (gmin === 0 || a[0][0] < gmin) gmin = a[0][0];
      });
      if (gmax > 0) {
        var w = range.to - range.from;
        if (w > 0) {
          range.to = gmax;
          range.from = gmin > 0 ? Math.max(gmin, gmax - w) : gmax - w;
        }
      }
      inputsShow();
      renderFromSeries(ser);
    }).catch(function () {});
  }

  // --- События UI ---
  Array.prototype.forEach.call(document.querySelectorAll(".stats-controls [data-period]"), function (b) {
    b.addEventListener("click", function () { setFollowOff(); range = preset(b.getAttribute("data-period")); zoomWin = {}; loadCharts(); });
  });
  document.getElementById("ch-show").addEventListener("click", function () {
    var f = document.getElementById("ch-from").value, t = document.getElementById("ch-to").value;
    var fn = f ? new Date(f).getTime() : NaN, tn = t ? new Date(t).getTime() : NaN;
    if (!isFinite(fn) || !isFinite(tn) || fn > tn) return; setFollowOff(); range = { from: fn, to: tn }; zoomWin = {}; loadCharts();
  });
  document.getElementById("ch-reload").addEventListener("click", function () { setFollowOff(); loadCharts(); });

  // Перемещение карточек графиков стрелками (↑←↓→) — делегирование на контейнер,
  // работает для всех страниц графиков; порядок сохраняется per-тип (saveOrder).
  chartsEl.addEventListener("click", function (ev) {
    var t = ev.target, btn = t && t.closest ? t.closest(".card-order button") : null;
    if (!btn) return;
    var m = /(?:^|\s)ord-(up|down|left|right)(?:\s|$)/.exec(btn.className);
    if (m) moveCard(btn, m[1]);
  });

  // «Следовать за временем»: переключатель справа вверху.
  var followCb = document.getElementById("ch-follow");
  if (followCb) followCb.addEventListener("change", function () {
    if (followCb.checked) { if (compareCb) compareCb.checked = false; compareMode = false; startFollow(); }
    else stopFollow();
  });
  // Движок глубины окна follow (1..10 мин): значение персистентно; при смене —
  // полный перезапрос диапазона.
  var depthRange = document.getElementById("ch-depth");
  if (depthRange) {
    followDepth = loadDepth();
    depthRange.value = String(followDepth);
    depthUI();
    depthRange.addEventListener("input", function () {
      followDepth = +depthRange.value; saveDepth(followDepth); depthUI();
      if (followActive) followFull().catch(function () {});
    });
  }

  // «Показать неактуальные устройства»: перерисовка закладок. Если активное
  // устройство скрыто фильтром — выбираем первое видимое, иначе остаёмся на нём.
  if (staleCb) staleCb.addEventListener("change", function () {
    showStale = staleCb.checked;
    var vis = visibleDevs();
    if (active && vis.indexOf(active) >= 0) { renderTabs(); }
    else if (vis.length) { active = vis[0]; renderTabs(); loadCharts(); }
    else { active = null; renderTabs(); showNoData(); }
  });

  if (compareCb) compareCb.addEventListener("change", function () {
    if (compareCb.checked) setFollowOff();
    loadCharts();
  });
  loadDevs();
})();
