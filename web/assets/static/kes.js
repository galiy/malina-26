// Страница КЭС (заготовка). Vanilla JS. Полностью по аналогии с map.js, но без
// приборной панели: пуллим /api/kes/raw?id&gen раз в 1с, парсим бинарный payload
// слота (KESE+KESR+KESS, 1536 Б) ПО КОНТРАКТУ и выводим распакованные поля в
// чёрное мемо (JSON). Распаковка зеркалит web/internal/web/kesmem.go (parseKesPayload).
(function () {
  "use strict";
  var I18N = window.I18N || {};
  var ID = window.KES_ID;
  var GEN = window.KES_GEN;
  var pre = document.getElementById("kesJson");
  var errBox = document.getElementById("kesErr");
  var pause = document.getElementById("kesPause");
  if (!pre || ID == null) { return; }

  var INTERVAL = 1000, timer = null, inflight = null;

  var KESE = 0, KESR = 512, KESS = 1024, SLOT = 1536;
  function u8(b, o) { return b[o]; }
  function u16(dv, o) { return dv.getUint16(o, true); }
  function u32(dv, o) { return dv.getUint32(o, true); }
  function u64(dv, o) { return Number(dv.getBigUint64(o, true)); }
  function magic(b, o) { return String.fromCharCode(b[o], b[o + 1], b[o + 2], b[o + 3]); }
  // Строка одного символа (Mode/Sign/MPP): печатный символ как есть, вне диапазона — 'U'.
  function charField(v) {
    if (v === 0) { return ""; }
    if (v > 0x2A && v <= 0x7C) { return String.fromCharCode(v); }
    return "U";
  }
  function pad3(n) { n = String(n); while (n.length < 3) { n = "0" + n; } return n; }

  // Распаковка payload слота КЭС (зеркало kesmem.go parseKesPayload).
  function parse(buf) {
    var b = new Uint8Array(buf), dv = new DataView(buf);
    if (b.length < SLOT || magic(b, KESE) !== "KESE" || magic(b, KESR) !== "KESR" || magic(b, KESS) !== "KESS") {
      throw new Error("bad payload");
    }
    if (b[KESE + 4] !== 1 || b[KESR + 4] !== 1 || b[KESS + 4] !== 1) { throw new Error("bad version"); }
    var f = {};
    // KESE.
    f.Serial = u16(dv, KESE + 6); f.Device = u8(b, KESE + 8); f.Pow = u8(b, KESE + 9);
    f.FW = u8(b, KESE + 10); f.ModelPow = u8(b, KESE + 11); f.ModelVolt = u8(b, KESE + 12);
    f.Pic = u8(b, KESE + 13); f.Net = u8(b, KESE + 14);
    // KESR.
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
    f.SignC0 = 0; f.SignC1 = 0; // в контракт KESR не публикуются
    // BMS и параллельные MPPT (смещение 55 от начала KESR, как в kesmem.go).
    var off = 55, i;
    f.UakbCell = []; for (i = 0; i < 32; i++) { f.UakbCell.push(u16(dv, KESR + off + 2 * i)); }
    off += 64;
    f.TakbCell = []; for (i = 0; i < 32; i++) { f.TakbCell.push(u8(b, KESR + off + i)); }
    off += 32;
    f.QCell = []; for (i = 0; i < 32; i++) { f.QCell.push(u16(dv, KESR + off + 2 * i)); }
    off += 64;
    f.ErrCells = []; for (i = 0; i < 32; i++) { f.ErrCells.push(u8(b, KESR + off + i)); }
    off += 32;
    f.IAkbMPPT = []; for (i = 0; i < 16; i++) { f.IAkbMPPT.push(u16(dv, KESR + off + 2 * i)); }
    off += 32;
    f.ModeMPPT = []; for (i = 0; i < 16; i++) { f.ModeMPPT.push(u8(b, KESR + off + i)); }
    off += 16;
    f.TakbMPPT = []; for (i = 0; i < 16; i++) { f.TakbMPPT.push(u8(b, KESR + off + i)); }
    off += 16;
    f.ErrMPPT = []; for (i = 0; i < 16; i++) { f.ErrMPPT.push(u8(b, KESR + off + i)); }
    // KESS (настройки).
    f.Settings = []; for (i = 0; i < 40; i++) { f.Settings.push(u16(dv, KESS + 5 + 2 * i)); }
    return f;
  }

  // Человекочитаемый снимок для чёрного мемо (поля из kesmem.go renderKesJSON).
  function dump(f) {
    return {
      id: ID, gen: GEN,
      serial: f.Serial, device: f.Device, pow: f.Pow,
      fw: (f.FW & 0x0F) + "." + (f.FW >> 5),
      model_pow: f.ModelPow, model_volt: f.ModelVolt, pic: f.Pic, net: f.Net,
      last_seen: f.LastSeen,
      VcPV: f.VcPV / 100, IcPV: f.IcPV / 100, VBat: f.VBat / 100,
      PPV: f.PPV, POut: f.POut, PLoad: f.PLoad, PCurr: f.PCurr,
      ICh: f.ICh / 100, IOut: f.IOut / 100,
      Pwr: f.PwrKW + "." + pad3(f.PwrW),
      TempInt: f.TempInt - 50, TempBat: f.TempBat - 50,
      RelayC: f.RelayC, RSErrSis: f.RSErrSis, I2CErr: f.I2CErr,
      Mode: charField(f.Mode), Sign: charField(f.Sign), MPP: charField(f.MPP),
      Windspeed: (f.Windspeed === 0xFFFF) ? 65535 : f.Windspeed,
      IExts0: f.IExts0 / 100, IExts1: f.IExts1 / 100,
      PExts0: f.PExts0, PExts1: f.PExts1,
      UakbCell: f.UakbCell, TakbCell: f.TakbCell, QCell: f.QCell, ErrCells: f.ErrCells,
      IAkbMPPT: f.IAkbMPPT, ModeMPPT: f.ModeMPPT, TakbMPPT: f.TakbMPPT, ErrMPPT: f.ErrMPPT,
      Settings: f.Settings
    };
  }

  function showErr(m) { if (!errBox) { return; } if (m) { errBox.textContent = m; errBox.hidden = false; } else { errBox.hidden = true; } }

  function tick(force) {
    if (pause && pause.checked && !force) { return; }
    inflight = new AbortController();
    fetch("api/kes/raw?id=" + encodeURIComponent(ID) + "&gen=" + encodeURIComponent(GEN), { cache: "no-store", signal: inflight.signal })
      .then(function (r) { if (r.status === 404) { throw { gone: true }; } if (r.status === 401) { throw { auth: true }; } if (!r.ok) { throw new Error("HTTP " + r.status); } return r.arrayBuffer(); })
      .then(function (buf) { showErr(""); pre.textContent = JSON.stringify(dump(parse(buf)), null, 2); })
      .catch(function (e) { if (e && e.auth) { window.location.href = "login"; return; } if (e && e.gone) { showErr(t("kes.gone")); } else if (!(e && e.name === "AbortError")) { showErr(t("kes.connection_lost")); } });
  }
  function t(k) { return I18N[k] || k; }
  function start() { if (timer) { return; } tick(); timer = setInterval(tick, INTERVAL); }
  function stop() { if (timer) { clearInterval(timer); timer = null; } if (inflight) { inflight.abort(); inflight = null; } }
  if (pause) { pause.addEventListener("change", function () { if (pause.checked) { stop(); } else { start(); } }); }

  // Вкладки — заглушки (как на МАП, но пустые). Переключаем active/hidden.
  function initTabs() {
    var tabs = document.querySelectorAll(".map-tab");
    var panes = { monitor: document.getElementById("tab-monitor"), info: document.getElementById("tab-info"), control: document.getElementById("tab-control") };
    function show(name) {
      Array.prototype.forEach.call(tabs, function (b) { b.classList.toggle("active", b.getAttribute("data-tab") === name); });
      Object.keys(panes).forEach(function (k) { if (panes[k]) { panes[k].hidden = (k !== name); } });
    }
    Array.prototype.forEach.call(tabs, function (b) { b.addEventListener("click", function () { show(b.getAttribute("data-tab")); }); });
    show("monitor");
  }
  initTabs();

  window.KES_FORCE_REFRESH = function () { tick(true); };
  start();
})();