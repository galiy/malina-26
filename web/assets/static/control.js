// Вкладка «Управление» страницы /map: команды + группы редактируемых параметров.
// Данные о параметрах — window.MAP_CONTROL (control-data.js). Текущие значения —
// из общего raw-пула страницы (map.js зовёт window.mapControlRaw(fields, gen)).
// Запись — POST /api/map/write (hex-контракт).
(function () {
  "use strict";
  var I18N = window.I18N || {};
  var META = window.MAP_CONTROL || { groups: [], commands: {} };
  var MAP_ID = window.MAP_ID;
  var tab = document.getElementById("tab-control");
  if (!tab || MAP_ID == null) { return; }

  function t(k) { return (I18N[k] !== undefined) ? I18N[k] : k; }
  function esc(s) { return String(s).replace(/[&<>"']/g, function (c) { return { "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" }[c]; }); }
  function hex2(n) { n = n & 0xff; return ("0" + n.toString(16)).slice(-2).toUpperCase(); }
  function round(v) { return Math.round(v); }

  var F = {};            // текущие поля raw
  var GEN = window.MAP_GEN || 0;
  var state = {};        // addr -> {edited, val, origRaw}

  // --- преобразования display <-> raw (легаси-масштаб) ---
  function shift() { var u = F["UACC"]; return (typeof u === "number") ? (u & 0x1f) : 2; }
  function toRaw(p, disp) {
    var k = shift();
    switch (p.type) {
      case "list": return round(disp);
      case "number": case "sec": return round(disp);
      case "dc": return (round(disp * 10)) >> k;
      case "dc_dop": return (round(disp * 10)) >> k;
      case "power_dop": var v = round(disp / 100); if (v > 255) { v = v >> 1; } return v;
      case "ac": return round(disp) - 100;
      case "temp": return round(disp) + 50;
      case "capacity": return (round(disp) << k) / 25 | 0;
      case "current_c": return round(disp * 100);
      case "charge_time": return round(disp * 60 / 16);
      case "time": return 0;
      default: return round(disp);
    }
  }
  function toDisp(p, raw) {
    var k = shift();
    switch (p.type) {
      case "list": case "number": case "sec": return raw;
      case "dc": return (raw << k) / 10;
      case "dc_dop": return ((raw << k) + dopVal(p)) / 10;
      case "power_dop": return ((raw << dopVal(p)) * 100);
      case "ac": return raw + 100;
      case "temp": return raw - 50;
      case "capacity": return (raw * 25) >> k;
      case "current_c": return raw / 100;
      case "charge_time": return raw * 16 / 60;
      default: return raw;
    }
  }
  function dopVal(p) { if (!p.dop) { return 0; } var v = F[fieldOf(p.dop)]; return (typeof v === "number") ? v : 0; }
  function inRange(p, disp) { if (p.min == null || p.max == null) { return true; } return disp >= p.min - 1e-9 && disp <= p.max + 1e-9; }

  function trimWord(s, n) { if (s.length <= n) { return s; } var t = s.slice(0, n); var i = t.lastIndexOf(" "); if (i > 0) { t = t.slice(0, i); } return t + "…"; }
  function shortLabel(p, k) { var s = (p.short && p.short[String(k)] !== undefined) ? p.short[String(k)] : (p.enum ? p.enum[String(k)] : String(k)); return (s && s.length > 30) ? trimWord(s, 30) : s; }
  function fullLabel(p, k) { return (p.enum && p.enum[String(k)] !== undefined) ? p.enum[String(k)] : shortLabel(p, k); }

  function fieldOf(addr) { return (window.MAP_CONTROL.addr2fid || {})[addr] || ""; }

  // --- DOM ---
  function h(html) { var d = document.createElement("div"); d.innerHTML = html; return d.firstChild; }

  function build() {
    var parts = [];
    parts.push('<div class="ctl-head">' +
      '<button type="button" id="ctlSaveAll" class="ctl-btn">' + esc(t("ctl.save_all")) + '</button>' +
      '<button type="button" id="ctlCancelAll" class="ctl-btn ghost">' + esc(t("ctl.cancel_all")) + '</button>' +
      '</div>');
    // Группа «Управление» (команды)
    parts.push('<h2 class="map-sec">' + esc(t("ctl.group_control")) + '</h2>');
    parts.push(cmdGroup());
    // Группы параметров
    META.groups.forEach(function (g, gi) { parts.push(paramGroup(g, gi)); });
    tab.innerHTML = parts.join("");
    wire();
  }

  function cmdGroup() {
    return '<div class="ctl-cmds"><div class="ctl-cmdrow">' +
      '<button type="button" class="ctl-toggle" id="cmdPower"></button>' +
      '<button type="button" class="ctl-toggle" id="cmdCharge"></button>' +
      '<span id="ctlRelays"></span>' +
      '<button type="button" class="ctl-btn danger" data-cmd="reset">' + esc(t("ctl.reset")) + '</button>' +
      '</div></div>';
  }

  function paramGroup(g, gi) {
    var rows = g.params.map(function (p) { return paramRow(p); }).join("");
    return '<h2 class="map-sec">' + esc(t("ctl.g" + gi)) + '</h2>' +
      '<table class="ctl-tbl"><thead><tr>' +
      '<th>' + esc(t("ctl.col_name")) + '</th><th></th>' +
      '<th>' + esc(t("ctl.col_cur")) + '</th><th></th>' +
      '<th>' + esc(t("ctl.col_edit")) + '</th><th>' + esc(t("ctl.col_unit")) + '</th><th></th></tr></thead>' +
      '<tbody>' + rows + '</tbody></table>';
  }
  function paramRow(p) {
    var h = helpText(p);
    return '<tr data-addr="' + p.addr + '">' +
      '<td class="ctl-name"><span>' + esc(p.name) + '</span></td>' +
      '<td class="ctl-q">' + (h ? '<a href="#" class="ctl-help">?</a>' : '') + '</td>' +
      '<td class="ctl-cur"></td>' +
      '<td class="ctl-q2"></td>' +
      '<td class="ctl-edit"></td>' +
      '<td class="ctl-unit">' + esc(p.unit || "") + '</td>' +
      '<td class="ctl-act"></td></tr>';
  }
  function norm(s) { return (s || "").trim().replace(/\.+$/, "").toLowerCase(); }
  // Содержательная подсказка: описание, которое НЕ дублирует имя параметра.
  function helpText(p) {
    var d = (p.desc || "").trim();
    if (!d) { return ""; }
    if (norm(d) === norm(p.name)) { return ""; }
    if (d.length <= p.name.length + 3) { return ""; }
    return d;
  }

  // Построение/обновление контрола редактирования.
  function buildEdit(p, tdrow) {
    var td = tdrow.querySelector(".ctl-edit");
    function esc2(el) { el.addEventListener("keydown", function (e) { if (e.key === "Escape") { e.preventDefault(); cancelOne(p, tdrow); } }); }
    if (p.enum) {
      var sel = '<select class="ctl-input">';
      Object.keys(p.enum).sort(function (a, b) { return a - b; }).forEach(function (k) {
        sel += '<option value="' + esc(k) + '">' + esc(shortLabel(p, k)) + '</option>';
      });
      sel += '</select>';
      td.innerHTML = sel;
      var se = td.querySelector("select");
      se.addEventListener("change", function () { onEdit(p, this.value); });
      esc2(se);
      return;
    }
    if (p.type === "time") {
      td.innerHTML = '<input type="time" class="ctl-input" step="600">';
      var te = td.querySelector("input");
      te.addEventListener("change", function () { onEdit(p, this.value); });
      esc2(te);
      return;
    }
    td.innerHTML = '<input type="number" class="ctl-input"' +
      (p.min != null ? ' min="' + p.min + '"' : '') + (p.max != null ? ' max="' + p.max + '"' : '') +
      ' step="any">';
    var ne = td.querySelector("input");
    ne.addEventListener("input", function () { onEdit(p, this.value); });
    esc2(ne);
  }

  function fmtCur(p, raw) {
    if (raw == null) { return "—"; }
    if (p.type === "time") { return ptime(raw); }
    var d = toDisp(p, raw);
    if (p.enum) { return shortLabel(p, raw); }
    return (Math.abs(d - round(d)) < 1e-6) ? String(round(d)) : d.toFixed(2);
  }
  function ptime(raw) { var hh = raw >> 3, mm = (raw & 7) * 10; return ("0" + hh).slice(-2) + ":" + ("0" + mm).slice(-2); }

  function toInput(p, raw) {
    if (p.type === "time") { return ptime(raw); }
    if (p.type === "list" && p.enum) { return String(raw); }
    var d = toDisp(p, raw);
    return (Math.abs(d - round(d)) < 1e-6) ? String(round(d)) : d.toFixed(2);
  }

  function onEdit(p, valStr) {
    var st = state[p.addr];
    var disp = (p.type === "time") ? null : parseFloat(valStr);
    st.edited = true;
    st.disp = valStr;
    var valid = true;
    if (p.type === "time") { valid = /^\d{1,2}:\d{2}$/.test(valStr); }
    else if (p.enum) { valid = valStr !== ""; }
    else { valid = isFinite(disp) && inRange(p, disp); }
    st.valid = valid;
    var row = tab.querySelector('tr[data-addr="' + p.addr + '"]');
    row.querySelector(".ctl-edit").classList.toggle("ctl-invalid", !valid);
    row.classList.toggle("ctl-changed", true);
    renderActs(p, row);
  }

  function renderActs(p, row) {
    var st = state[p.addr], act = row.querySelector(".ctl-act");
    if (!st.edited) { act.innerHTML = ""; return; }
    act.innerHTML = (st.valid ? '<button type="button" class="ctl-btn sm" data-a="save">' + esc(t("ctl.save")) + '</button>' : '') +
      '<button type="button" class="ctl-btn sm ghost" data-a="cancel">' + esc(t("ctl.cancel")) + '</button>';
    var sv = act.querySelector('[data-a="save"]');
    if (sv) { sv.onclick = function () { saveOne(p); }; }
    act.querySelector('[data-a="cancel"]').onclick = function () { cancelOne(p, row); };
  }

  function cancelOne(p, row) {
    var st = state[p.addr];
    st.edited = false; st.valid = true; st.disp = null;
    row.classList.remove("ctl-changed");
    row.querySelector(".ctl-edit").classList.remove("ctl-invalid");
    applyRaw(p, row);
    renderActs(p, row);
  }

  function applyRaw(p, row) {
    var raw = rawOf(p);
    var cur = row.querySelector(".ctl-cur"), q2 = row.querySelector(".ctl-q2");
    if (q2) { q2.innerHTML = ""; }
    if (p.enum && raw != null) {
      var sh = shortLabel(p, raw), fu = fullLabel(p, raw);
      cur.textContent = sh;
      if (fu !== sh && q2) {
        var a = document.createElement("a"); a.href = "#"; a.className = "ctl-help ctl-hval"; a.textContent = "?";
        a.onclick = function (e) { e.preventDefault(); showPop(a, fu); };
        q2.appendChild(a);
      }
    } else {
      cur.textContent = fmtCur(p, raw);
    }
    if (!state[p.addr].edited) {
      var inp = row.querySelector(".ctl-input");
      if (inp) { inp.value = toInput(p, raw); }
    }
  }

  function rawOf(p) {
    var fid = p.fid || fieldOf(p.addr);
    if (!fid) { return null; }
    var v = F[fid];
    return (typeof v === "number") ? v : null;
  }

  function opsFor(p) {
    var st = state[p.addr], ops = [];
    var raw;
    if (p.type === "time") {
      var mm = st.disp.split(":"); var hh = parseInt(mm[0], 10), mi = parseInt(mm[1], 10);
      raw = ((hh & 0x1f) << 3) | ((Math.round(mi / 10)) & 7);
    } else if (p.enum) {
      raw = parseInt(st.disp, 10);
    } else {
      raw = toRaw(p, parseFloat(st.disp));
    }
    raw = ((raw % 256) + 256) % 256;
    ops.push({ op: "reg", addr: p.addr, data: hex2(raw) });
    if (p.dop) {
      var dop = 0;
      var k = shift();
      if (p.type === "dc_dop") { dop = (parseFloat(st.disp) * 10) - (raw << k); }
      else if (p.type === "power_dop") { dop = (parseFloat(st.disp) / 100) > 255 ? 1 : 0; }
      ops.push({ op: "reg", addr: p.dop, data: hex2(dop) });
    }
    return ops;
  }

  function post(ops, done) {
    var body = { id: MAP_ID, gen: GEN, ops: ops };
    fetch("api/map/write", { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify(body), credentials: "same-origin" })
      .then(function (r) { return r.json().then(function (j) { return { s: r.status, j: j }; }); })
      .then(function (res) { done(res.j, res.s); })
      .catch(function () { done({ ok: false, error: "network" }, 0); });
  }

  function saveOne(p) {
    post(opsFor(p), function (j) {
      var row = tab.querySelector('tr[data-addr="' + p.addr + '"]');
      if (j && j.ok) {
        state[p.addr].edited = false; row.classList.remove("ctl-changed");
        row.querySelector(".ctl-edit").classList.remove("ctl-invalid");
        renderActs(p, row);
        forceRefresh();
      } else {
        row.querySelector(".ctl-edit").classList.add("ctl-invalid");
        alert(errText(j));
      }
    });
  }

  function errText(j) { return (j && (j.error || j.code)) ? (j.code ? j.code + ": " : "") + (j.error || "") : t("ctl.error"); }

  function forceRefresh() { if (typeof window.MAP_FORCE_REFRESH === "function") { window.MAP_FORCE_REFRESH(); } }

  function saveAll() {
    var ops = [];
    META.groups.forEach(function (g) { g.params.forEach(function (p) { var st = state[p.addr]; if (st.edited && st.valid) { ops = ops.concat(opsFor(p)); } }); });
    if (!ops.length) { return; }
    post(ops, function (j) {
      if (j && j.ok) { META.groups.forEach(function (g) { g.params.forEach(function (p) { state[p.addr].edited = false; var r = tab.querySelector('tr[data-addr="' + p.addr + '"]'); if (r) { r.classList.remove("ctl-changed"); r.querySelector(".ctl-edit").classList.remove("ctl-invalid"); renderActs(p, r); } }); }); }
      else { alert(errText(j)); }
      forceRefresh();
    });
  }
  function cancelAll() { META.groups.forEach(function (g) { g.params.forEach(function (p) { var r = tab.querySelector('tr[data-addr="' + p.addr + '"]'); if (r) { cancelOne(p, r); } }); }); }

  // --- команды ---
  function setCmd(key) {
    var c = (META.commands || {})[key];
    if (!c) { return; }
    var ops = [{ op: "cmd", code: c.code }];
    post(ops, function (j) { if (!(j && j.ok)) { alert(errText(j)); } forceRefresh(); });
  }
  function confirmThen(msg, fn) { if (window.confirm(msg)) { fn(); } }
  function tripleConfirm(msg, fn) { if (window.confirm(msg + " (1/3)") && window.confirm(msg + " (2/3)") && window.confirm(msg + " (3/3)")) { fn(); } }

  function renderRelays() {
    var box = document.getElementById("ctlRelays"); if (!box) { return; }
    box.innerHTML = "";
    var st = F["Status_RELEdop"] || 0;
    // реле1/2 всегда; реле3 — только если есть funk-ячейка (модель Т)
    [[0x198, 0, "1"], [0x19C, 1, "2"], [0x120, 2, "3"]].forEach(function (r) {
      var fv = F[fieldOf(("" + r[0].toString(16)).toUpperCase())];
      if (typeof fv !== "number") { return; }
      if (fv !== 8) { return; }
      var on = (st >> r[1]) & 1;
      var b = document.createElement("button");
      b.type = "button"; b.className = "ctl-toggle" + (on ? " act-off" : "");
      b.textContent = (on ? t("ctl.relay_off") : t("ctl.relay_on")) + " " + r[2];
      var bit = 1 << r[1];
      b.onclick = function () { postRelay(on ? (st & ~bit) : (st | bit)); };
      box.appendChild(b);
    });
  }
  function postRelay(value) { post([{ op: "reg", addr: (META.commands.relay_cell || "586"), data: hex2(value) }], function (j) { if (!(j && j.ok)) { alert(errText(j)); } forceRefresh(); }); }

  function wire() {
    var sa = document.getElementById("ctlSaveAll"); if (sa) { sa.onclick = saveAll; }
    var ca = document.getElementById("ctlCancelAll"); if (ca) { ca.onclick = cancelAll; }
    // «?» подсказки
    tab.querySelectorAll("tr[data-addr]").forEach(function (row) {
      var p = paramByAddr(row.getAttribute("data-addr"));
      var hq = row.querySelector(".ctl-q .ctl-help");
      if (hq) { hq.addEventListener("click", function (e) { e.preventDefault(); showHelp(p, this); }); }
      state[p.addr] = { edited: false, valid: true };
      buildEdit(p, row);
    });
    // команды
    var pw = document.getElementById("cmdPower");
    if (pw) { pw.onclick = function () { if (powerOn()) { confirmThen(t("ctl.confirm_off"), function () { setCmd("off"); }); } else { setCmd("on"); } }; }
    var chOn = document.getElementById("cmdCharge");
    if (chOn) { chOn.onclick = function () { setCmd(chargeOn() ? "charge_off" : "charge_on"); }; }
    var rst = tab.querySelector('[data-cmd="reset"]'); if (rst) { rst.onclick = function () { tripleConfirm(t("ctl.confirm_reset"), function () { setCmd("reset"); }); }; }
  }
  function paramByAddr(a) { var r = null; META.groups.forEach(function (g) { g.params.forEach(function (p) { if (p.addr === a) { r = p; } }); }); return r; }

  // Всплывающая подсказка (немодальная).
  var pop = null;
  function closePop() { if (pop) { pop.remove(); pop = null; document.removeEventListener("click", popOutside, true); document.removeEventListener("keydown", popEsc); } }
  function popOutside(e) { if (pop && !pop.contains(e.target)) { closePop(); } }
  function popEsc(e) { if (e.key === "Escape") { closePop(); } }
  function showPop(anchor, text) {
    if (pop) { closePop(); return; } // повторный клик по тому же «?» закрывает
    pop = document.createElement("div");
    pop.className = "ctl-pop";
    pop.textContent = text;
    document.body.appendChild(pop);
    var r = anchor.getBoundingClientRect();
    var top = r.bottom + 6, left = r.left;
    if (left + 440 > window.innerWidth) { left = Math.max(8, window.innerWidth - 448); }
    if (top + pop.offsetHeight > window.innerHeight) { top = Math.max(8, r.top - pop.offsetHeight - 6); }
    pop.style.top = top + "px"; pop.style.left = left + "px";
    setTimeout(function () { document.addEventListener("click", popOutside, true); document.addEventListener("keydown", popEsc); }, 0);
  }
  function showHelp(p, anchor) { showPop(anchor, helpText(p)); }

  // обновление состояния команд из текущих полей
  // Заряд включён ⇔ МАП в режиме M_ONCHARGE (4) и StatusCh != 0.
  function chargeOn() { return F["MODE_RAM"] === 4 && ((F["StatusCh"] || 0) !== 0); }
  function powerOn() { var m = F["MODE_RAM"]; return (typeof m === "number") && (m !== 0); }
  function updateCommands() {
    var pb = document.getElementById("cmdPower");
    if (pb) { var on = powerOn(); pb.classList.toggle("act-off", on); pb.textContent = on ? t("ctl.off") : t("ctl.on"); }
    var ch = document.getElementById("cmdCharge");
    if (ch) { var con = chargeOn(); ch.classList.toggle("act-off", con); ch.textContent = con ? t("ctl.charge_off") : t("ctl.charge_on"); }
    renderRelays();
  }

  // --- публичный хук из map.js ---
  window.mapControlRaw = function (fields, gen) {
    F = fields || {};
    if (gen) { GEN = gen; }
    META.groups.forEach(function (g) { g.params.forEach(function (p) { var row = tab.querySelector('tr[data-addr="' + p.addr + '"]'); if (row) { applyRaw(p, row); } }); });
    updateCommands();
  };

  build();
})();
