// Страница BMS: «Панель» — бинарный контракт 107 Б (docs/bms-reader.md §1) с
// /api/bms/raw?id&gen. Декод и трактовка alarm (по bms_type) — на клиенте.
// Графики — общий stats_charts.js (режим window.STATS_FIXED).
(function () {
  "use strict";
  var I18N = window.I18N || {};
  var ID = window.BMS_ID, GEN = window.BMS_GEN, SERIAL = window.BMS_SERIAL || "";
  var BMS_TYPE = window.BMS_TYPE || "";
  var errBox = document.getElementById("bmsErr");
  var pause = document.getElementById("bmsPause");
  if (ID == null) { return; }
  function t(k) { return I18N[k] || k; }
  function esc(s) { return String(s == null ? "" : s).replace(/[&<>"]/g, function (c) { return { "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;" }[c]; }); }
  function n(v, d) { return (typeof v === "number" && isFinite(v)) ? v.toFixed(d === undefined ? 1 : d) : "—"; }
  var MODES = ["idle", "discharge", "charge", "maintain", "full", "shutdown"];

  // --- Декод контракта (big-endian) ---
  function decode(buf) {
    var b = new Uint8Array(buf), dv = new DataView(buf);
    if (b.length < 107) { throw new Error("bad payload"); }
    function u8(o) { return b[o]; }
    function u16(o) { return dv.getUint16(o, false); }
    function i16(o) { return dv.getInt16(o, false); }
    function i32(o) { return dv.getInt32(o, false); }
    function u32(o) { return dv.getUint32(o, false); }
    var d = { cell_count: u8(0), temp_count: u8(1), flags: u16(2) };
    d.cells_v = []; for (var i = 0; i < d.cell_count; i++) { d.cells_v.push(u16(4 + 2 * i) / 1000); }
    d.temperatures_c = []; for (var j = 0; j < d.temp_count; j++) { d.temperatures_c.push(i16(36 + 2 * j) / 10); }
    d.current_a = i32(52) / 100; d.power_w = i32(56);
    d.soc = u16(60) / 10; d.soh = u16(62) / 10;
    d.capacity_ah = u32(64) / 1000; d.remaining_ah = u32(68) / 1000; d.rated_ah = u32(72) / 1000;
    d.cycles = u16(76); d.total_voltage_v = u16(78) / 100; d.port_voltage_v = u16(80) / 100;
    d.balance_mask = u32(82); d.mode = u16(86) & 0xFF;
    d.alarm = []; for (var k = 0; k < 19; k++) { d.alarm.push(b[88 + k]); }
    // Производные по ячейкам.
    var mx = 0, mn = 0, sum = 0;
    for (var c = 0; c < d.cells_v.length; c++) {
      sum += d.cells_v[c];
      if (d.cells_v[c] > d.cells_v[mx]) { mx = c; }
      if (d.cells_v[c] < d.cells_v[mn]) { mn = c; }
    }
    d.max_cell_idx = d.cells_v.length ? mx + 1 : 0; d.min_cell_idx = d.cells_v.length ? mn + 1 : 0;
    d.max_cell_v = d.cells_v.length ? d.cells_v[mx] : 0; d.min_cell_v = d.cells_v.length ? d.cells_v[mn] : 0;
    d.avg_cell_v = d.cells_v.length ? sum / d.cells_v.length : 0;
    return d;
  }

  // --- Трактовка alarm по типу (зеркало daemons/bms-reader/docs/bms/<TYPE>.md) ---
  function abit(a, bit) { return (bit >= 0 && (bit >> 3) < a.length) && ((a[bit >> 3] >> (bit & 7)) & 1) === 1; }
  function anib(a, base) { var v = 0; for (var b = 0; b < 4; b++) { if (abit(a, base + b)) { v |= 1 << b; } } return v; }
  function decodeAlarms(d) {
    var a = d.alarm, out = { alarms: [], protections: [], cell_warn: 0, cell_prot: 0, temp_warn: 0, temp_prot: 0 };
    function A(k) { return t("bms.alarm." + k); }
    if (BMS_TYPE === "AntBMS-DSP") {
      // Коды MOS (0..15) — только защитные состояния считаем аварией (нормальные
      // коды: 0=Off, 1=On, 4=battery full, 11=charge on, 15=manually closed, а также
      // 2/4 в балансировке). Зеркало antMOSFault (web/internal/web/bms.go).
      var ch = { 2: "overvoltage", 3: "overcurrent", 5: "overpressure", 6: "battery_overtemp", 7: "power_overtemp", 8: "abnormal_current", 9: "balance_line", 10: "board_overtemp", 13: "discharge_tube" };
      var di = { 2: "overdischarge", 3: "overcurrent", 5: "undervoltage", 6: "battery_overtemp", 7: "power_overtemp", 8: "abnormal_current", 9: "balance_line", 10: "board_overtemp", 12: "short_circuit", 13: "discharge_tube", 14: "start_exception" };
      var ba = { 3: "balance_overtemp", 10: "board_overtemp" };
      var cc = anib(a, 0); if (ch[cc]) { out.alarms.push(A("grp_ch") + ": " + A(ch[cc])); }
      var dc = anib(a, 4); if (di[dc]) { out.alarms.push(A("grp_dis") + ": " + A(di[dc])); }
      var bc = anib(a, 8); if (ba[bc]) { out.alarms.push(A("grp_bal") + ": " + A(ba[bc])); }
      var sys = 0; for (var k = 0; k < 16; k++) { if (abit(a, 12 + k)) { sys |= 1 << k; } }
      if (sys) { out.alarms.push(A("syslog") + ": status=" + (sys & 31) + " battery=" + ((sys >> 5) & 31) + " order=" + ((sys >> 10) & 31) + " chg/dis=" + ((sys >> 15) & 1)); }
    } else if (BMS_TYPE === "EnBMS-RS485") {
      var cellTxt = ["cell_high_warn", "cell_high_prot", "cell_low_warn", "cell_low_prot"];
      var tempTxt = ["temp_high_warn", "temp_high_prot", "temp_low_warn", "temp_low_prot"];
      for (var i = 0; i < d.cell_count && i < 16; i++) {
        var vv = anib(a, i * 4);
        for (var bb = 0; bb < 4; bb++) { if (vv & (1 << bb)) { var m = A("cell") + " " + (i + 1) + ": " + A(cellTxt[bb]); if (bb === 1 || bb === 3) { out.protections.push(m); out.cell_prot |= 1 << i; } else { out.alarms.push(m); out.cell_warn |= 1 << i; } } }
      }
      for (var ti = 0; ti < d.temp_count && ti < 8; ti++) {
        var tv = anib(a, 64 + ti * 4);
        for (var tb = 0; tb < 4; tb++) { if (tv & (1 << tb)) { var tm = A("sensor") + " " + (ti + 1) + ": " + A(tempTxt[tb]); if (tb === 1 || tb === 3) { out.protections.push(tm); out.temp_prot |= 1 << ti; } else { out.alarms.push(tm); out.temp_warn |= 1 << ti; } } }
      }
      var single = { 96: "ambient_warn", 97: "power_warn", 98: "charge_current_warn", 99: "user_input_warn" };
      for (var sb = 96; sb <= 99; sb++) { if (abit(a, sb)) { out.alarms.push(A(single[sb])); } }
      for (var by = 0; by < 6; by++) { for (var bi = 0; bi < 8; bi++) { if (by === 5 && bi > 6) { continue; } if (abit(a, 100 + by * 8 + bi)) { out.alarms.push(A("ext") + " B" + by + "." + bi); } } }
    }
    return out;
  }

  // --- Рендер ---
  function kpi(label, val, unit, cls, sub) {
    return '<div class="kpi"><div class="lbl">' + esc(label) + '</div><div class="val ' + (cls || "") + '">' + val +
      (unit ? '<span class="unit">' + esc(unit) + "</span>" : "") + "</div>" + (sub ? '<div class="sub">' + esc(sub) + "</div>" : "") + "</div>";
  }
  function renderKpi(d) {
    var el = document.getElementById("bmsKpi"); if (!el) { return; }
    var mode = (d.mode >= 0 && d.mode < 6) ? t("bms.mode." + MODES[d.mode]) : "";
    var socCol = d.soc < 20 ? "#d9534f" : d.soc < 50 ? "#f08c00" : d.soc < 80 ? "#f0ad4e" : "#37b24d";
    var h = "";
    h += kpi(t("bms.chart.soc"), '<span style="color:' + socCol + '">' + n(d.soc, 0) + "</span>", "%", "", t("bms.kpi.rem") + " " + n(d.remaining_ah, 1) + " / " + n(d.capacity_ah, 0) + " А·ч");
    h += kpi(t("bms.chart.pack"), n(d.total_voltage_v, 2), "V", "", d.cell_count + " " + t("bms.kpi.cells") + ", " + t("bms.kpi.avg") + " " + n(d.avg_cell_v, 3) + " В");
    if (d.port_voltage_v > 0) { h += kpi(t("bms.kpi.portv"), n(d.port_voltage_v, 2), "V"); }
    h += kpi(t("stats.c.i"), n(d.current_a, 1), "A", d.current_a >= 0 ? "pos" : "neg");
    h += kpi(t("stats.c.p"), n(d.power_w, 1), "W", d.power_w >= 0 ? "pos" : "neg");
    h += kpi(t("bms.kpi.cap"), n(d.capacity_ah, 0), "А·ч");
    h += kpi(t("bms.cell.min"), n(d.min_cell_v, 3), "V", "", "№ " + d.min_cell_idx);
    h += kpi(t("bms.cell.max"), n(d.max_cell_v, 3), "V", "", "№ " + d.max_cell_idx);
    h += kpi(t("bms.cell.diff"), n(d.max_cell_v - d.min_cell_v, 3), "V");
    if (d.soh > 0) { h += kpi("SOH", n(d.soh, 1), "%"); }
    if (d.cycles > 0) { h += kpi(t("bms.kpi.cycles"), String(d.cycles)); }
    if (mode) { h += kpi(t("bms.kpi.mode"), esc(mode)); }
    if (d.rated_ah > 0) { h += kpi(t("bms.kpi.rated"), n(d.rated_ah, 0), "А·ч"); }
    el.innerHTML = h;
  }
  function renderCells(d, al) {
    var el = document.getElementById("bmsCellsGrid"); if (!el) { return; }
    var h = "";
    for (var i = 0; i < d.cells_v.length; i++) {
      var bit = 1 << i, cls = "cell", v = d.cells_v[i];
      if (d.balance_mask & bit) { cls += " bal"; }
      if (al.cell_prot & bit) { cls += " prot"; } else if (al.cell_warn & bit) { cls += " warn"; }
      if (i + 1 === d.max_cell_idx) { cls += " max"; } else if (i + 1 === d.min_cell_idx) { cls += " min"; }
      // «Батарейка»: заливка по напряжению (шкала LiFePO4 3.00–3.65 В), цвет — по роли ячейки.
      var col = (i + 1 === d.max_cell_idx) ? "#d9534f" : (i + 1 === d.min_cell_idx ? "#428bca" : "#37b24d");
      var pct = Math.max(4, Math.min(100, (v - 3.0) / 0.65 * 100));
      h += '<div class="' + cls + '"><div class="cell-batt"><div class="cell-fill" style="height:' + pct +
        "%;background:" + col + '"></div></div><div class="mv">' + v.toFixed(3) + '</div><div class="idx">' + (i + 1) + "</div></div>";
    }
    el.innerHTML = h;
    var dl = document.getElementById("bmsCellDelta");
    if (dl) { dl.textContent = t("bms.cell.delta") + " " + (d.max_cell_v - d.min_cell_v).toFixed(3) + " " + t("kes.v"); }
  }
  function renderTemps(d, al) {
    var el = document.getElementById("bmsTempsWrap"); if (!el) { return; }
    var h = "";
    for (var i = 0; i < d.temperatures_c.length; i++) {
      var bit = 1 << i, cls = "trow", tv = d.temperatures_c[i];
      if (al.temp_prot & bit) { cls += " prot"; } else if (al.temp_warn & bit) { cls += " warn"; }
      var tc = tv < 15 ? "#428bca" : tv <= 40 ? "#37b24d" : tv <= 55 ? "#f08c00" : "#d9534f";
      var w = Math.max(2, Math.min(100, tv / 60 * 100));
      h += '<div class="' + cls + '"><span class="tname">T' + (i + 1) + '</span><span class="tbar"><span class="tbar-fill" style="width:' +
        w + "%;background:" + tc + '"></span></span><span class="tval" style="color:' + tc + '">' + n(tv, 0) + " &deg;C</span></div>";
    }
    el.innerHTML = h || '<span class="muted">—</span>';
  }
  function renderMos(d) {
    var el = document.getElementById("bmsMosWrap"); if (!el) { return; }
    function on(b) { return (d.flags & (1 << b)) ? "on" : ""; }
    var bals = [];
    if (d.balance_mask) { for (var i = 0; i < d.cell_count && i < 32; i++) { if ((d.balance_mask >>> i) & 1) { bals.push(i + 1); } } }
    el.innerHTML =
      '<span class="mos ' + on(0) + '">' + esc(t("bms.kpi.chg")) + ": " + (on(0) ? "ВКЛ" : "ВЫКЛ") + "</span>" +
      '<span class="mos ' + on(1) + '">' + esc(t("bms.kpi.dis")) + ": " + (on(1) ? "ВКЛ" : "ВЫКЛ") + "</span>" +
      '<span class="mos ' + (on(2) || bals.length ? "on" : "") + '">' + esc(t("bms.kpi.bal")) + ": " + (bals.length ? bals.join(", ") : (on(2) ? "ВКЛ" : "ВЫКЛ")) + "</span>";
  }
  function renderAlarms(d, al) {
    var el = document.getElementById("bmsAlarms"); if (!el) { return; }
    var type = '<div class="alarm-type">' + esc(t("bms.kpi.type")) + ": " + esc(BMS_TYPE) + "</div>";
    if (!al.alarms.length && !al.protections.length) { el.innerHTML = type + '<span class="ok">' + esc(t("bms.alarms_ok")) + "</span>"; return; }
    function group(title, items) { if (!items.length) { return ""; } var g = '<div class="alarm-group"><div class="alarm-group-h">' + esc(title) + " (" + items.length + ")</div><ul>"; for (var i = 0; i < items.length; i++) { g += "<li>" + esc(items[i]) + "</li>"; } return g + "</ul></div>"; }
    el.innerHTML = type + group(t("bms.protections"), al.protections) + group(t("bms.alarms"), al.alarms);
  }
  function render(d) { var al = decodeAlarms(d); renderKpi(d); renderCells(d, al); renderTemps(d, al); renderMos(d); renderAlarms(d, al); }

  function showErr(m) { if (!errBox) { return; } if (m) { errBox.textContent = m; errBox.hidden = false; } else { errBox.hidden = true; } }
  function relocate() {
    var tries = 0, max = 60;
    (function attempt() {
      fetch("/api/bms/devices", { cache: "no-store", credentials: "same-origin" }).then(function (r) { return r.json(); }).then(function (list) {
        var d = null; list = list || [];
        for (var i = 0; i < list.length; i++) { if (SERIAL && String(list[i].serial) === SERIAL) { d = list[i]; break; } }
        // Никакого fallback на единственную живую BMS: это открыло бы чужое устройство.
        if (d) { window.location.href = "/bms?id=" + encodeURIComponent(d.id) + "&gen=" + encodeURIComponent(d.gen) + "&serial=" + encodeURIComponent(d.serial); return; }
        if (++tries < max) { setTimeout(attempt, 1000); } else { window.location.href = "/"; }
      }).catch(function () { if (++tries < max) { setTimeout(attempt, 1000); } else { window.location.href = "/"; } });
    })();
  }
  function tick(force) {
    if (pause && pause.checked && !force) { return; }
    if (inflight) { inflight.abort(); }
    inflight = new AbortController();
    fetch("/api/bms/raw?id=" + encodeURIComponent(ID) + "&gen=" + encodeURIComponent(GEN), { cache: "no-store", credentials: "same-origin", signal: inflight.signal })
      .then(function (r) { if (r.status === 404) { throw { gone: true }; } if (r.status === 401) { throw { auth: true }; } if (!r.ok) { throw new Error("HTTP " + r.status); } return r.arrayBuffer(); })
      .then(function (buf) { showErr(""); render(decode(buf)); })
      .catch(function (e) { if (e && e.auth) { window.location.href = "/login"; return; } if (e && e.gone) { relocate(); return; } if (!(e && e.name === "AbortError")) { showErr(t("map.connection_lost")); } });
  }
  var inflight = null;
  function start() { if (timer) { return; } tick(); timer = setInterval(tick, 1000); }
  function stop() { if (timer) { clearInterval(timer); timer = null; } if (inflight) { inflight.abort(); inflight = null; } }
  var timer = null;
  if (pause) { pause.addEventListener("change", function () { if (pause.checked) { stop(); } else { start(); } }); }
  start();
})();
