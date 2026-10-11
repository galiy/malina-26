// Панель 1 (целевая). Свой стек: vanilla JS + SVG + CSS. Без jQuery/Raphael/kuma/bootstrap.
// Вид/эргономика — по оригиналу (панель 2): стрелочные гейджи, скользящие значения
// на градиентных полосах, термометры, раскладка «приборной» страницы.
// Данные — новый контракт (/api/map/raw).
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
  var hideDebug = document.getElementById("mapHideDebug");
  var hdTarget = document.getElementById("secTarget");
  var hdTransit = document.getElementById("secTransit");
  var wrap2 = document.getElementById("panel2wrap");
  var hdLegacy = document.getElementById("secLegacy");
  var wrap3 = document.getElementById("panel3wrap");
  var f2 = document.getElementById("panel2frame");
  var f3 = document.getElementById("panel3frame");
  if (!pre || !panel || ID == null) { return; }

  var INTERVAL = 1000, timer = null, inflight = null, lastP = null, infoActive = false, debugHidden = false;
  var POWER_KW = [1.3, 1.5, 2, 3, 4.5, 6, 9, 12, 15, 20, 24, 36];
  var VOLTAGE = [12, 24, 48, 96];
  // Границы гейджей — из ячеек МАП, фиксируем при первой загрузке (в цикле не пересчитываем).
  var LIM = {};

  function t(k) { return I18N[k] || k; }
  function fmt(x, d) { return (typeof x === "number" && isFinite(x)) ? x.toFixed(d === undefined ? 1 : d) : "—"; }
  function clamp(v, lo, hi) { return Math.max(lo, Math.min(hi, v)); }
  function esc(s) { return String(s).replace(/[&<>"']/g, function (c) { return { "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" }[c]; }); }
  function pad2(n) { return (n < 10 ? "0" : "") + n; }
  function hhmmss(ts) { if (!ts) { return "--:--:--"; } var d = new Date(ts * 1000); return ("0" + d.getHours()).slice(-2) + ":" + ("0" + d.getMinutes()).slice(-2) + ":" + ("0" + d.getSeconds()).slice(-2); }
  function s16(x) { x = x & 0xffff; return x >= 0x8000 ? x - 0x10000 : x; }
  function num(f, k) { var x = f[k]; return (typeof x === "number") ? x : undefined; }

  // Границы гейджей берём из ячеек МАП один раз (при первой загрузке значения).
  function fillLim(f) {
    function set(k, v) { if (LIM[k] === undefined && typeof v === "number" && isFinite(v) && v > 0) { LIM[k] = v; } }
    var d = num(f, "LCD_UNETDown"), u = num(f, "LCD_UNETUp");
    if (d !== undefined) { set("Umin", d + 100); }
    if (u !== undefined) { set("Umax", u + 100); }
    var uo = num(f, "LCD_UMAP220_NEED"); if (uo) { set("Uout", uo); }
    var np = num(f, "LCD_NetMaxPow"); if (np) { set("PnetMax", (np << (num(f, "LCD_NetMaxPow_dop") || 0)) * 100); }
    var pow = num(f, "POW"); if (pow !== undefined && POWER_KW[pow]) { set("PmapMax", POWER_KW[pow] * 1000); }
    var pa = num(f, "PowAccNom"); if (pa && pa !== 255) { set("PowAcc", pa / 100); }
    var ua = num(f, "UACC"); if (ua !== undefined && VOLTAGE[ua]) { set("Vmodel", VOLTAGE[ua]); }
    var shf = (ua !== undefined) ? (ua & 0x1f) : 2;
    var acmin = num(f, "LCD_UAccMin"); if (acmin !== undefined) { set("UakbLo", (acmin << shf) / 10); }
    var acmax = num(f, "E_LCD_UAccChMax"); if (acmax !== undefined) { set("UakbHi", (acmax << shf) / 10); }
    var kn = num(f, "LCD_MPPTNum"); if (kn) { set("ImpptMax", 60 * kn); }
  }
  function limImaxGrid() { return (LIM.PnetMax && LIM.Uout) ? LIM.PnetMax / LIM.Uout : 50; }
  function limIbatMax() { return (LIM.PowAcc && LIM.PmapMax && LIM.Vmodel) ? LIM.PowAcc * LIM.PmapMax / LIM.Vmodel : 60; }
  function limImpptMax() { return LIM.ImpptMax || 60; }

  function parse(buf) {
    var b = new Uint8Array(buf), dv = new DataView(buf);
    if (b.length < 64 || dv.getUint32(0, true) !== 0x4441504d) { throw new Error("bad payload"); }
    var n = dv.getUint16(6, true), blen = Math.ceil(n / 8), vbase = 64 + blen, f = {};
    for (var i = 0; i < n; i++) { if (!(b[64 + (i >> 3)] & (1 << (i % 8)))) { continue; } f[FIELDS[i] || ("field_" + i)] = dv.getUint32(vbase + i * 4, true); }
    return { gen: Number(dv.getBigUint64(48, true)), last_seen: Number(dv.getBigUint64(56, true)), fields: f };
  }

  // Композитный код режима — плоский список по кодам полей (зеркало mapmode.Mode).
  // Заряд (_MODE>3) уточняется ступенью из StatusCh (точное равенство): 0x00→4, 0x01→5, 0x02→6, 0x04→7, 0x08→8, иначе 9.
  function modeNum(f) {
    if (typeof f.MODE_RAM !== "number") { return undefined; }
    switch (true) {
      case f.MODE_RAM <= 1: return f.MODE_RAM;
      case f.MODE_RAM > 3 && (f.StatusCh || 0) === 0x00: return 4;
      case f.MODE_RAM > 3 && (f.StatusCh || 0) === 0x01: return 5;
      case f.MODE_RAM > 3 && (f.StatusCh || 0) === 0x02: return 6;
      case f.MODE_RAM > 3 && (f.StatusCh || 0) === 0x04: return 7;
      case f.MODE_RAM > 3 && (f.StatusCh || 0) === 0x08: return 8;
      case f.MODE_RAM > 3: return 9;
      case f.LCD_NetUpECO === 0 && f.LCD_NetUpLoad === 1 && ((f.Pmax_On || 0) & 2) && (f.UNET ? f.UNET + 100 : 0) > 100: return 18;
      case f.LCD_NetUpECO === 0 && f.MODE_RAM === 2 && (f.UNET ? f.UNET + 100 : 0) > 100 && f.LCD_NETAlg === 2: return 10;
      case f.LCD_NetUpECO === 0 && f.MODE_RAM === 2 && (f.UNET ? f.UNET + 100 : 0) > 100 && f.LCD_NETAlg === 3 && ((f.flag_NETON_ECO || 0) & 2) === 0: return 11;
      case f.LCD_NetUpECO === 0 && f.MODE_RAM === 2 && (f.UNET ? f.UNET + 100 : 0) > 100 && f.LCD_NETAlg === 3: return 12;
      case f.LCD_NetUpECO === 0 && f.MODE_RAM === 2: return 2;
      case f.LCD_NetUpECO === 0: return 3;
      case f.MODE_RAM === 3 && f.LCD_NetUpECO === 1 && f.LCD_NETAlg === 2 && ((f.flag_NETON_ECO || 0) & 1): return 15;
      case f.MODE_RAM === 3 && f.LCD_NetUpECO === 1 && f.LCD_NETAlg === 2: return 13;
      case f.MODE_RAM === 3 && f.LCD_NetUpECO === 1 && f.LCD_NETAlg === 3 && ((f.flag_NETON_ECO || 0) & 2): return 12;
      case f.MODE_RAM === 3 && f.LCD_NetUpECO === 1 && f.LCD_NETAlg === 3: return 16;
      case f.MODE_RAM === 3 && f.LCD_NetUpECO === 2 && f.LCD_NETAlg === 2 && ((f.flag_NETON_ECO || 0) & 1): return 15;
      case f.MODE_RAM === 3 && f.LCD_NetUpECO === 2 && f.LCD_NETAlg === 2: return 14;
      case f.MODE_RAM === 3 && f.LCD_NetUpECO === 2 && f.LCD_NETAlg === 3 && ((f.flag_NETON_ECO || 0) & 2): return 12;
      case f.MODE_RAM === 3 && f.LCD_NetUpECO === 2 && f.LCD_NETAlg === 3: return 17;
      case f.MODE_RAM === 2: return 2;
    }
    return 3;
  }
  function isCharge(f) { var c = modeNum(f); return c >= 4 && c <= 9; }
  function modeLabel(c, f) {
    if (c === 9) { var sc = (f && f.StatusCh) || 0; return t("p1.mode9_prefix") + " " + sc + " (" + sc.toString(2) + ")"; }
    return t("p1.mode" + c);
  }
  function modeText(f) {
    var c = modeNum(f);
    if (c == null) { return ""; }
    return modeLabel(c, f);
  }
  function serialOf(f) {
    if (SERIAL) { return SERIAL; }
    var s0 = f["SerialNum0"], s1 = f["SerialNum1"], s3 = f["SerialNum3"];
    if (typeof s0 === "number" && typeof s1 === "number") { var n = String(s0 + s1 * 256); if (s3) { n += " " + String.fromCharCode(s3); } return n; }
    return "—";
  }

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
    if (il !== undefined && ih !== undefined) {
      // Как в mapd: беззнаковое (L + H*256)/16, знак — по режиму (заряд MODE==4 → «+», иначе «−»).
      v.Iacc = (il + ih * 256) / 16;
      if (!isCharge(f)) { v.Iacc = -v.Iacc; }
    }
    var sh = (typeof f["UACC"] === "number") ? (f["UACC"] & 0x1f) : 2;
    if (typeof f["E_LCD_UAccChBUF"] === "number") { v.Ubuf = (f["E_LCD_UAccChBUF"] << sh) / 10; }
    if (typeof f["E_LCD_UAccChMax"] === "number") { v.Uchg = (f["E_LCD_UAccChMax"] << sh) / 10; }
    if (typeof f["UAccMinNetGen_S"] === "number") { v.Ueco = f["UAccMinNetGen_S"] / 10; } // реальное напряжение ЭКО
    function counter(base) { var L = f[base + "_L"], H = f[base + "_H"], HH = f[base + "_HH"]; if (typeof L !== "number" || typeof H !== "number" || typeof HH !== "number") { return undefined; } return (L + H * 256 + HH * 65536) / 100; }
    v.Enet = counter("M_POWhourNET"); v.Eacc = counter("M_POWhourMAP"); v.Echg = counter("M_POWhourMAPCharge");
    v.Tacc = (typeof f["Temp_Grad0"] === "number") ? f["Temp_Grad0"] - 50 : undefined;
    v.Ttor = (typeof f["Temp_Grad1"] === "number") ? f["Temp_Grad1"] - 50 : undefined;
    v.Ttr = (typeof f["Temp_Grad2"] === "number") ? f["Temp_Grad2"] - 50 : undefined;
    v.Fnet = num(f, "TFNET") ? 6250 / f["TFNET"] : undefined;
    v.Fout = num(f, "ThFMAP") ? 6250 / f["ThFMAP"] : undefined;
    var pow = num(f, "POW"), uacc = num(f, "UACC");
    v.Pmax = (pow !== undefined && POWER_KW[pow] !== undefined) ? POWER_KW[pow] * 1000 : 20000;
    // Предел мощности АКБ — доля от номинала МАП; долю берём из ячейки PowAccNom (%),
    // при отсутствии — 68%.
    var powAccNom = num(f, "PowAccNom");
    var powAccPct = (powAccNom !== undefined && powAccNom !== 0 && powAccNom !== 255) ? powAccNom / 100 : 0.68;
    v.PbatMax = v.Pmax * powAccPct;
    v.Imax = (pow !== undefined && uacc !== undefined && VOLTAGE[uacc]) ? POWER_KW[pow] * 1500 / VOLTAGE[uacc] : 50;
    v.Pnet = (v.Uin !== undefined && v.Iin !== undefined) ? v.Uin * v.Iin : undefined;
    if (v.Pnet !== undefined && f["PNET_Sign_P"] === 0) { v.Pnet = -v.Pnet; }
    v.Pbat = (v.Uacc !== undefined && v.Iacc !== undefined) ? v.Uacc * v.Iacc : undefined;
    v.IFaze = num(f, "LCD_MAP_iFaze");
    function ph(l, h) { var L = num(f, l) || 0, H = num(f, h) || 0; var x = ((L & 0xff) | ((H & 0x7f) << 8)) / 10; return (H & 0x80) ? -x : x; }
    v.Iph1 = ph("I_Akb_MAP_faz1L", "I_Akb_MAP_faz1H"); v.Iph2 = ph("I_Akb_MAP_faz2L", "I_Akb_MAP_faz2H"); v.Iph3 = ph("I_Akb_MAP_faz3L", "I_Akb_MAP_faz3H");
    // Pвых = Pnet − Pbat (мощность сети минус мощность нагрузки по АКБ)
    v.Pout = (typeof v.Pnet === "number" ? v.Pnet : 0) - (typeof v.Pbat === "number" ? v.Pbat : 0);
    v.Iout = v.Uout ? Math.round((v.Pout / v.Uout) * 10) / 10 : undefined; // Iвых = Pвых / Uвых (≤1 знак)
    if (v.Uacc !== undefined) { v.Ph1 = v.Uacc * v.Iph1 / 1000; v.Ph2 = v.Uacc * v.Iph2 / 1000; v.Ph3 = v.Uacc * v.Iph3 / 1000; }
    v.FlagUnet2 = num(f, "flagUnet2") || 0;
    v.Cooler = num(f, "CulerSpead") || 0;
    var rele = num(f, "Status_RELEdop") || 0;
    v.Relay1 = rele & 1; v.Relay2 = (rele >> 1) & 1; v.Relay3 = (rele >> 2) & 1;
    var errs = ["RSErrSis", "RSErrJobM", "RSErrJob", "RSWarning", "RSErrDop"], eb = 0;
    errs.forEach(function (k) { if (typeof f[k] === "number" && f[k] !== 0) { eb++; } });
    v.ErrCount = eb;

    var bmsf = num(f, "LCD_BMSFunk") || 0, kes = [], bms = [];
    if (bmsf === 2 || bmsf === 3) {
      var kn = num(f, "LCD_MPPTNum") || 0;
      if (kn >= 1 && kn <= 16) { for (var i = 0; i < kn; i++) { var kl = num(f, "I_Akb_MPPT_L" + i) || 0, kh = num(f, "I_Akb_MPPT_H" + i) || 0; kes.push({ I: ((kl & 0xff) | ((kh & 0x7f) << 8)) / 16, T: (num(f, "Takb_MPPT" + i) || 0) - 50 }); } }
    }
    if (bmsf === 1 || bmsf === 3) {
      var bn = num(f, "BMS_Num") || 0;
      if (bn >= 1 && bn <= 32) { for (var j = 0; j < bn; j++) { var L = num(f, "UakbCell_L" + j) || 0, H = num(f, "UakbCell_H" + j) || 0; var vol = ((L & 0xff) | ((H & 0x7f) << 8)) / 100; var raw = num(f, "TakbCell" + j) || 0; var q = num(f, "Q_Cell" + j) || 0; bms.push({ V: vol, t: (raw === 255 ? 127 : raw - 50), I: q / 100 * vol }); } }
    }
    v.kes = kes; v.bms = bms;
    if (kes.length) { var s = 0; for (var k2 = 0; k2 < kes.length; k2++) { s += kes[k2].I; } v.Imppt = s / kes.length; if (v.Uacc) { v.Pmppt = v.Uacc * v.Imppt; } }
    // Ошибка вентилятора: документированный бит FT_Culer (0x08) в F_AccOver.
    // (CulerErr=0x48 — счётчик-накопитель, не флаг, поэтому не используем.)
    v.FanErr = (((num(f, "F_AccOver") || 0) & 0x08) !== 0);

    // Время МАП: часы и десятки минут — E_LCD_TimeCyr, минуты — TimeCyr_MINUT;
    // дата (если валидна) — WATCH.
    var tcyr = num(f, "E_LCD_TimeCyr"), tmin = num(f, "TimeCyr_MINUT");
    if (tcyr !== undefined) {
      var whh = (tcyr >> 3) & 0x1f, wmm = (tcyr & 0x07) * 10 + (tmin || 0);
      if (whh <= 23 && wmm <= 59) {
        var dt = "", watch = num(f, "WATCH");
        if (watch !== undefined) {
          var wday = watch & 0x1f, wmon = (watch >> 5) & 0x0f, wyr = 2000 + ((watch >> 9) & 0x3f);
          if (wday >= 1 && wday <= 31 && wmon >= 1 && wmon <= 12) { dt = pad2(wday) + "." + pad2(wmon) + "." + String(wyr).slice(-2) + " "; }
        }
        v.MapTime = dt + pad2(whh) + ":" + pad2(wmm);
      }
    }

    v.mode = modeText(f);
    return v;
  }

  // --- SVG ---
  var gid = 0;
  function gradStops(g) {
    if (g === "i") { return '<stop offset="0" stop-color="#c0392b"/><stop offset="0.5" stop-color="#e6c94a"/><stop offset="1" stop-color="#63b94e"/>'; }
    if (g === "ib") { return '<stop offset="0" stop-color="#2f6ea3"/><stop offset="0.5" stop-color="#9fb3c0"/><stop offset="1" stop-color="#c0392b"/>'; }
    if (g === "m") { return '<stop offset="0" stop-color="#2f6ea3"/><stop offset="0.5" stop-color="#38a8b6"/><stop offset="1" stop-color="#63b94e"/>'; }
    if (g === "b") { return '<stop offset="0" stop-color="#1b3a6b"/><stop offset="0.5" stop-color="#63b94e"/><stop offset="1" stop-color="#c0392b"/>'; }
    return '<stop offset="0" stop-color="#2f6ea3"/><stop offset="0.2" stop-color="#38b6c6"/><stop offset="0.4" stop-color="#74c44c"/><stop offset="0.55" stop-color="#8fd157"/><stop offset="0.72" stop-color="#e6c94a"/><stop offset="0.86" stop-color="#e0902f"/><stop offset="1" stop-color="#c0392b"/>';
  }
  // Стрелочный гейдж: диапазон [lo..hi], градиент по типу (v/i/m) + стрелка от центра.
  function svgGauge(title, val, lo, hi, grad) {
    var id = "g" + (gid++), cx = 70, cy = 84, R = 52, a0 = 180, sweep = 180, frac = 0;
    if (typeof val === "number" && isFinite(val) && hi > lo) { frac = clamp((val - lo) / (hi - lo), 0, 1); }
    function pt(a, r) { var rad = a * Math.PI / 180; return [cx + r * Math.cos(rad), cy + r * Math.sin(rad)]; }
    function arcPath(aS, aE, r) {
      if (aE < aS) { var t = aS; aS = aE; aE = t; }
      var s = pt(aS, r), e = pt(aE, r), large = (aE - aS) > 180 ? 1 : 0;
      return "M" + s[0].toFixed(1) + " " + s[1].toFixed(1) + " A" + r + " " + r + " 0 " + large + " 1 " + e[0].toFixed(1) + " " + e[1].toFixed(1);
    }
    var aVal = a0 + frac * sweep;
    var valid = (typeof val === "number" && isFinite(val));
    var bipolar = lo < 0 && hi > 0;
    var aZero = a0 + ((0 - lo) / (hi - lo)) * sweep;
    var aStart = bipolar ? aZero : a0;
    var frame = arcPath(a0, a0 + sweep, R);
    var fill = (valid && Math.abs(aVal - aStart) > 0.5) ? arcPath(aStart, aVal, R) : "";
    return '<div class="p1-gauge"><svg viewBox="0 16 140 96" width="132" height="90">' +
      '<defs><linearGradient id="' + id + '" gradientUnits="userSpaceOnUse" x1="' + (cx - R) + '" y1="0" x2="' + (cx + R) + '" y2="0">' + gradStops(grad) + '</linearGradient></defs>' +
      '<path d="' + frame + '" fill="none" stroke="#0d5637" stroke-width="3" stroke-linecap="butt"/>' +
      (fill ? '<path d="' + fill + '" fill="none" stroke="url(#' + id + ')" stroke-width="20" stroke-linecap="butt"/>' : '') +
      '<text x="' + cx + '" y="' + (cy - 12) + '" text-anchor="middle" dominant-baseline="central" class="p1-gv">' + fmt(val, 1) + '</text>' +
      '</svg><div class="p1-glbl">' + esc(title) + '</div></div>';
  }
  // Градиентная полоса со СКОЛЬЗЯЩИМ значением (как бегунок в оригинале).
  function sliderBar(leftPct, text, neg, title) {
    var box = '<div class="p1-slider' + (neg ? ' neg' : '') + '"' + (title ? ' title="' + esc(title) + '"' : '') + ' style="left:' + clamp(leftPct, 4, 96).toFixed(1) + '%">' + text + '</div>';
    return '<div class="p1-bar">' + box + '</div>';
  }
  // Линейная шкала мощности: 0 в центре, [−pmax..+pmax] на всю ширину полосы.
  function posNet(p, pmax) { if (typeof p !== "number" || !isFinite(p) || pmax <= 0) { return 50; } return 50 + clamp(p / pmax, -1, 1) * 46; }
  function posMppt(p, pmax) { if (!p || pmax <= 0) { return 0; } return 80 * Math.log(p / 10) / Math.log(pmax); }

  // Солнышко КЭС: серое (нет мощности) / жёлтое (Pmppt > 1 Вт).
  function sunSvg(col) {
    var rays = "";
    for (var a = 0; a < 360; a += 45) {
      var rad = a * Math.PI / 180, x1 = 12 + 8 * Math.cos(rad), y1 = 12 + 8 * Math.sin(rad), x2 = 12 + 11 * Math.cos(rad), y2 = 12 + 11 * Math.sin(rad);
      rays += '<line x1="' + x1.toFixed(1) + '" y1="' + y1.toFixed(1) + '" x2="' + x2.toFixed(1) + '" y2="' + y2.toFixed(1) + '" stroke="' + col + '" stroke-width="2" stroke-linecap="round"/>';
    }
    return '<svg width="20" height="20" viewBox="0 0 24 24" style="vertical-align:middle"><circle cx="12" cy="12" r="5.5" fill="' + col + '"/>' + rays + '</svg>';
  }

  // Индикаторы: вход 220В (гнездо), вентилятор (вращается), реле.
  function icoInput(on, active, label) {
    return '<span class="p1-inp' + (on ? ' on' : '') + (active ? ' act' : '') + '" title="' + esc(label) + '">' +
      '<svg width="22" height="22" viewBox="0 0 24 24"><circle cx="12" cy="12" r="9" fill="none" stroke="currentColor" stroke-width="2"/>' +
      '<circle cx="9" cy="12" r="1.7" fill="currentColor"/><circle cx="15" cy="12" r="1.7" fill="currentColor"/></svg></span>';
  }
  function icoFan(on, err) {
    var title = err ? t("p1.fan.err") : (on ? t("p1.fan.on") : t("p1.fan.off"));
    return '<span class="p1-fan' + (on ? ' on' : '') + (err ? ' err' : '') + '" title="' + esc(title) + '"><svg width="26" height="26" viewBox="0 0 24 24">' +
      '<g fill="currentColor"><ellipse cx="12" cy="7" rx="2.4" ry="4.2"/>' +
      '<ellipse cx="12" cy="7" rx="2.4" ry="4.2" transform="rotate(120 12 12)"/>' +
      '<ellipse cx="12" cy="7" rx="2.4" ry="4.2" transform="rotate(240 12 12)"/></g>' +
      '<circle cx="12" cy="12" r="2.2" fill="#e3efe8"/></svg></span>';
  }
  function relayChip(n, on) { return '<span class="p1-relay' + (on ? ' on' : '') + '" title="' + t("p1.relay") + ' ' + n + '">R' + n + '</span>'; }

  function infCell(label, value, hint) {
    return '<td title="' + esc(hint) + '"><span class="p1-inf-l">' + esc(label) + '</span><span class="p1-inf-v">' + value + '</span></td>';
  }

  function bmsTable(bms) {
    if (!bms || !bms.length) { return ""; }
    var min = bms[0].V, max = 0, i;
    for (i = 0; i < bms.length; i++) { if (bms[i].V < min) { min = bms[i].V; } if (bms[i].V > max) { max = bms[i].V; } }
    var head = "", body = "";
    for (i = 0; i < bms.length; i++) {
      var bg = (min !== max) ? (bms[i].V === min ? "#5bc0de" : (bms[i].V === max ? "#e48683" : "#0d5637")) : "#0d5637";
      head += '<th style="background:' + bg + ';color:#fff">BMS ' + (i + 1) + '</th>';
      body += '<td>' + fmt(bms[i].V, 2) + 'В<br>' + fmt(bms[i].I, 2) + 'А<br>' + ((bms[i].t === 127) ? '-' : bms[i].t) + '&deg;C</td>';
    }
    return '<details class="p1-details" open><summary>' + t("p1.bms") + '</summary><div class="p1-scroll"><table class="p1-tbl"><tr>' + head + '</tr><tr>' + body + '</tr></table></div></details>';
  }
  function kesTable(kes) {
    if (!kes || !kes.length) { return ""; }
    var cells = "";
    for (var i = 0; i < kes.length; i++) { cells += '<td><b>' + (i + 1) + '</b> ' + fmt(kes[i].I, 2) + ' А</td>'; }
    return '<details class="p1-details" open><summary>' + t("p1.mppts") + '</summary><table class="p1-tbl mppt"><tr>' + cells + '</tr></table></details>';
  }

  function renderPanel(p) {
    var f = p.fields, v = compute(f);
    fillLim(f);
    var Ulo = LIM.Umin || 120, Uhi = LIM.Umax || 320, Vm = LIM.Vmodel || 48;
    var ImaxG = limImaxGrid(), IbMax = limIbatMax(), ImMax = limImpptMax();
    var gauges = svgGauge(t("p1.uin"), v.Uin, Ulo, Uhi, "v") + svgGauge(t("p1.uout"), v.Uout, Ulo, Uhi, "v") +
      svgGauge(t("p1.iin"), v.Iin, -ImaxG, ImaxG, "ib") + svgGauge(t("p1.iout"), v.Iout, -ImaxG, ImaxG, "ib") +
      svgGauge(t("p1.uacc"), v.Uacc, LIM.UakbLo || (Vm / 2), LIM.UakbHi || (Vm * 1.4), "b") + svgGauge(t("p1.iacc"), v.Iacc, -IbMax, IbMax, "i") +
      svgGauge(t("p1.imppt"), v.Imppt, 0, ImMax, "m");
    var pnetTxt = (typeof v.Pnet === "number" ? Math.round(v.Pnet) : "—") + " " + t("p1.va");
    var poutTxt = (typeof v.Pout === "number" ? Math.round(v.Pout) : "—") + " " + t("p1.w");
    var pbatTxt = (typeof v.Pbat === "number" ? Math.round(v.Pbat) : "—") + " " + t("p1.w");
    var mpptTxt = typeof v.Imppt === "number" ? ("i:" + Math.round(v.Uacc ? v.Uacc * v.Imppt : 0) + " " + t("p1.w")) : "";

    panel.innerHTML =
      '<div class="p1">' +
      '<div class="p1-head">' +
        '<span class="p1-ind">' +
          icoInput((v.FlagUnet2 & 2) !== 0, (v.FlagUnet2 & 1) === 0, t("p1.in1")) +
          icoInput((v.FlagUnet2 & 4) !== 0, (v.FlagUnet2 & 1) !== 0, t("p1.in2")) +
          icoFan(v.Cooler > 0, v.FanErr) +
          '<span class="p1-relays">' + relayChip(1, v.Relay1) + relayChip(2, v.Relay2) + relayChip(3, v.Relay3) + '</span>' +
        '</span>' +
        '<span class="p1-mode">' + esc(v.mode) + '</span>' +
        '<span class="p1-right"><span class="p1-maptime" title="' + esc(t("p1.h.maptime")) + '">' + (v.MapTime || "—") + '</span>' +
          '<b class="p1-time" title="' + esc(t("p1.h.ostime")) + '">' + hhmmss(p.last_seen) + '</b><span class="p1-id">' + t("p1.serial") + esc(serialOf(f)) + '</span></span>' +
      '</div>' +
      '<table class="p1-main"><tr>' +
        '<td width="50%"><div class="p1-row1"><span class="p1-freq">' + fmt(v.Fnet, 1) + t("p1.hz") + '</span>' +
          '<span class="p1-freq right">' + fmt(v.Fout, 1) + t("p1.hz") + '</span></div>' +
          sliderBar(posNet(v.Pnet, v.Pmax), pnetTxt, false, t("p1.pnet")) +
          sliderBar(posNet(v.Pout, v.Pmax), poutTxt, false, t("p1.pload")) +
          (v.IFaze === 1 ? '<div class="p1-phases">&#966;<sub>1</sub>:' + fmt(v.Ph1, 2) + '&nbsp;' + t("p1.kw") + '&nbsp;&nbsp;&#966;<sub>2</sub>:' + fmt(v.Ph2, 2) + '&nbsp;' + t("p1.kw") + '&nbsp;&nbsp;&#966;<sub>3</sub>:' + fmt(v.Ph3, 2) + '&nbsp;' + t("p1.kw") + '</div>' : '') + '</td>' +
        '<td width="33.3%"><div class="p1-row1"><span class="p1-freq">' + t("p1.bat") + '</span></div>' +
          sliderBar(posNet(v.Pbat, v.PbatMax), pbatTxt, false) + '</td>' +
        '<td width="16.7%"><div class="p1-row1">' + sunSvg((typeof v.Pmppt === "number" && v.Pmppt > 1) ? "#f5c518" : "#9aa4a0") + (v.kes.length ? '<a class="p1-link" href="#p1-mppt">' + t("p1.mppts") + '</a>' : '') + '</div>' +
          sliderBar(posMppt(v.Pmppt, v.Pmax * 0.3), mpptTxt, false) + '</td>' +
      '</tr><tr><td colspan="3" class="p1-infwrap"><table class="p1-inf"><tr>' +
        infCell(t("p1.tacc"), fmt(v.Tacc, 0) + '&deg;C', t("p1.h.tacc")) +
        infCell(t("p1.ttor"), fmt(v.Ttor, 0) + '&deg;C', t("p1.h.ttor")) +
        infCell(t("p1.ttr"), fmt(v.Ttr, 0) + '&deg;C', t("p1.h.ttr")) +
        infCell(t("p1.net"), fmt(v.Enet, 2) + ' ' + t("p1.kwh"), t("p1.h.net")) +
        infCell(t("p1.charge"), fmt(v.Echg, 2) + ' ' + t("p1.kwh"), t("p1.h.charge")) +
        infCell(t("p1.frombat"), fmt(v.Eacc, 2) + ' ' + t("p1.kwh"), t("p1.h.frombat")) +
      '</tr></table></td></tr></table>' +
      '<div class="p1-gauges">' + gauges + '</div>' +
      '<div class="p1-foot"><span>' + t("p1.ubuf") + ':<b>' + fmt(v.Ubuf, 1) + '</b>' + t("p1.v") + '</span>' +
        '<span>' + t("p1.uchg") + ':<b>' + fmt(v.Uchg, 1) + '</b>' + t("p1.v") + '</span>' +
        '<span>' + t("p1.ueco") + ':<b>' + fmt(v.Ueco, 1) + '</b>' + t("p1.v") + '</span>' +
        '<span class="right">' + t("p1.err") + '&nbsp;<b class="p1-badge">' + v.ErrCount + '</b></span></div>' +
      '<div id="p1-bms">' + bmsTable(v.bms) + '</div>' +
      '<div id="p1-mppt">' + kesTable(v.kes) + '</div>' +
      '</div>';
  }

  // ===== Вкладка «Информация»: подзакладки «Основные» и «Системные» =====
  // Данные — из того же бинарного payload (window.MAPINFO: адреса, группы,
  // единицы, конверсии, описания; см. docs/map-contract.md, docs/map-memory.md).
  var MI = window.MAPINFO || { groups: [], hide: [], fields: {} };
  var MIHIDE = {};
  (MI.hide || []).forEach(function (h) { MIHIDE[h] = 1; });
  var MVOLT = [12, 24, 48, 96];
  var MPOW = [1.3, 1.5, 2, 3, 4.5, 6, 9, 12, 15, 20, 24, 36];

  function L(prefix, v) { var k = prefix + "." + v; return (I18N[k] !== undefined) ? I18N[k] : String(v); }
  function U(k) { return I18N["mi.u." + k] || k; }
  function gv(f, k) { var x = f[k]; return (typeof x === "number") ? x : undefined; }
  function shift(f) { return (typeof f["UACC"] === "number") ? f["UACC"] : 2; }

  // Конверсия сырой ячейки в человекочитаемое значение (без единицы).
  function conv(code, raw, f) {
    if (typeof raw !== "number") { return null; }
    switch (code) {
      case "raw": return String(raw);
      case "x0.1": return (raw / 10).toFixed(1);
      case "x0.01": return (raw / 100).toFixed(2);
      case "x100": return String(raw * 100);
      case "x4": return String(raw * 4);
      case "watch": { var wd = raw & 0x1f, wm = (raw >> 5) & 0x0f, wy = 2000 + ((raw >> 9) & 0x3f); return (wd >= 1 && wd <= 31 && wm >= 1 && wm <= 12) ? (pad2(wd) + "." + pad2(wm) + "." + String(wy).slice(-2)) : String(raw); }
      case "hhmm": return pad2(raw >> 3) + ":" + pad2((raw & 7) * 10);
      case "netmax": return String((raw << (gv(f, "LCD_NetMaxPow_dop") || 0)) * 100);
      case "x2h": return String(raw * 2);
      case "temp": return String(raw - 50);
      case "v100": return String(raw === 0 ? 0 : raw + 100);
      case "f6250": return raw ? (6250 / raw).toFixed(2) : null;
      case "f2500": return raw ? (2500 / raw).toFixed(2) : null;
      case "inet164": { var inet = gv(f, "INET") || 0; return (raw / (inet >= 16 ? 4 : 16)).toFixed(2); }
      case "p8": return String(s16(raw) * 10);
      case "pow_kw": return (MPOW[raw] !== undefined) ? String(MPOW[raw]) : String(raw);
      case "uacc_v": return (MVOLT[raw] !== undefined) ? String(MVOLT[raw]) : String(raw);
      case "lang": return raw === 0 ? L("mi.lang", 0) : L("mi.lang", 1);
      case "fw": return (raw & 0x1f) + "." + (raw >> 5);
      case "bool": return raw ? t("mi.yes") : t("mi.no");
      case "sin": return L("mi.sin", raw);
      case "phase": return raw === 255 ? t("mi.phase_off") : L("mi.phase", raw);
      case "pmax_mode": return L("mi.pmax", raw);
      case "eco": return raw === 255 ? t("mi.none") : L("mi.eco", raw);
      case "netalg": return L("mi.netmode", raw);
      case "acctype": return L("mi.acctype", raw);
      case "chgalg": return L("mi.chgalg", raw);
      case "cacc": return String((raw * 25) >> shift(f));
      case "rsbaud": case "rsbaud485": return L("mi.rsbaud", raw);
      case "rsproto": return L("mi.rsproto", raw);
      case "adddev": return L("mi.adddev", raw);
      case "extdev": return L("mi.ext", raw);
      case "din": return L("mi.din", raw);
      case "mode": return modeLabel(raw, f);
      default: return String(raw);
    }
  }

  // Комбинированные (многобайтные) величины — сводятся из нескольких ячеек.
  function combo(name, f) {
    function r(v, u, k) { return { v: v, u: u, t: k }; }
    switch (name) {
      case "UAcc_med_VH": { var vh = gv(f, "UAcc_med_VH"), vl = gv(f, "UAcc_med_VL"); if (vh === undefined && vl === undefined) { return null; } return r((((vh || 0) * 256 + (vl || 0)) / 10).toFixed(1), "V", "mi.f.battery_voltage"); }
      case "IAcc_med_A_u16_L": { var l = gv(f, "IAcc_med_A_u16_L"), h = gv(f, "IAcc_med_A_u16_H"); if (l === undefined && h === undefined) { return null; } var am = (l + (h || 0) * 256) / 16; if (!isCharge(f)) { am = -am; } return r(am.toFixed(2), "A", "mi.f.iacc_med"); }
      case "M_POWhourNET_L": return r(cnt24(f, "M_POWhourNET").toFixed(2), "kWh", "mi.f.e_net");
      case "M_POWhourMAP_L": return r(cnt24(f, "M_POWhourMAP").toFixed(2), "kWh", "mi.f.e_acc");
      case "M_POWhourMAPCharge_L": return r(cnt24(f, "M_POWhourMAPCharge").toFixed(2), "kWh", "mi.f.e_chg");
      case "PLoad_L": { var pl = gv(f, "PLoad_L"), ph = gv(f, "PLoad_H"); if (pl === undefined && ph === undefined) { return null; } return r(String(((pl || 0) + (ph || 0) * 256) * 100), "W", "mi.f.pload"); }
      case "PNET_L": { var nl = gv(f, "PNET_L"), nh = gv(f, "PNET_H"); if (nl === undefined && nh === undefined) { return null; } return r(String(((nl || 0) + (nh || 0) * 256) * 100), "W", "mi.f.pnet"); }
      case "I_Akb_MAP_faz1L": return r(fazA(f, 1), "A", "mi.f.iph1");
      case "I_Akb_MAP_faz2L": return r(fazA(f, 2), "A", "mi.f.iph2");
      case "I_Akb_MAP_faz3L": return r(fazA(f, 3), "A", "mi.f.iph3");
    }
    return undefined;
  }
  function cnt24(f, base) { var L = gv(f, base + "_L") || 0, H = gv(f, base + "_H") || 0, HH = gv(f, base + "_HH") || 0; return (L + H * 256 + HH * 65536) / 100; }
  function fazA(f, n) { var L = gv(f, "I_Akb_MAP_faz" + n + "L") || 0, H = gv(f, "I_Akb_MAP_faz" + n + "H") || 0; var v = ((L & 0xff) | ((H & 0x7f) << 8)) / 10; return ((H & 0x80) ? -v : v).toFixed(1); }

  function addrOf(n) { var e = MI.fields[n]; return e ? e.a : -1; }
  var SYSCACHE = null;
  function sysRows() {
    if (SYSCACHE) { return SYSCACHE; }
    var by = {};
    Object.keys(MI.fields).forEach(function (n) {
      if (MIHIDE[n]) { return; }
      var e = MI.fields[n], gi = (typeof e.g === "number") ? e.g : MI.groups.length - 1;
      (by[gi] = by[gi] || []).push(n);
    });
    Object.keys(by).forEach(function (gi) { by[gi].sort(function (x, y) { return MI.fields[x].a - MI.fields[y].a; }); });
    SYSCACHE = by; return by;
  }

  // --- «Основные»: паритет со старой страницей /mac_info.php ---
  function buildMain(f) {
    var rows = [];
    function row(k, v) { rows.push([k, (v === undefined || v === null || v === "") ? "—" : String(v)]); }
    var devopt = gv(f, "DevOpt"), verpow = gv(f, "VerPow"), vpd = gv(f, "VerPlatPowDop"), vpic = gv(f, "VerPlatPic"), vnet = gv(f, "VerPlatNet");
    // version_hw
    if (devopt !== undefined) {
      var s = (devopt <= 2 ? "LCD" : "TFT") + " Pow v" + (verpow !== undefined ? (verpow & 0x1f) : "?");
      if (vpd !== undefined) { s += ((vpd & 2) ? "H" : "L"); }
      if (verpow !== undefined && (verpow & 0x80)) { s += " " + t("mi.hybrid"); }
      if (vpic !== undefined) { s += " Mv" + vpic; }
      if (vnet !== undefined) { s += " Nv" + (vnet & 7) + (((vnet >> 3) & 1) === 0 ? "L" : "H"); }
      row("version_hw", s);
    } else { row("version_hw", "—"); }
    var vpo = gv(f, "VerPO");
    row("version_fw", vpo === undefined ? "—" : ((vpo & 0x1f) + "." + (vpo >> 5)));
    var sd = gv(f, "SyncDiz_Plat"), devmodel;
    if (devopt === 3) { devmodel = t("mi.model_titan"); }
    else if (sd !== undefined && (sd & 1)) { devmodel = t("mi.model_dom"); }
    else if (verpow !== undefined && (verpow & 0x80)) { devmodel = t("mi.model_hyb"); }
    else { devmodel = t("mi.model_pro"); }
    row("device_type", devmodel);
    var pow = gv(f, "POW"), uacc = gv(f, "UACC");
    row("device_power", pow === undefined ? "—" : (MPOW[pow] !== undefined ? MPOW[pow] : pow) + " " + U("kW"));
    row("work_voltage", uacc === undefined ? "—" : (MVOLT[uacc] !== undefined ? MVOLT[uacc] : uacc) + " " + U("V"));
    row("motherboard_version", devopt === undefined ? "—" : devopt);
    var mask = gv(f, "MASK_DEV_ON");
    if (mask !== undefined) { var ts = []; if (mask & 1) { ts.push(t("mi.env")); } if (mask & 2) { ts.push(t("mi.tor")); } if (mask & 4) { ts.push(t("mi.transistors")); } row("t_sensors", ts.length ? ts.join(", ") : "—"); } else { row("t_sensors", "—"); }
    row("vent_no", gv(f, "NUMCULLER"));
    row("min_curr_absorption", gv(f, "CIChargeAbsorb") === undefined ? "—" : (f["CIChargeAbsorb"] / 100) + " C");
    row("chg_time_BMS", gv(f, "T_MaxLiIonBMS") === undefined ? "—" : f["T_MaxLiIonBMS"] + " " + U("min"));
    row("nominal_power", gv(f, "PowAccNom") === undefined ? "—" : f["PowAccNom"] + "% " + t("mi.from_max"));
    row("restart_attempts", gv(f, "NUM_Overload"));
    row("chg_delay", gv(f, "T_CHARGE") === undefined ? "—" : f["T_CHARGE"] + " " + U("s"));
    row("stab_time", gv(f, "T_NET_ON") === undefined ? "—" : f["T_NET_ON"] + " " + U("s"));
    row("tmax_chg", gv(f, "T_CHARGE_Umax") === undefined ? "—" : f["T_CHARGE_Umax"] + " " + U("min"));
    row("NET_delay", "—");
    row("discharged_acc_time", gv(f, "T_ACCDISCH") === undefined ? "—" : f["T_ACCDISCH"] + " " + U("s"));
    row("Vdd", gv(f, "P_ModemVdd") === undefined ? "—" : ((f["P_ModemVdd"] & 1) ? t("mi.always_on") : t("mi.depends_on_mode")));
    row("sine_accuracy", gv(f, "LCD_TypeSin") === undefined ? "—" : L("mi.sin", f["LCD_TypeSin"]));
    var ph = gv(f, "LCD_MAP_iFaze");
    row("3ph", ph === undefined ? "—" : (ph === 255 ? t("mi.phase_off") : L("mi.phase", ph)));
    row("Uout_gen", gv(f, "LCD_UMAP220_NEED") === undefined ? "—" : (f["LCD_UMAP220_NEED"] + 100) + " " + U("V"));
    row("Pmax_add", gv(f, "LCD_NetUpLoad") === undefined ? "—" : L("mi.pmax", f["LCD_NetUpLoad"]));
    var eco = gv(f, "LCD_NetUpECO");
    row("Eco_mode", eco === undefined ? "—" : (eco === 255 ? t("mi.no_in_settings") : L("mi.eco", eco)));
    var sh = shift(f);
    row("Uacc_min", (gv(f, "LCD_UAccMin") === undefined) ? "—" : (((f["LCD_UAccMin"] << sh) + (gv(f, "LCD_UAccMin_dop") || 0)) / 10) + " " + U("V"));
    var load = gv(f, "LCD_SensLoad");
    row("Load_gen", load === undefined ? "—" : (load === 0 ? t("mi.off_full") : load + " " + U("W")));
    var par = gv(f, "LCD_SlaveMAPNum");
    row("parallel_macs", par === undefined ? "—" : (par === 255 ? t("mi.not_available") : par));
    row("input_source", gv(f, "LCD_NetDizel") === undefined ? "—" : L("mi.net", f["LCD_NetDizel"]));
    row("external_devices", gv(f, "LCD_BMSFunk") === undefined ? "—" : L("mi.ext", f["LCD_BMSFunk"]));
    var mppt = gv(f, "LCD_MPPTNum");
    row("mppt_no", mppt === undefined ? "—" : (mppt === 255 ? t("mi.off_full") : mppt));
    row("input_max", gv(f, "LCD_NetMaxPow") === undefined ? "—" : ((f["LCD_NetMaxPow"] << (gv(f, "LCD_NetMaxPow_dop") || 0)) * 100) + " " + U("W"));
    row("hi_threshold", gv(f, "LCD_UNETUp") === undefined ? "—" : (f["LCD_UNETUp"] + 100) + " " + U("V"));
    row("lo_threshold", gv(f, "LCD_UNETDown") === undefined ? "—" : (f["LCD_UNETDown"] + 100) + " " + U("V"));
    var alg = gv(f, "LCD_NETAlg");
    row("net_alg", alg === undefined ? "—" : L("mi.netmode", alg));
    if (alg !== undefined && alg >= 2) {
      row("Ueco_net", (gv(f, "LCD_UAccMinNetGen") === undefined) ? "—" : (((f["LCD_UAccMinNetGen"] << sh) + (gv(f, "LCD_UAccMinNetGen_dop") || 0)) / 10) + " " + U("V"));
    }
    if (alg === 3) {
      function hhmm(v) { if (v === undefined) { return "—"; } if (v === 0) { return t("mi.off_full"); } return pad2(v >> 3) + ":" + pad2((v & 7) * 10); }
      row("tzone_start", hhmm(gv(f, "LCD_TimeEcoStart")));
      row("tzone_end", hhmm(gv(f, "LCD_TimeEcoEnd")));
    }
    var pump = gv(f, "LCD_PercentECOMinGen");
    row("Min_u_pumping", pump === undefined ? "—" : (pump === 255 ? t("mi.off_full") : pump + "%"));
    row("acc_type", gv(f, "LCD_AccType") === undefined ? "—" : L("mi.acctype", f["LCD_AccType"]));
    row("c_acc_array", gv(f, "LCD_CAcc") === undefined ? "—" : ((f["LCD_CAcc"] * 25) >> sh) + " " + U("Ah"));
    row("1st_step_i", gv(f, "LCD_CIChargeStart") === undefined ? "—" : (f["LCD_CIChargeStart"] / 100) + " C");
    row("2nd_step_i", gv(f, "LCD_CIChargeEnd") === undefined ? "—" : (f["LCD_CIChargeEnd"] / 100) + " C");
    row("acc_chg_alg", gv(f, "LCD_ChargeAlg") === undefined ? "—" : L("mi.chgalg", f["LCD_ChargeAlg"]));
    row("u_acc_stop", gv(f, "LCD_UAccChMax") === undefined ? "—" : (((f["LCD_UAccChMax"] << sh) + (gv(f, "LCD_UAccChMax_dop") || 0)) / 10) + " " + U("V"));
    row("u_acc_buf", gv(f, "LCD_UAccChBUF") === undefined ? "—" : (((f["LCD_UAccChBUF"] << sh) + (gv(f, "LCD_UAccChBUF_dop") || 0)) / 10) + " " + U("V"));
    row("u_acc_start", gv(f, "LCD_UAccChStart") === undefined ? "—" : (((f["LCD_UAccChStart"] << sh) + (gv(f, "LCD_UAccChStart_dop") || 0)) / 10) + " " + U("V"));
    return rows;
  }

  function renderMain(f) {
    var el = document.getElementById("mi-main"); if (!el) { return; }
    var rows = buildMain(f), h = '<table class="mi-main"><tbody>';
    for (var i = 0; i < rows.length; i++) { h += '<tr><td class="mi-main-l">' + esc(t("mi." + rows[i][0])) + '</td><td class="mi-main-v">' + esc(rows[i][1]) + '</td></tr>'; }
    el.innerHTML = h + "</tbody></table>";
  }

  // --- «Системные»: все ячейки по группам каталога (mapsettings/catalog.json) ---
  function nameOf(n) { return (I18N["mi.n." + n] !== undefined) ? I18N["mi.n." + n] : (MI.fields[n].n || n); }
  function bitName(n, b) { var k = "mi.b." + n + "." + b; if (I18N[k] !== undefined) { return I18N[k]; } var e = MI.fields[n].bits || []; for (var i = 0; i < e.length; i++) { if (e[i][0] === b) { return e[i][1]; } } return "bit " + b; }
  function subName(n, i) { var k = "mi.s." + n + "." + i; if (I18N[k] !== undefined) { return I18N[k]; } var s = MI.fields[n].sub[i]; return s.l || ("#" + i); }
  function enumText(n, val) { var k = "mi.e." + n + "." + val; if (I18N[k] !== undefined) { return I18N[k]; } var e = MI.fields[n].enum; return (e && e[String(val)] !== undefined) ? e[String(val)] : String(val); }
  function rowHTML(name, addr, val, unit, title, na) {
    return '<tr><td class="mi-name"' + (title ? ' title="' + esc(title) + '"' : '') + '>' + esc(name) + '</td>' +
      '<td class="mi-addr">' + addr + '</td>' +
      '<td class="mi-val' + (na ? " mi-na" : "") + '">' + esc(val) + '</td>' +
      '<td class="mi-unit">' + (unit ? esc(U(unit)) : "") + '</td></tr>';
  }
  function sysRow(n, f) {
    var meta = MI.fields[n], raw = f[n];
    var addr = "0x" + ("00" + meta.a.toString(16).toUpperCase()).slice(-3);
    var name = nameOf(n), title = meta.d || "", kind = meta.k || "num", na = (typeof raw !== "number");
    if (kind === "bits") {
      var h = '<tr class="mi-subhdr"><td colspan="4">' + esc(name) + '</td></tr>';
      for (var i = 0; i < meta.bits.length; i++) {
        var b = meta.bits[i][0], on = (!na && ((raw >> b) & 1) !== 0);
        h += rowHTML(bitName(n, b), addr, on ? t("mi.yes") : t("mi.no"), "", title, na);
      }
      return h;
    }
    if (kind === "sub") {
      var h2 = '<tr class="mi-subhdr"><td colspan="4">' + esc(name) + '</td></tr>';
      for (var j = 0; j < meta.sub.length; j++) {
        var s = meta.sub[j], mask = (1 << s.bits) - 1, vv = na ? undefined : (((raw >> s.lsb) & mask) + (s.off || 0));
        var txt = "—";
        if (vv !== undefined) {
          var ek = "mi.e." + n + "." + j + "." + vv;
          if (I18N[ek] !== undefined) { txt = I18N[ek]; }
          else if (s.enum && s.enum[String(vv)] !== undefined) { txt = s.enum[String(vv)]; }
          else { txt = String(vv); }
        }
        h2 += rowHTML(subName(n, j), addr, txt, "", title, vv === undefined);
      }
      return h2;
    }
    if (kind === "enum") {
      var ce = combo(n, f), ve = (ce ? ce.v : (na ? "—" : enumText(n, raw))), ue = (ce && ce.u) ? ce.u : meta.u;
      return rowHTML(name, addr, ve, ue, title, na);
    }
    var c = combo(n, f), val, unit = meta.u;
    if (c) { val = c.v; if (c.u) { unit = c.u; } }
    else { var disp = conv(meta.c, raw, f); val = (disp === null ? "—" : disp); }
    return rowHTML(name, addr, val, unit, title, na);
  }
  function miBmsTable(f) {
    var bmsf = gv(f, "LCD_BMSFunk") || 0, n = gv(f, "BMS_Num") || 0;
    if ((bmsf !== 1 && bmsf !== 3) || n < 1 || n > 32) { return ""; }
    var head = "", body = "", min = 1e9, max = -1e9, i, vs = [];
    for (i = 0; i < n; i++) { var L = gv(f, "UakbCell_L" + i) || 0, H = gv(f, "UakbCell_H" + i) || 0; var v = ((L & 0xff) | ((H & 0x7f) << 8)) / 100; vs.push(v); if (v < min) { min = v; } if (v > max) { max = v; } }
    for (i = 0; i < n; i++) {
      var raw = gv(f, "TakbCell" + i) || 0, tt = (raw === 255) ? "—" : (raw - 50);
      var q = gv(f, "Q_Cell" + i) || 0, cur = (q / 100 * vs[i]).toFixed(2);
      var bg = (min !== max) ? (vs[i] === min ? "#5bc0de" : (vs[i] === max ? "#e48683" : "#0d5637")) : "#0d5637";
      head += '<th style="background:' + bg + ';color:#fff">' + t("p1.bms") + ' ' + (i + 1) + '</th>';
      body += '<td>' + vs[i].toFixed(2) + U("V") + '<br>' + cur + U("A") + '<br>' + tt + '&deg;C</td>';
    }
    return '<div class="mi-scroll"><table class="p1-tbl"><tr>' + head + '</tr><tr>' + body + '</tr></table></div>';
  }
  function mpptTable(f) {
    var bmsf = gv(f, "LCD_BMSFunk") || 0, n = gv(f, "LCD_MPPTNum") || 0;
    if ((bmsf !== 2 && bmsf !== 3) || n < 1 || n > 16) { return ""; }
    var cells = "";
    for (var i = 0; i < n; i++) { var L = gv(f, "I_Akb_MPPT_L" + i) || 0, H = gv(f, "I_Akb_MPPT_H" + i) || 0; var cur = ((L & 0xff) | ((H & 0x7f) << 8)) / 16; var tt = (gv(f, "Takb_MPPT" + i) || 0) - 50; cells += '<td><b>' + (i + 1) + '</b> ' + cur.toFixed(2) + U("A") + ' / ' + tt + '&deg;C</td>'; }
    return '<div class="mi-scroll"><table class="p1-tbl mppt"><tr>' + cells + '</tr></table></div>';
  }
  function renderSystem(f) {
    var el = document.getElementById("mi-system"); if (!el) { return; }
    var by = sysRows(), h = "";
    MI.groups.forEach(function (gname, gi) {
      var rows = by[gi];
      if (!rows || !rows.length) { return; }
      h += '<h2 class="mi-group">' + esc(t("mi.g." + gi)) + '</h2>';
      h += '<div class="mi-scroll"><table class="mi-tbl"><thead><tr><th>' + esc(t("mi.col_param")) + '</th><th>' + esc(t("mi.col_addr")) + '</th><th>' + esc(t("mi.col_value")) + '</th><th>' + esc(t("mi.col_unit")) + '</th></tr></thead><tbody>';
      rows.forEach(function (n) { h += sysRow(n, f); });
      h += '</tbody></table></div>';
    });
    // Отдельные таблицы массивов (BMS-банки, КЭС) — вне групп каталога.
    var bms = miBmsTable(f), mppt = mpptTable(f);
    if (bms) { h += '<h2 class="mi-group">' + esc(t("p1.bms")) + '</h2>' + bms; }
    if (mppt) { h += '<h2 class="mi-group">' + esc(t("mi.mppt_group")) + '</h2>' + mppt; }
    el.innerHTML = h;
  }

  var infoCache = null;
  function renderInfo(p) {
    var infoPane = document.getElementById("tab-info");
    if (!infoPane || infoPane.hidden) { return; }
    infoCache = p;
    renderMain(p.fields);
    renderSystem(p.fields);
  }
  function initSubTabs() {
    var tabs = document.querySelectorAll(".mi-subtab");
    var panes = { main: document.getElementById("mi-main"), system: document.getElementById("mi-system") };
    function show(name) {
      Array.prototype.forEach.call(tabs, function (b) { b.classList.toggle("active", b.getAttribute("data-subtab") === name); });
      Object.keys(panes).forEach(function (k) { if (panes[k]) { panes[k].hidden = (k !== name); } });
      if (infoCache) { renderMain(infoCache.fields); renderSystem(infoCache.fields); }
    }
    Array.prototype.forEach.call(tabs, function (b) { b.addEventListener("click", function () { show(b.getAttribute("data-subtab")); }); });
  }

  function showErr(m) { if (!errBox) { return; } if (m) { errBox.textContent = m; errBox.hidden = false; } else { errBox.hidden = true; } }

  function tick(force) {
    if (pause && pause.checked && !force) { return; }
    inflight = new AbortController();
    fetch("api/map/raw?id=" + encodeURIComponent(ID) + "&gen=" + encodeURIComponent(window.MAP_GEN || 0), { cache: "no-store", signal: inflight.signal })
      .then(function (r) { if (r.status === 404) { throw { gone: true }; } if (r.status === 401) { throw { auth: true }; } if (!r.ok) { throw new Error("HTTP " + r.status); } return r.arrayBuffer(); })
      .then(function (buf) { showErr(""); var p = parse(buf); lastP = p; renderPanel(p); if (infoActive) { renderInfo(p); } if (!debugHidden) { pre.textContent = JSON.stringify(p); postToPanel2(buf); } window.MAP_LAST_FIELDS = p.fields; window.MAP_GEN = p.gen; window.MAP_MODE = modeNum(p.fields); if (typeof window.mapControlRaw === "function") { try { window.mapControlRaw(p.fields, p.gen); } catch (e) { /* ignore */ } } })
      .catch(function (e) { if (e && e.auth) { window.location.href = "login"; return; } if (e && e.gone) { showErr(t("map.gone")); } else if (!(e && e.name === "AbortError")) { showErr(t("map.connection_lost")); } });
  }
  function start() { if (timer) { return; } tick(); timer = setInterval(tick, INTERVAL); }
  function stop() { if (timer) { clearInterval(timer); timer = null; } if (inflight) { inflight.abort(); inflight = null; } }
  if (pause) { pause.addEventListener("change", function () { if (pause.checked) { stop(); } else { start(); } }); }

  function initTabs() {
    var tabs = document.querySelectorAll(".map-tab");
    var panes = { monitor: document.getElementById("tab-monitor"), info: document.getElementById("tab-info"), control: document.getElementById("tab-control") };
    var TAB_KEY = "malina.map.tab";
    function show(name) {
      Array.prototype.forEach.call(tabs, function (b) { b.classList.toggle("active", b.getAttribute("data-tab") === name); });
      Object.keys(panes).forEach(function (k) { if (panes[k]) { panes[k].hidden = (k !== name); } });
      infoActive = (name === "info");
      if (infoActive && lastP) { renderInfo(lastP); }
    }
    Array.prototype.forEach.call(tabs, function (b) { b.addEventListener("click", function () {
      var nm = b.getAttribute("data-tab"); show(nm);
      try { window.localStorage.setItem(TAB_KEY, nm); } catch (e) { /* ignore */ }
    }); });
    var init = "monitor";
    try { var sv = window.localStorage.getItem(TAB_KEY); if (sv === "monitor" || sv === "info" || sv === "control") { init = sv; } } catch (e) { /* ignore */ }
    show(init);
  }
  initTabs();
  initSubTabs();

  function postToPanel2(buf) {
    var fr = document.getElementById("panel2frame");
    if (!fr || !fr.contentWindow) { return; }
    try { fr.contentWindow.postMessage({ type: "map-raw", id: ID, ts: Date.now(), buf: buf.slice(0) }, location.origin); } catch (err) { /* ignore */ }
  }
  function sizeFrames() {
    Array.prototype.forEach.call(document.querySelectorAll(".legacy-frame"), function (fr) {
      try { var d = fr.contentDocument; if (d && d.documentElement) { fr.style.height = Math.max(160, d.documentElement.scrollHeight) + "px"; } } catch (err) { /* ignore */ }
    });
  }
  Array.prototype.forEach.call(document.querySelectorAll(".legacy-frame"), function (fr) { fr.addEventListener("load", sizeFrames); });
  setInterval(sizeFrames, 1000);

  // «Скрыть отладочные панели»: гасит панели 2/3 (вместе с заголовками),
  // заголовок панели 1 и memo с JSON, останавливая их фоновую механику.
  // Состояние хранится на клиенте; по умолчанию — включено.
  var DBG_KEY = "malina.map.hideDebug";
  var panel2Src = f2 ? f2.getAttribute("src") : "";
  var panel3Src = f3 ? f3.getAttribute("src") : "";
  function setDebugHidden(on) {
    debugHidden = !!on;
    [hdTarget, hdTransit, wrap2, hdLegacy, wrap3, pre].forEach(function (el) { if (el) { el.hidden = debugHidden; } });
    // Полностью останавливаем фоновую механику панелей 2/3: разгружаем iframe
    // (panel2 не пересчитывается — postToPanel2 не вызывается; panel3 не ходит в свой API).
    if (f2) { f2.src = debugHidden ? "about:blank" : panel2Src; }
    if (f3) { f3.src = debugHidden ? "about:blank" : panel3Src; }
  }
  if (hideDebug) {
    var saved = null;
    try { saved = window.localStorage.getItem(DBG_KEY); } catch (e) { /* ignore */ }
    hideDebug.checked = (saved === null) ? true : (saved === "1");
    setDebugHidden(hideDebug.checked);
    hideDebug.addEventListener("change", function () {
      setDebugHidden(hideDebug.checked);
      try { window.localStorage.setItem(DBG_KEY, hideDebug.checked ? "1" : "0"); } catch (e) { /* ignore */ }
    });
  }

  window.MAP_FORCE_REFRESH = function () { tick(true); };
  start();
})();
