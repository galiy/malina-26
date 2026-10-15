// Страница КЭС: панель (по мотивам легаси-монитора КЭС), «Информация» (основные/
// системные) и «Управление». Live-данные — polling /api/kes/raw?id&gen раз в 1с
// (тот же поток raw слота). Распаковка зеркалит web/internal/web/kesmem.go.
(function () {
  "use strict";
  var I18N = window.I18N || {};
  var ID = window.KES_ID;
  var GEN = window.KES_GEN;
  var errBox = document.getElementById("kesErr");
  var pause = document.getElementById("kesPause");
  if (ID == null) { return; }
  function t(k) { return I18N[k] || k; }

  var INTERVAL = 1000, timer = null, inflight = null, lastF = null, curTab = "monitor";

  var KESE = 0, KESR = 512, KESS = 1024, SLOT = 1536;
  function u8(b, o) { return b[o]; }
  function u16(dv, o) { return dv.getUint16(o, true); }
  function u64(dv, o) { return Number(dv.getBigUint64(o, true)); }
  function magic(b, o) { return String.fromCharCode(b[o], b[o + 1], b[o + 2], b[o + 3]); }
  function charField(v) { if (v === 0) { return ""; } if (v > 0x2A && v <= 0x7C) { return String.fromCharCode(v); } return "U"; }
  function pad3(n) { n = String(n); while (n.length < 3) { n = "0" + n; } return n; }

  function parse(buf) {
    var b = new Uint8Array(buf), dv = new DataView(buf);
    if (b.length < SLOT || magic(b, KESE) !== "KESE" || magic(b, KESR) !== "KESR" || magic(b, KESS) !== "KESS") { throw new Error("bad payload"); }
    if (b[KESE + 4] !== 1 || b[KESR + 4] !== 1 || b[KESS + 4] !== 1) { throw new Error("bad version"); }
    var f = {};
    f.Serial = u16(dv, KESE + 6); f.Device = u8(b, KESE + 8); f.Pow = u8(b, KESE + 9);
    f.FW = u8(b, KESE + 10); f.ModelPow = u8(b, KESE + 11); f.ModelVolt = u8(b, KESE + 12);
    f.Pic = u8(b, KESE + 13); f.Net = u8(b, KESE + 14);
    f.LastSeen = u64(dv, KESR + 6); f.RelayC = u8(b, KESR + 14); f.RSErrSis = u8(b, KESR + 15);
    f.I2CErr = u8(b, KESR + 16);
    f.VcPV = u16(dv, KESR + 18); f.IcPV = u16(dv, KESR + 20); f.VBat = u16(dv, KESR + 22);
    f.PPV = u16(dv, KESR + 24); f.POut = u16(dv, KESR + 26); f.PLoad = u16(dv, KESR + 28);
    f.PCurr = u16(dv, KESR + 30); f.ICh = u16(dv, KESR + 32); f.IOut = u16(dv, KESR + 34);
    f.TempInt = u8(b, KESR + 36); f.TempBat = u8(b, KESR + 37);
    f.PwrW = u16(dv, KESR + 38); f.PwrKW = u16(dv, KESR + 40);
    f.IExts0 = u16(dv, KESR + 42); f.IExts1 = u16(dv, KESR + 44);
    f.PExts0 = u16(dv, KESR + 46); f.PExts1 = u16(dv, KESR + 48);
    f.Mode = u8(b, KESR + 50); f.Sign = u8(b, KESR + 51); f.MPP = u8(b, KESR + 52);
    f.Windspeed = u16(dv, KESR + 53);
    var off = 55, i;
    f.UakbCell = []; for (i = 0; i < 32; i++) { f.UakbCell.push(u16(dv, KESR + off + 2 * i)); } off += 64;
    f.TakbCell = []; for (i = 0; i < 32; i++) { f.TakbCell.push(u8(b, KESR + off + i)); } off += 32;
    f.QCell = []; for (i = 0; i < 32; i++) { f.QCell.push(u16(dv, KESR + off + 2 * i)); } off += 64;
    f.ErrCells = []; for (i = 0; i < 32; i++) { f.ErrCells.push(u8(b, KESR + off + i)); } off += 32;
    f.IAkbMPPT = []; for (i = 0; i < 16; i++) { f.IAkbMPPT.push(u16(dv, KESR + off + 2 * i)); } off += 32;
    f.ModeMPPT = []; for (i = 0; i < 16; i++) { f.ModeMPPT.push(u8(b, KESR + off + i)); } off += 16;
    f.TakbMPPT = []; for (i = 0; i < 16; i++) { f.TakbMPPT.push(u8(b, KESR + off + i)); } off += 16;
    f.ErrMPPT = []; for (i = 0; i < 16; i++) { f.ErrMPPT.push(u8(b, KESR + off + i)); }
    f.Settings = []; for (i = 0; i < 40; i++) { f.Settings.push(u16(dv, KESS + 5 + 2 * i)); }
    return f;
  }

  // ---- форматирование ----
  function f1(x) { return (x / 100).toFixed(1); }
  function temp(x) { return (x - 50); } // °C (x−50)
  function modeStr(f) { var m = charField(f.Mode), s = charField(f.Sign); return (m ? m : "—") + (s ? s : ""); }
  function mpptModeStr(m) { return ["цикл", "дозаряд", "буфер1", "буфер2"][m] || ("?" + m); }
  function energy(f) { return f.PwrKW + "." + pad3(f.PwrW); } // кВт·ч
  function hhmmss(ts) { if (!ts) { return "—"; } var d = new Date(ts * 1000), p = function (x) { return (x < 10 ? "0" : "") + x; }; return p(d.getHours()) + ":" + p(d.getMinutes()) + ":" + p(d.getSeconds()); }
  function esc(s) { return String(s).replace(/[&<>"]/g, function (c) { return ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;" })[c]; }); }
  function relBits(c) { return [(c & 1) ? 1 : 0, (c & 2) ? 1 : 0, (c & 4) ? 1 : 0]; }
  function errBits(v) { var out = [], n = [t("kes.e.overv"), t("kes.e.overbat"), t("kes.e.deepdis"), t("kes.e.short"), t("kes.e.overtemp"), t("kes.e.i2c")]; for (var i = 0; i < n.length; i++) { if (v & (1 << i)) { out.push(n[i]); } } return out; }

  // ---- Панель (layout легаси-монитора КЭС, по эталонному скриншоту) ----
  function vcell(v) { return '<span class="kesp-val">' + esc(v) + '</span>'; }
  function vcellDT(label, v) { return '<span class="kesp-val"><span class="kesp-dtl">' + esc(label) + '</span>' + esc(v) + '</span>'; }
  function renderPanel(f) {
    var el = document.getElementById("kesPanel"); if (!el) { return; }
    var errs = errBits(f.RSErrSis);
    var status = errs.length ? errs.join(", ") : (f.I2CErr ? t("kes.i.i2c") : t("kes.nd"));
    var wind = (f.Windspeed && f.Windspeed !== 0xFFFF) ? '<span class="kesp-ico">🌬️</span>' + vcell(f.Windspeed + " " + t("kes.hz")) : "";
    // Легаси-логика «солнца»: P_PV==0 → гаснет (белое, тусклое); >0 → яркость по log(PPV).
    var sunColor;
    if (f.PPV > 0) {
      var a = 0.25 + Math.log(f.PPV / 10) / Math.log(3000);
      if (a > 1) { a = 1; } if (a < 0.25) { a = 0.25; }
      sunColor = "rgba(255,196,0," + a.toFixed(2) + ")";
    } else {
      sunColor = "rgba(255,255,255,0.4)"; // нет выработки — погасло
    }
    var sunStyle = ' style="color:' + sunColor + '"';
    // 3 индикатора реле (биты RelayC: 1/2/3).
    var rel = "";
    [0, 1, 2].forEach(function (i) {
      var on = ((f.RelayC >> i) & 1) === 1;
      rel += '<span class="kesp-rel' + (on ? " on" : "") + '" title="' + esc(t("kes.rele" + (i + 1))) + '">' + (on ? '<i class="glyphicons glyphicons-power"></i>' : "") + "</span>";
    });
    el.innerHTML =
      // Верхняя полоса: солнце · индикаторы реле · режим · время · мощность · селектор · ID
      '<div class="kesp-top">' +
        '<span class="kesp-sun"><i class="glyphicons glyphicons-sun"' + sunStyle + '></i></span>' +
        rel +
        '<span class="kesp-mode">' + esc(modeStr(f)) + '</span>' +
        '<span class="kesp-time">' + hhmmss(f.LastSeen) + '</span>' +
        '<span class="kesp-pow">' + f.PPV + " " + esc(t("kes.w")) + '</span>' +
        '<span class="kesp-sel">▾</span>' +
        '<span class="kesp-id"><b>MPPT 1</b> ID:' + f.Serial + '</span>' +
      '</div>' +
      '<div class="kesp-body">' +
        '<div class="kesp-cell kesp-pv" title="' + esc(t("kes.lbl_pv")) + '"><i class="glyphicons glyphicons-sun kesp-ico"' + sunStyle + '></i><span class="kesp-set">' +
          vcell(f1(f.VcPV) + " " + t("kes.v")) + vcell(f1(f.IcPV) + " " + t("kes.a")) + vcell(f.PPV + " " + t("kes.w")) + '</span></div>' +
        '<div class="kesp-cell kesp-bat" title="' + esc(t("kes.lbl_bat")) + '"><i class="glyphicons glyphicons-battery-full kesp-ico c-bat"></i><span class="kesp-set">' +
          vcell(f1(f.VBat) + " " + t("kes.v")) + vcell(f1(f.ICh) + " " + t("kes.a")) + vcell(f.POut + " " + t("kes.w")) + '</span></div>' +
        '<div class="kesp-cell kesp-out" title="' + esc(t("kes.lbl_cs")) + '"><i class="glyphicons glyphicons-record kesp-ico c-rec"></i><span class="kesp-set">' +
          vcellDT(t("kes.dt1"), f1(f.IExts0) + " " + t("kes.a")) + vcellDT(t("kes.dt2"), f1(f.IExts1) + " " + t("kes.a")) + '</span></div>' +
        '<div class="kesp-tint" title="' + esc(t("kes.lbl_temp")) + '"><i class="glyphicons glyphicons-temperature kesp-ico c-temp"></i><span class="kesp-set">' + vcell(temp(f.TempInt) + " " + t("kes.c")) +
          '<span class="kesp-leaf" title="' + esc(t("kes.lbl_energy")) + '"><i class="glyphicons glyphicons-leaf"></i> ' + esc(energy(f) + " " + t("kes.kwh")) + '</span></span></div>' +
        '<div class="kesp-tbat" title="' + esc(t("kes.lbl_temp")) + '"><i class="glyphicons glyphicons-temperature kesp-ico c-temp"></i><span class="kesp-set">' + vcell(temp(f.TempBat) + " " + t("kes.c")) + '</span></div>' +
        '<div class="kesp-st' + (errs.length ? " bad" : "") + '" title="' + esc(t("kes.errors")) + '"><i class="glyphicons glyphicons-rotate-right kesp-ico c-st"></i><span class="kesp-set">' + wind +
          '<span class="kesp-status">' + esc(status) + '</span></span></div>' +
      '</div>';
  }

  // ---- Информация ----
  function renderInfo(f) {
    var el = document.getElementById("kesInfo"); if (!el) { return; }
    if (curSub === "system") {
      var rows = [
        [t("kes.i.serial"), f.Serial], [t("kes.i.device"), f.Device], [t("kes.i.pow"), f.Pow],
        [t("kes.i.fw"), (f.FW & 0x0F) + "." + (f.FW >> 5)], [t("kes.i.modelpow"), (f.ModelPow * 100) + " " + t("kes.w")],
        [t("kes.i.modelvolt"), (f.ModelVolt * 12) + " " + t("kes.v")], [t("kes.i.pic"), f.Pic], [t("kes.i.net"), f.Net],
        [t("kes.i.lastseen"), new Date(f.LastSeen * 1000).toLocaleString(navigator.language || undefined, { hour12: false })],
        [t("kes.i.rse"), f.RSErrSis], [t("kes.i.i2c"), f.I2CErr],
        [t("kes.i.iexts0"), f1(f.IExts0) + " " + t("kes.a")], [t("kes.i.iexts1"), f1(f.IExts1) + " " + t("kes.a")],
        [t("kes.i.pexts0"), f.PExts0 + " " + t("kes.w")], [t("kes.i.pexts1"), f.PExts1 + " " + t("kes.w")],
        [t("kes.i.wind"), (f.Windspeed === 0xFFFF ? "—" : f.Windspeed + " " + t("kes.hz"))]
      ];
      el.innerHTML = '<table class="tbl kes-sys"><tbody>' + rows.map(function (r) {
        return '<tr><th>' + esc(r[0]) + '</th><td>' + esc(r[1]) + '</td></tr>';
      }).join("") + '</tbody></table>';
    } else {
      var main = [
        [t("kes.i.model"), String(f.ModelPow)],
        [t("kes.i.fw"), (f.FW & 0x0F) + "." + (f.FW >> 5)],
        [t("kes.i.serial"), "#" + f.Serial],
        [t("kes.lbl_mode"), modeStr(f)],
        [t("kes.lbl_pv"), f1(f.VcPV) + " " + t("kes.v") + " · " + f1(f.IcPV) + " " + t("kes.a") + " · " + f.PPV + " " + t("kes.w")],
        [t("kes.lbl_bat"), f1(f.VBat) + " " + t("kes.v") + " · " + f1(f.ICh) + " " + t("kes.a") + " · " + f.PCurr + " " + t("kes.w")],
        [t("kes.lbl_out"), f1(f.IOut) + " " + t("kes.a") + " · " + f.PLoad + " " + t("kes.w")],
        [t("kes.lbl_temp"), temp(f.TempInt) + t("kes.c") + " / " + temp(f.TempBat) + t("kes.c")],
        [t("kes.lbl_energy"), energy(f) + " " + t("kes.kwh")]
      ];
      el.innerHTML = '<table class="tbl kes-main"><tbody>' + main.map(function (r) {
        return '<tr><th>' + esc(r[0]) + '</th><td>' + esc(r[1]) + '</td></tr>';
      }).join("") + '</tbody></table>';
    }
  }

  // ---- Управление: реле кнопками (как на МАП) ----
  function renderControl(f) {
    var el = document.getElementById("kesControl"); if (!el) { return; }
    el.innerHTML = '<div class="kesctl-sec"><h3>' + esc(t("kes.lbl_relay")) + '</h3>' +
      '<div class="ctl-cmdrow" id="kesRelays"></div></div>';
    var box = el.querySelector("#kesRelays");
    [0, 1, 2].forEach(function (i) {
      var on = ((f.RelayC >> i) & 1) === 1;
      var b = document.createElement("button");
      b.type = "button";
      b.className = "ctl-toggle" + (on ? " act-off" : "");
      b.textContent = (on ? t("ctl.relay_off") : t("ctl.relay_on")) + " " + (i + 1);
      b.onclick = function () {
        var bit = 1 << i;
        postRelay(on ? (f.RelayC & ~bit) : (f.RelayC | bit));
      };
      box.appendChild(b);
    });
  }
  function postRelay(val) {
    var data = ("0" + val.toString(16)).slice(-2);
    fetch("api/kes/write", {
      method: "POST", headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ id: ID, gen: GEN, ops: [{ op: "reg", addr: "E3D", data: data }] })
    })
      .then(function (r) { return r.json(); })
      .then(function (j) { if (j && j.ok) { showErr(""); } else { showErr(t("kes.ctl_err") + " " + ((j && (j.error || j.code)) || "")); } })
      .catch(function () { showErr(t("kes.connection_lost")); });
  }

  function render() {
    if (!lastF) { return; }
    if (curTab === "monitor") { renderPanel(lastF); }
    else if (curTab === "info") { renderInfo(lastF); }
    else if (curTab === "control") { renderControl(lastF); }
  }

  function showErr(m) { if (!errBox) { return; } if (m) { errBox.textContent = m; errBox.hidden = false; } else { errBox.hidden = true; } }

  // Потеря устройства (404, id/gen устарели): найти КЭС по серийнику в списке
  // устройств и перейти на актуальный id/gen; иначе — на главную (как МАП).
  function relocateKes() {
    var serial = (lastF && String(lastF.Serial)) || String(window.KES_SERIAL || "");
    var tries = 0, max = 60; // ~60 c: после старта устройства обнаруживаются не сразу
    (function attempt() {
      fetch("api/kes/devices", { cache: "no-store" }).then(function (r) { return r.json(); }).then(function (list) {
        var d = null; list = list || [];
        for (var i = 0; i < list.length; i++) { if (serial && String(list[i].serial) === serial) { d = list[i]; break; } }
        if (!d && list.length === 1) { d = list[0]; }
        if (d) { window.location.href = "kes?id=" + encodeURIComponent(d.id) + "&gen=" + encodeURIComponent(d.gen) + "&serial=" + encodeURIComponent(serial); return; }
        if (++tries < max) { setTimeout(attempt, 1000); } else { window.location.href = "/"; }
      }).catch(function () { if (++tries < max) { setTimeout(attempt, 1000); } else { window.location.href = "/"; } });
    })();
  }

  function tick(force) {
    if (pause && pause.checked && !force) { return; }
    inflight = new AbortController();
    fetch("api/kes/raw?id=" + encodeURIComponent(ID) + "&gen=" + encodeURIComponent(GEN), { cache: "no-store", signal: inflight.signal })
      .then(function (r) { if (r.status === 404) { throw { gone: true }; } if (r.status === 401) { throw { auth: true }; } if (!r.ok) { throw new Error("HTTP " + r.status); } return r.arrayBuffer(); })
      .then(function (buf) { showErr(""); lastF = parse(buf); render(); })
      .catch(function (e) { if (e && e.auth) { window.location.href = "login"; return; } if (e && e.gone) { relocateKes(); return; } if (!(e && e.name === "AbortError")) { showErr(t("kes.connection_lost")); } });
  }

  var curSub = "main";
  function initTabs() {
    var tabs = document.querySelectorAll(".map-tab");
    var panes = { monitor: document.getElementById("tab-monitor"), info: document.getElementById("tab-info"), control: document.getElementById("tab-control") };
    function show(name) {
      curTab = name;
      Array.prototype.forEach.call(tabs, function (b) { b.classList.toggle("active", b.getAttribute("data-tab") === name); });
      Object.keys(panes).forEach(function (k) { if (panes[k]) { panes[k].hidden = (k !== name); } });
      render();
    }
    Array.prototype.forEach.call(tabs, function (b) { b.addEventListener("click", function () { show(b.getAttribute("data-tab")); }); });
    Array.prototype.forEach.call(document.querySelectorAll(".mi-subtab"), function (b) {
      b.addEventListener("click", function () {
        curSub = b.getAttribute("data-sub");
        Array.prototype.forEach.call(document.querySelectorAll(".mi-subtab"), function (x) { x.classList.toggle("active", x === b); });
        render();
      });
    });
    show("monitor");
  }
  initTabs();

  function start() { if (timer) { return; } tick(); timer = setInterval(tick, INTERVAL); }
  function stop() { if (timer) { clearInterval(timer); timer = null; } if (inflight) { inflight.abort(); inflight = null; } }
  if (pause) { pause.addEventListener("change", function () { if (pause.checked) { stop(); } else { start(); } }); }

  window.KES_FORCE_REFRESH = function () { tick(true); };
  start();
})();
