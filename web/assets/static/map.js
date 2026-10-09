(function () {
  "use strict";
  var I18N = window.I18N || {};
  var FIELDS = window.MAP_FIELDS || [];
  var ID = window.MAP_ID;
  var SERIAL = window.MAP_SERIAL || "";
  var pre = document.getElementById("mapJson");
  var panel = document.getElementById("mpPanel");
  var errBox = document.getElementById("mapErr");
  var pause = document.getElementById("mapPause");
  if (!pre || ID == null) { return; }

  var INTERVAL = 1000, timer = null, inflight = null;

  // Тексты режима работы (из locale старой Малины, по _MODE 0x400).
  var MODE_TEXT = [
    "МАП выключен и нет сети на входе",
    "МАП выключен. Сеть на входе",
    "МАП включен. Генерация от АКБ. Нет сети.",
    "МАП включен и транслирует сеть",
    "МАП включен. Трансляция + заряд"
  ];
  var POWER_KW = [1.3, 1.5, 2, 3, 4.5, 6, 9, 12, 15, 18, 24, 36];
  var VOLTAGE = [12, 24, 48, 96];

  function t(k) { return I18N[k] || k; }
  function u16(dv, o) { return dv.getUint16(o, true); }
  function u32(dv, o) { return dv.getUint32(o, true); }
  function str(b, off, len) { var s = ""; for (var i = off; i < off + len && b[i]; i++) { s += String.fromCharCode(b[i]); } return s; }
  function parse(buf) {
    var b = new Uint8Array(buf), dv = new DataView(buf);
    if (b.length < 64 || u32(dv, 0) !== 0x4441504d) { throw new Error("bad payload"); }
    var n = u16(dv, 6), blen = Math.ceil(n / 8), vbase = 64 + blen;
    var obj = { port: str(b, 8, 32), model: b[40] ? String.fromCharCode(b[40]) : "",
      protocol: b[41] === 1 ? "modbus" : "microart", unit: b[42],
      gen: Number(dv.getBigUint64(48, true)), last_seen: Number(dv.getBigUint64(56, true)), fields: {} };
    for (var i = 0; i < n; i++) { if (!(b[64 + (i >> 3)] & (1 << (i % 8)))) { continue; } obj.fields[FIELDS[i] || ("field_" + i)] = u32(dv, vbase + i * 4); }
    return obj;
  }
  function num(f, k) { var v = f[k]; return (typeof v === "number") ? v : undefined; }
  function fmt(x, d) { return (typeof x === "number" && isFinite(x)) ? x.toFixed(d === undefined ? 1 : d) : "—"; }
  function clamp(v, lo, hi) { return Math.max(lo, Math.min(hi, v)); }

  function compute(f) {
    var v = {};
    var unet = num(f, "UNET"), uout = num(f, "UOUTmed");
    v.Uin = (unet === undefined) ? undefined : (unet === 0 ? 0 : unet + 100);
    v.Uout = (uout === undefined) ? undefined : (uout === 0 ? 0 : uout + 100);
    var inet = num(f, "INET"), inet16 = num(f, "INET_16_4");
    if (inet16 !== undefined) { v.Iin = inet16 / ((inet !== undefined && inet >= 16) ? 4 : 16); }
    var vh = num(f, "UAcc_med_VH"), vl = num(f, "UAcc_med_VL");
    if (vh !== undefined && vl !== undefined) { v.Uacc = (vh * 256 + vl) / 10; }
    var il = num(f, "IAcc_med_A_u16_L"), ih = num(f, "IAcc_med_A_u16_H");
    if (il !== undefined && ih !== undefined) { v.Iacc = -(il + ih * 256) / 16; } // «−» = заряд/потребление (как legacy)
    var sum = 0, cnt = 0;
    for (var i = 0; i < 16; i++) { var l = f["I_Akb_MPPT_L" + i], h = f["I_Akb_MPPT_H" + i];
      if (typeof l === "number" && typeof h === "number") { var c = (l + (h & 0x7f) * 256) / 16; if (c !== 0) { sum += c; cnt++; } } }
    if (cnt) { v.Imppt = sum / cnt; }
    function counter(base) { var L = f[base + "_L"], H = f[base + "_H"], HH = f[base + "_HH"];
      if (typeof L !== "number" || typeof H !== "number" || typeof HH !== "number") { return undefined; }
      return (L + H * 256 + HH * 65536) / 100; }
    v.Enet = counter("M_POWhourNET"); v.Eacc = counter("M_POWhourMAP"); v.Echg = counter("M_POWhourMAPCharge");
    v.Tacc = (typeof f["Temp_Grad0"] === "number") ? f["Temp_Grad0"] - 50 : undefined;
    v.Ttor = (typeof f["Temp_Grad1"] === "number") ? f["Temp_Grad1"] - 50 : undefined;
    v.Ttr = (typeof f["Temp_Grad2"] === "number") ? f["Temp_Grad2"] - 50 : undefined;
    v.Fnet = num(f, "TFNET") ? 6250 / f["TFNET"] : undefined;
    v.Fout = num(f, "ThFMAP") ? 6250 / f["ThFMAP"] : undefined;
    var sh = (typeof f["UACC"] === "number") ? (f["UACC"] & 0x1f) : 2;
    if (typeof f["E_LCD_UAccChBUF"] === "number") { v.Ubuf = (f["E_LCD_UAccChBUF"] << sh) / 10; }
    if (typeof f["E_LCD_UAccChMax"] === "number") { v.Uchg = (f["E_LCD_UAccChMax"] << sh) / 10; }
    // Максимальная мощность МАП из класса мощности (_POW) и напряжения (_UACC).
    var pow = num(f, "POW"), uacc = num(f, "UACC");
    v.Pmax = (pow !== undefined && POWER_KW[pow] !== undefined) ? POWER_KW[pow] * 1000 : 20000;
    v.PbatMax = v.Pmax * 0.68;
    v.Imax = (pow !== undefined && uacc !== undefined && VOLTAGE[uacc]) ? POWER_KW[pow] * 1500 / VOLTAGE[uacc] : 50;
    v.Pnet = (v.Uin !== undefined && v.Iin !== undefined) ? v.Uin * v.Iin : undefined;
    if (v.Pnet !== undefined && f["PNET_Sign_P"] === 0) { v.Pnet = -v.Pnet; }
    // P АКБ = Uacc*Iacc (Iacc уже со знаком: «−» = потребление), как legacy-api.
    v.Pbat = (v.Uacc !== undefined && v.Iacc !== undefined) ? v.Uacc * v.Iacc : undefined;
    v.Mode = num(f, "MODE_RAM");
    var errs = ["RSErrSis", "RSErrJobM", "RSErrJob", "RSWarning", "RSErrDop"], eb = 0;
    errs.forEach(function (k) { if (typeof f[k] === "number" && f[k] !== 0) { eb++; } });
    v.ErrCount = eb;
    return v;
  }

  // Гейдж-подкова с плавным градиентом (зелёный слева -> красный справа) и стрелкой.
  function gaugeIdx(idx, frac) {
    frac = clamp(frac, 0, 1);
    var cx = 60, cy = 62, R = 48;
    var th = Math.PI * (1 - frac);
    var nx = cx + (R - 7) * Math.cos(th), ny = cy - (R - 7) * Math.sin(th);
    return '<defs><linearGradient id="gg' + idx + '" x1="12" y1="0" x2="108" y2="0">' +
      '<stop offset="0" stop-color="#0d5637"/><stop offset="0.5" stop-color="#fec678"/><stop offset="1" stop-color="#8a2b2b"/>' +
      '</linearGradient></defs>' +
      '<path d="M12 62 A48 48 0 0 1 108 62" fill="none" stroke="url(#gg' + idx + ')" stroke-width="9"/>' +
      '<line x1="60" y1="62" x2="' + nx.toFixed(1) + '" y2="' + ny.toFixed(1) + '" stroke="#0d5637" stroke-width="3" stroke-linecap="round"/>' +
      '<circle cx="60" cy="62" r="4.5" fill="#0d5637"/>';
  }
  // pos >= 0: шкала 0..max (слева направо). bipolar: 0 в центре (внизу/вверху).
  function gauge(gi, label, val, unit, max, bipolar) {
    var frac = 0;
    if (typeof val === "number" && isFinite(val) && max > 0) {
      frac = bipolar ? clamp((val / max + 1) / 2, 0, 1) : clamp(val / max, 0, 1);
    }
    return '<div class="mp-gauge"><svg viewBox="0 0 120 74" width="132" height="80">' + gaugeIdx(gi, frac) +
      '<text x="60" y="52" text-anchor="middle" class="mp-gtv">' + fmt(val, 1) + '<tspan class="mp-gtu">' + unit + '</tspan></text>' +
      '</svg><div class="mp-gauge-lbl">' + label + '</div></div>';
  }

  // Двуполярная полоса (0 в центре).
  function barBipolar(val, max, unit) {
    var fr = (typeof val === "number" && isFinite(val) && max > 0) ? clamp(Math.abs(val) / max, 0, 1) : 0;
    var col = (typeof val === "number" && val < 0) ? "#8a2b2b" : "#0d5637";
    var w = (fr * 50).toFixed(1);
    var left = (typeof val === "number" && val < 0) ? '<i class="mp-fill left" style="width:' + w + '%;background:' + col + '"></i>' : '<i class="mp-fill right" style="width:' + w + '%;background:' + col + '"></i>';
    return '<div class="mp-bar"><i class="mp-zero"></i>' + left + '<span class="mp-bar-val">' + fmt(val, 0) + ' ' + unit + '</span></div>';
  }

  function tile(label, value) { return '<div class="mp-tile"><div class="mp-tile-v">' + value + '</div><div class="mp-tile-l">' + label + '</div></div>'; }

  // Составной режим работы — точный алгоритм mapd (compound_mode из дизасма).
  // Входы: MODE_RAM(0x400), LCD_NETAlg(0x16B), flag_NETON_ECO(0x585),
  //        LCD_NetUpECO(0x13C), LCD_NetUpLoad(0x13B), UNET(0x422), Pmax_On(0x58C).
  var MAP_MODE_RU = [
    "МАП выключен и нет сети на входе",
    "МАП выключен. Сеть на входе",
    "МАП включен. Генерация от АКБ. Нет сети.",
    "МАП включен и транслирует сеть",
    "МАП включен. Трансляция + заряд",
    "", "", "", "", "",
    "Принудительная генерация",
    "Тарифная сеть. максимальный тариф. принудительная генерация",
    "Тарифная сеть. минимальный тариф",
    "Трансляция + эко-подкачка",
    "Трансляция + продажа в сеть",
    "Ожидание внешнего полного заряда",
    "Тарифная сеть. трансляция+эко-подкачка",
    "Тарифная сеть. трансляция+продажа в сеть",
    "Подкачка Pmax"
  ];
  function modeCode(f) {
    var M = f.MODE_RAM, N = f.LCD_NETAlg, E = f.flag_NETON_ECO, U = f.LCD_NetUpECO,
        L = f.LCD_NetUpLoad, P = f.Pmax_On, V = (f.UNET ? f.UNET + 100 : 0);
    if (typeof M !== "number") { return undefined; }
    if (typeof E !== "number") { E = 0; }
    if (typeof P !== "number") { P = 0; }
    if (M <= 1) { return M; }
    if (M > 3) { return M; }
    if (U === 0) {
      if (L === 1 && (P & 2) && V > 100) { return 18; }
      if (M === 2) {
        if (V > 100) {
          if (N === 2) { return 10; }
          if (N === 3 && (E & 2) === 0) { return 11; }
          if (N === 3) { return 12; }
        }
        return 2;
      }
      return 3; // M==3, U==0
    }
    if (M === 3) {
      if (U === 1) {
        if (N === 2) { return (E & 1) ? 15 : 13; }
        if (N === 3) { return (E & 2) ? 12 : 16; }
      }
      if (U === 2) {
        if (N === 2) { return (E & 1) ? 15 : 14; }
        if (N === 3) { return (E & 2) ? 12 : 17; }
      }
    }
    return M;
  }
  function modeText(f) {
    var c = modeCode(f);
    return (c != null && MAP_MODE_RU[c]) ? MAP_MODE_RU[c] : "";
  }

  function serialOf(f) {
    if (SERIAL) { return SERIAL; }
    var s0 = f["SerialNum0"], s1 = f["SerialNum1"], s3 = f["SerialNum3"];
    if (typeof s0 === "number" && typeof s1 === "number") {
      var n = String(s0 + s1 * 256);
      if (typeof s3 === "number" && s3) { n += String.fromCharCode(s3); }
      return n;
    }
    return "—";
  }

  function hhmmss(ts) { if (!ts) { return "--:--:--"; } var d = new Date(ts * 1000);
    return ("0" + d.getHours()).slice(-2) + ":" + ("0" + d.getMinutes()).slice(-2) + ":" + ("0" + d.getSeconds()).slice(-2); }

  function renderPanel(p) {
    if (!panel) { return; }
    var v = compute(p.fields), f = p.fields;
    var mode = modeText(f);
    var bal = (typeof v.Pnet === "number" && typeof v.Pbat === "number") ? (v.Pnet + v.Pbat) : undefined;
    var gi = 0, gauges = gauge(gi++, "Uвх, В", v.Uin, "В", 300, false) + gauge(gi++, "Uвых, В", v.Uout, "В", 300, false) +
      gauge(gi++, "Iвх, А", v.Iin, "А", v.Imax, true) + gauge(gi++, "Uакб, В", v.Uacc, "В", 60, false) +
      gauge(gi++, "Iакб-мап, А", v.Iacc, "А", v.Imax, true);
    if (typeof v.Imppt === "number") { gauges += gauge(gi++, "Imppt, А", v.Imppt, "А", v.Imax, true); }
    panel.innerHTML =
      '<div class="mp-head"><span class="mp-head-status">' + mode + '</span>' +
        '<span class="mp-head-right"><b class="mp-head-time">' + hhmmss(p.last_seen) + '</b>' +
        '<span class="mp-head-id">МАП ID:' + serialOf(f) + '</span></span></div>' +
      '<div class="mp-cols">' +
        '<div class="mp-block mp-ac"><div class="mp-block-top"><span class="mp-ico">◉</span>' +
          '<span class="mp-freq">' + fmt(v.Fnet, 1) + ' Гц</span><span class="mp-cos">cos φ = —</span></div>' +
          barBipolar(v.Pnet, v.Pmax, 'Вт') +
          '<div class="mp-block-bot"><span>🌡 <b>' + fmt(v.Tacc, 0) + '</b>°C</span><span>🔌 <b>' + fmt(v.Enet, 0) + '</b> кВт·ч</span><span>🔋 <b>' + fmt(v.Eacc, 0) + '</b> кВт·ч</span></div></div>' +
        '<div class="mp-block mp-bat"><div class="mp-block-top"><span class="mp-ico">🔋</span>' +
          '<span class="mp-freq">' + fmt(v.Fout, 1) + ' Гц</span></div>' +
          barBipolar(v.Pbat, v.PbatMax, 'Вт') +
          '<div class="mp-block-bot"><span>🌡 <b>' + fmt(v.Ttr, 0) + '</b>°C</span><span>🔌 <b>' + fmt(v.Echg, 0) + '</b> кВт·ч</span></div></div>' +
        (function(){ if (typeof v.Imppt !== "number") { return ""; }
          var pm = (typeof v.Uacc === "number") ? v.Uacc * v.Imppt : undefined;
          return '<div class="mp-block mp-mppt"><div class="mp-block-top"><span class="mp-ico">☀</span><span class="mp-freq">КЭС</span></div>' +
            '<div class="mp-mppt-row"><span>I mppt: <b>' + fmt(v.Imppt, 1) + '</b> А</span>' +
            '<span>P mppt: <b>' + fmt(pm, 0) + '</b> Вт</span></div></div>'; })() +
      '</div>' +
      '<div class="mp-gauges">' + gauges + '</div>' +
      '<div class="mp-grid">' +
        tile("Сеть, кВт·ч", fmt(v.Enet, 2)) + tile("АКБ, кВт·ч", fmt(v.Eacc, 2)) + tile("Заряд, кВт·ч", fmt(v.Echg, 2)) +
        tile("P сети, Вт", fmt(v.Pnet, 0)) + tile("P АКБ, Вт", fmt(v.Pbat, 0)) + tile("Баланс, Вт", fmt(bal, 0)) +
        tile("T АКБ, °C", fmt(v.Tacc, 0)) + tile("T тор, °C", fmt(v.Ttor, 0)) + tile("T транз., °C", fmt(v.Ttr, 0)) +
        tile("Ubuf(t°), В", fmt(v.Ubuf, 1)) + tile("Uchg(t°), В", fmt(v.Uchg, 1)) +
      '</div>' +
      '<div class="mp-foot"><span>Ubuf(t°): <b>' + fmt(v.Ubuf, 1) + '</b> В</span><span>Uchg(t°): <b>' + fmt(v.Uchg, 1) + '</b> В</span>' +
        '<span class="mp-foot-err">Ошибки <b>' + v.ErrCount + '</b></span></div>';
  }

  function showErr(m) { if (!errBox) { return; } if (m) { errBox.textContent = m; errBox.hidden = false; } else { errBox.hidden = true; } }

  function tick() {
    if (pause && pause.checked) { return; }
    inflight = new AbortController();
    fetch("/api/map/raw?id=" + encodeURIComponent(ID), { cache: "no-store", signal: inflight.signal })
      .then(function (r) { if (r.status === 404) { throw { gone: true }; } if (r.status === 401) { throw { auth: true }; } if (!r.ok) { throw new Error("HTTP " + r.status); } return r.arrayBuffer(); })
      .then(function (buf) { showErr(""); var p = parse(buf); pre.textContent = JSON.stringify(p); renderPanel(p); postToPanel2(buf); })
      .catch(function (e) { if (e && e.auth) { window.location.href = "/login"; return; } if (e && e.gone) { showErr(t("map.gone")); } else if (!(e && e.name === "AbortError")) { showErr(t("map.connection_lost")); } });
  }
  function start() { if (timer) { return; } tick(); timer = setInterval(tick, INTERVAL); }
  function stop() { if (timer) { clearInterval(timer); timer = null; } if (inflight) { inflight.abort(); inflight = null; } }
  if (pause) { pause.addEventListener("change", function () { if (pause.checked) { stop(); } else { start(); } }); }

  // Закладки: всегда открывается «Монитор»; JSON-мемо вне закладок.
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

  // Передать бинарный массив (из нового API) и событие обновления в панель 2.
  // Панель 2 изолирована и сама данные не запрашивает; список полей
  // (window.MAP_FIELDS) она берёт у родителя напрямую (same-origin).
  function postToPanel2(buf) {
    var f = document.getElementById("panel2frame");
    if (!f || !f.contentWindow) { return; }
    try { f.contentWindow.postMessage({ type: "map-raw", id: ID, ts: Date.now(), buf: buf.slice(0) }, location.origin); } catch (err) { /* ignore */ }
  }

  // Авто-высота изолированных фреймов (same-origin) — без лишней пустоты.
  function sizeFrames() {
    var frames = document.querySelectorAll(".legacy-frame");
    Array.prototype.forEach.call(frames, function (fr) {
      try {
        var d = fr.contentDocument;
        if (d && d.documentElement) { fr.style.height = Math.max(160, d.documentElement.scrollHeight) + "px"; }
      } catch (err) { /* ignore */ }
    });
  }
  Array.prototype.forEach.call(document.querySelectorAll(".legacy-frame"), function (fr) { fr.addEventListener("load", sizeFrames); });
  setInterval(sizeFrames, 1000);

  start();
})();
