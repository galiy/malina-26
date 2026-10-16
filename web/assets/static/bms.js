// Страница BMS: закладки «Панель/Информация/Управление» (заглушки) + Memo с JSON.
// Live-данные — polling /api/bms/raw?id&gen раз в 1с (бинарный контракт 107 Б,
// раскладка — docs/bms-reader.md §1). Декод — big-endian.
(function () {
  "use strict";
  var I18N = window.I18N || {};
  var ID = window.BMS_ID;
  var GEN = window.BMS_GEN;
  var errBox = document.getElementById("bmsErr");
  var memo = document.getElementById("bmsMemo");
  var pause = document.getElementById("bmsPause");
  if (ID == null) { return; }
  function t(k) { return I18N[k] || k; }

  var INTERVAL = 1000, timer = null, inflight = null;
  var SIZE = 107;

  function parse(buf) {
    var b = new Uint8Array(buf), dv = new DataView(buf);
    if (b.length < SIZE) { throw new Error("bad payload"); }
    function u8(o) { return b[o]; }
    function u16(o) { return dv.getUint16(o, false); } // BE
    function i16(o) { return dv.getInt16(o, false); }
    function i32(o) { return dv.getInt32(o, false); }
    function u32(o) { return dv.getUint32(o, false); }
    var i, f = {};
    f.cell_count = u8(0); f.temp_count = u8(1); f.flags = u16(2);
    f.cells_mv = []; for (i = 0; i < 16; i++) { f.cells_mv.push(u16(4 + 2 * i)); }
    f.temps_dc = []; for (i = 0; i < 8; i++) { f.temps_dc.push(i16(36 + 2 * i)); }
    f.current_da = i32(52); f.power_w = i32(56);
    f.soc_dp = u16(60); f.soh_dp = u16(62);
    f.cap_mah = u32(64); f.remaining_mah = u32(68); f.rated_mah = u32(72);
    f.cycles = u16(76); f.total_cv = u16(78); f.port_cv = u16(80);
    f.balance_mask = u32(82); f.mode = u16(86);
    f.alarm = []; for (i = 0; i < 19; i++) { f.alarm.push(b[88 + i]); }
    return f;
  }

  function showErr(m) { if (!errBox) { return; } if (m) { errBox.textContent = m; errBox.hidden = false; } else { errBox.hidden = true; } }

  function setMemo(f) { if (memo) { memo.textContent = JSON.stringify(f, null, 2); } }

  // Потеря устройства (404): найти BMS по серийнику и перейти на актуальный id/gen.
  function relocate() {
    var serial = String(window.BMS_SERIAL || "");
    var tries = 0, max = 60;
    (function attempt() {
      fetch("api/bms/devices", { cache: "no-store", credentials: "same-origin" })
        .then(function (r) { return r.json(); })
        .then(function (list) {
          var d = null; list = list || [];
          for (var i = 0; i < list.length; i++) { if (serial && String(list[i].serial) === serial) { d = list[i]; break; } }
          if (!d && list.length === 1) { d = list[0]; }
          if (d) { window.location.href = "bms?id=" + encodeURIComponent(d.id) + "&gen=" + encodeURIComponent(d.gen) + "&serial=" + encodeURIComponent(serial); return; }
          if (++tries < max) { setTimeout(attempt, 1000); } else { window.location.href = "/"; }
        })
        .catch(function () { if (++tries < max) { setTimeout(attempt, 1000); } else { window.location.href = "/"; } });
    })();
  }

  function tick(force) {
    if (pause && pause.checked && !force) { return; }
    if (inflight) { inflight.abort(); } // снять подвисший предыдущий запрос
    inflight = new AbortController();
    fetch("api/bms/raw?id=" + encodeURIComponent(ID) + "&gen=" + encodeURIComponent(GEN),
      { cache: "no-store", signal: inflight.signal, credentials: "same-origin" })
      .then(function (r) {
        if (r.status === 404) { throw { gone: true }; }
        if (r.status === 401) { throw { auth: true }; }
        if (!r.ok) { throw new Error("HTTP " + r.status); }
        return r.arrayBuffer();
      })
      .then(function (buf) { showErr(""); setMemo(parse(buf)); })
      .catch(function (e) {
        if (e && e.auth) { window.location.href = "login"; return; }
        if (e && e.gone) { relocate(); return; }
        if (!(e && e.name === "AbortError")) { showErr(t("map.connection_lost")); }
      });
  }

  function initTabs() {
    var tabs = document.querySelectorAll(".map-tab");
    var panes = { panel: document.getElementById("tab-panel"), info: document.getElementById("tab-info"), control: document.getElementById("tab-control") };
    function show(name) {
      Array.prototype.forEach.call(tabs, function (b) { b.classList.toggle("active", b.getAttribute("data-tab") === name); });
      Object.keys(panes).forEach(function (k) { if (panes[k]) { panes[k].hidden = (k !== name); } });
    }
    Array.prototype.forEach.call(tabs, function (b) { b.addEventListener("click", function () { show(b.getAttribute("data-tab")); }); });
  }
  initTabs();

  function start() { if (timer) { return; } tick(); timer = setInterval(tick, INTERVAL); }
  function stop() { if (timer) { clearInterval(timer); timer = null; } if (inflight) { inflight.abort(); inflight = null; } }
  if (pause) { pause.addEventListener("change", function () { if (pause.checked) { stop(); } else { start(); } }); }
  start();
})();
