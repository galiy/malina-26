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
        { n: "stats.f.map.Temp_Grad2", k: "16" }] }
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
    mbms: [
      { t: "stats.c.cells_v", cellGroup: 1 },
      { t: "stats.c.spread", lines: [{ n: "stats.n.spread", calc: "spread" }] },
      { t: "stats.c.pack", lines: [{ n: "stats.n.pack", calc: "pack" }] },
      { t: "stats.c.cells_t", cellGroup: 2 },
      { t: "stats.c.cells_i", cellGroup: 0 }
    ]
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
  var CALC_DEPS = { map: ["33", "29", "30", "31", "32"] }; // PNET_Sign_P + MPPT-каналы
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

  // Pnet = Uin·Iin со знаком по PNET_Sign_P (0 → отрицательное), как в панели 1.
  function pnetAt(vals, t) {
    var u = g(vals, "1", t), i = g(vals, "3", t);
    if (u == null || i == null) return undefined;
    var p = u * i, s = g(vals, "33", t);
    if (s === 0) p = -p;
    return p;
  }
  function pbatAt(vals, t) {
    var ua = g(vals, "8", t), ia = g(vals, "10", t);
    return (ua != null && ia != null) ? ua * ia : undefined;
  }
  function calcSeries(calc, vals) {
    var out = [];
    vals.times.forEach(function (t) {
      var v;
      if (calc === "imppt") v = sumKeys(vals, t, [29, 30, 31, 32]);
      else if (calc === "pnet") v = pnetAt(vals, t);
      else if (calc === "pbat") v = pbatAt(vals, t);
      else if (calc === "pout") { var pn = pnetAt(vals, t), pb = pbatAt(vals, t); v = (pn != null && pb != null) ? (pn - pb) : undefined; }
      else if (calc === "iout") { var pn2 = pnetAt(vals, t), pb2 = pbatAt(vals, t), uo = g(vals, "6", t); var po = (pn2 != null && pb2 != null) ? (pn2 - pb2) : undefined; v = (po != null && uo) ? po / uo : undefined; }
      else if (calc === "pmppt") { var ua2 = g(vals, "8", t), im = sumKeys(vals, t, [29, 30, 31, 32]); v = (ua2 != null && im != null) ? ua2 * im : undefined; }
      else if (calc === "spread" || calc === "pack") {
        var vals2 = [], m = vals.byKey;
        Object.keys(m).forEach(function (k) { var id = +k; if (id % 3 === 1) { var x = m[k].get(t); if (x != null) vals2.push(x); } });
        if (!vals2.length) return;
        if (calc === "spread") { v = Math.max.apply(null, vals2) - Math.min.apply(null, vals2); }
        else { v = vals2.reduce(function (a, b) { return a + b; }, 0); }
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
    if (p === "last") return { from: 0, to: to }; // «Последние данные» — хвост БД (как в таблицах)
    return { from: to - 24 * 3600e3, to: to };
  }
  // Значение для <input type="datetime-local" step="1"> (локальная зона, секунды).
  function toLocalInput(ms) { var d = new Date(ms), p = function (x) { return (x < 10 ? "0" : "") + x; }; return d.getFullYear() + "-" + p(d.getMonth() + 1) + "-" + p(d.getDate()) + "T" + p(d.getHours()) + ":" + p(d.getMinutes()) + ":" + p(d.getSeconds()); }

  // --- Устройства/закладки ---
  var devs = [], active = null;
  function devLabel(d) { return (d.type === "map" ? "МАП" : d.type === "kes" ? "КЭС" : "BMS") + " " + d.id; }
  function renderTabs() {
    tabsEl.innerHTML = "";
    devs.forEach(function (d, i) {
      var b = document.createElement("button");
      b.className = "stats-tab" + (active === d ? " active" : "");
      b.textContent = devLabel(d);
      b.addEventListener("click", function () { active = d; renderTabs(); loadCharts(); });
      tabsEl.appendChild(b);
    });
  }
  function loadDevs() {
    return api("/api/stats/devices").then(function (j) {
      devs = (j && j.devices) || [];
      if (devs.length) { active = devs[0]; renderTabs(); loadCharts(); }
      else chartsEl.innerHTML = '<p class="muted">' + esc(T("stats.no_data")) + "</p>";
    }).catch(function () {});
  }

  // --- Рендер карточек ---
  var CHARTS = {};
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
  var orderKey = function () { return "statsChartOrder:" + (active ? active.type : ""); };

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
  function moveCard(btn, dir) {
    var card = btn.closest(".stats-card");
    var cards = Array.prototype.slice.call(chartsEl.querySelectorAll(".stats-card"));
    var idx = cards.indexOf(card), cols = 2, target = -1;
    if (dir === "up") target = idx - cols;
    else if (dir === "down") target = idx + cols < cards.length ? idx + cols : -1;
    else if (dir === "left") target = idx % cols === 1 ? idx - 1 : -1;
    else if (dir === "right") target = idx % cols === 0 && idx + 1 < cards.length ? idx + 1 : -1;
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
  function fmtVal(v) { if (v == null || !isFinite(v)) return "—"; var a = Math.abs(v); return v.toFixed(a >= 100 ? 0 : a >= 10 ? 1 : 2); }
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
              ds.forEach(function (d, i) { if (c.isDatasetVisible(i) && d.data[lo]) rows.push({ col: d.borderColor, s: d.label + ": " + fmtVal(d.data[lo][1]) }); });
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
    var all = document.createElement("button"); all.type = "button"; all.className = "lg-chip lg-all"; all.textContent = "Все";
    all.addEventListener("click", function () { chart.data.datasets.forEach(function (x, j) { chart.setDatasetVisibility(j, true); }); chart.update("none"); sync(); saveState(); });
    row.appendChild(all);
    sync();
  }

  // Перезапрос под окно при zoom/панораме (§3.5): debounce, окно берётся из осей,
  // bucket на бэкенде пересчитывается от нового окна (детализация растёт при зуме).
  var reloadTimers = {};
  function scheduleReload(cid) {
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
    card.innerHTML = orderBtns() + "<h2>" + esc(T(set.t)) + '</h2><div class="chart-toolbar"><span></span></div>' +
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

  function loadCharts() {
    if (!active) return;
    if (!range.from) { range = preset("24h"); }
    document.getElementById("ch-from").value = toLocalInput(range.from);
    document.getElementById("ch-to").value = toLocalInput(range.to);
    var url = "/api/stats/series?type=" + encodeURIComponent(active.type) + "&id=" + encodeURIComponent(active.id) + "&from=" + range.from + "&to=" + range.to +
      "&fields=" + encodeURIComponent(fieldsFor(active.type).join(","));
    api(url).then(function (j) {
      Object.keys(CHARTS).forEach(function (id) { try { CHARTS[id].destroy(); } catch (e) {} delete CHARTS[id]; });
      chartsEl.innerHTML = "";
      var vals = buildVals((j && j.series) || {});
      var sets = SETS[active.type] || [];
      ensureHidden(active.type); // восстановить настройку видимости линий типа
      sets.forEach(function (set, si) {
        var lines = set.cellGroup !== undefined ? cellGroupLines(set.cellGroup) : set.lines;
        var datasets = [], cid = "ch" + si;
        lines.forEach(function (ln, li) {
          var data = ln.calc ? calcSeries(ln.calc, vals) : ((j && j.series && j.series[ln.k]) || []).map(function (p) { return [+p[0], +p[1]]; });
          if (!data.length) return;
          var name = T(ln.n) + (ln.cell ? " " + ln.cell : "");
          var keyStr = ln.n + (ln.cell ? ":" + ln.cell : ""); // стабильный ключ для персиста
          var pr = data.length < 3 ? 3 : 0; // редкие данные — точки видимы
          datasets.push({ label: name, keyStr: keyStr, data: data, borderColor: COLORS[li % COLORS.length], backgroundColor: COLORS[li % COLORS.length], pointRadius: pr, pointHoverRadius: 3, pointBorderColor: COLORS[li % COLORS.length], borderWidth: 1.5, tension: .35, cubicInterpolationMode: "monotone", fill: false });
        });
        renderCard(set, cid, datasets);
      });
      applyOrder();
    }).catch(function () {});
  }

  // --- События UI ---
  Array.prototype.forEach.call(document.querySelectorAll(".stats-controls [data-period]"), function (b) {
    b.addEventListener("click", function () { range = preset(b.getAttribute("data-period")); zoomWin = {}; loadCharts(); });
  });
  document.getElementById("ch-show").addEventListener("click", function () {
    var f = document.getElementById("ch-from").value, t = document.getElementById("ch-to").value;
    var fn = f ? new Date(f).getTime() : NaN, tn = t ? new Date(t).getTime() : NaN;
    if (!isFinite(fn) || !isFinite(tn) || fn > tn) return; range = { from: fn, to: tn }; zoomWin = {}; loadCharts();
  });
  document.getElementById("ch-reload").addEventListener("click", function () { loadCharts(); });

  loadDevs();
})();
