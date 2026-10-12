// Главная страница — живая сводка по устройствам (МАП + КЭС).
// Данные — GET /api/mainlist (сервер отдаёт сырые значения в единых физ.
// единицах + флаг nd; раскраска и округление — здесь, см. mainlist.go).
//
// Ключевое поведение:
//  - обновление раз в секунду (INTERVAL = 1000);
//  - стабильные позиции плиток: привязаны к устройству по ключу map:<id>/kes:<id>,
//    НЕ к порядку в ответе. Новая плитка добавляется только при первом появлении;
//  - устройства, пропавшие из ответа (lost) или с online=false (stale),
//    гасятся «туманом» (полупрозрачная заслонка) на своём месте, не удаляются
//    и не переезжают. При возврате устройства туман снимается.
(function () {
  "use strict";
  var I18N = window.I18N || {};
  var INTERVAL = 1000;

  // Единицы измерения по физвеличинам ячеек (для всех блоков плитки).
  var UNIT = { u: "\u0412", f: "\u0413\u0446", i: "\u0410", p: "\u0412\u0442", t: "\u00b0C", soc: "%" };

  // ---- Пороговая расцветка (зеркало docs/mainpage.md §8.2/§8.3). Сервер
  //      отдаёт сырые значения в единых физ. единицах, цвет — здесь, на клиенте.
  function colorVolts(v) {  // U сети/нагрузки
    if (v > 235) { return "red"; }
    if (v < 210) { return "blue"; }
    return "green";
  }
  function colorFreq(v) {  // F сети/нагрузки
    if (v > 50.1) { return "red"; }
    if (v < 49.9) { return "blue"; }
    return "green";
  }
  function colorGridPower(v) {  // I/P сети: <0 отдача → green, >0 → red, 0 → blue
    if (v < 0) { return "green"; }
    if (v > 0) { return "red"; }
    return "blue";
  }
  function colorBatCur(v) {  // I/P АКБ: >0 заряд → red, <0 разряд → green, 0 → blue
    if (v > 0) { return "red"; }
    if (v < 0) { return "green"; }
    return "blue";
  }
  function colorLoadPower(v) {  // I/P нагрузки: >0 потребление → red, <0 → green, 0 → blue
    if (v > 0) { return "red"; }
    if (v < 0) { return "green"; }
    return "blue";
  }
  function colorTempTrans(v) {  // T транзисторов/тор/панели КЭС
    if (v < 30) { return "blue"; }
    if (v < 50) { return "green"; }
    if (v < 70) { return "orange"; }
    return "red";
  }
  function colorTempBat(v) {  // T АКБ
    if (v < 0) { return "blue"; }
    if (v < 15) { return "darkblue"; }
    if (v < 35) { return "green"; }
    if (v < 50) { return "orange"; }
    return "red";
  }
  function colorSoc(v) {  // §8.3
    if (v < 10) { return "darkblue"; }
    if (v < 30) { return "blue"; }
    if (v < 70) { return "green"; }
    if (v < 90) { return "orange"; }
    return "red";
  }

  // Расцветка ячейки по колонке (kind) и физвеличине (field).
  //  kind: "grid"|"load" — МАП сеть/нагрузка; "batt"|"batt-kes" — АКБ;
  //        "panel" — панели КЭС (u/i/p бежевые, t — по темпу).
  function fieldColor(kind, field, v, soc) {
    if (field === "soc") { return colorSoc(v); }
    if (kind === "panel") {
      return field === "t" ? colorTempTrans(v) : "beige";
    }
    if (kind === "batt" || kind === "batt-kes") {
      if (field === "u") { return colorSoc(soc); }
      if (field === "t") { return colorTempBat(v); }
      if (field === "i" || field === "p") {
        return kind === "batt-kes" ? "beige" : colorBatCur(v);
      }
      return "";
    }
    // grid / load (МАП)
    if (field === "u") { return colorVolts(v); }
    if (field === "f") { return colorFreq(v); }
    if (field === "i" || field === "p") {
      return kind === "load" ? colorLoadPower(v) : colorGridPower(v);
    }
    if (field === "t") { return colorTempTrans(v); }
    return "";
  }

  var mapSection = document.getElementById("homeMaps");
  var mapWrap = document.getElementById("homeMapTiles");
  var kesSection = document.getElementById("homeKes");
  var kesWrap = document.getElementById("homeKesTiles");
  var errBox = document.getElementById("homeErr");

  // Реестр плиток: key ("map:3"/"kes:5") -> элемент .home-tile.
  var tiles = {};
  var timer = null;

  function t(k) { return I18N[k] || k; }
  function fmt(v, d) {
    if (typeof v === "number" && isFinite(v)) {
      return v.toFixed(d === undefined ? 1 : d);
    }
    return "\u2014";
  }
  function esc(s) {
    return String(s).replace(/[&<>"']/g, function (c) {
      return { "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" }[c];
    });
  }
  // Значение с заданным числом знаков (dec) и единицей измерения (field).
  // v.v — сырое значение в единых физ. единицах; dec задаёт округление.
  function valueHTML(v, field, dec, color) {
    if (v && v.nd) {
      return '<span class="home-cell-v c-gray">' + esc(t("home.nd")) + "</span>";
    }
    var cls = color ? (" c-" + color) : "";
    var val = (v && typeof v.v === "number") ? fmt(v.v, dec) : "\u2014";
    var unit = UNIT[field] || "";
    var u = unit ? (' <span class="home-unit">' + esc(unit) + "</span>") : "";
    return '<span class="home-cell-v' + cls + '">' + val + u + "</span>";
  }
  function cellHTML(label, v, field, dec, kind, soc) {
    var color = (v && !v.nd) ? fieldColor(kind, field, v.v, soc) : "";
    return '<div class="home-cell"><span class="home-cell-l">' + esc(label) +
      "</span>" + valueHTML(v, field, dec, color) + "</div>";
  }
  // SOC — отдельная ячейка колонки АКБ (корень плитки, не блок).
  function socHTML(soc) {
    var lab = esc(t("home.cell.soc"));
    if (soc === null || soc === undefined) {
      return '<div class="home-cell"><span class="home-cell-l">' + lab +
        '</span><span class="home-cell-v c-gray">' + esc(t("home.nd")) + "</span></div>";
    }
    var cls = " c-" + colorSoc(soc);
    return '<div class="home-cell"><span class="home-cell-l">' + lab +
      '</span><span class="home-cell-v' + cls + '">' + Math.round(soc) + " %</span></div>";
  }

  // Колонка из перечня [labelKey, fieldKey, decimals] по блоку mainBlock.
  // Мощность — целое (0 знаков); ток АКБ — 2 знака; остальное — 1 знак.
  // (десятичные задаются в items конкретной колонки).
  function colHTML(title, block, items, kind, soc) {
    var h = '<div class="home-col"><div class="home-col-title">' + esc(title) + "</div>";
    items.forEach(function (it) {
      if (it[1] === "soc") { h += socHTML(soc); return; }
      h += cellHTML(t(it[0]), block[it[1]], it[1], it[2], kind, soc);
    });
    return h + "</div>";
  }

  function cardHTML(d, isKes) {
    var h = '<div class="home-title">' + esc(d.title) + "</div>" +
      '<div class="home-status">' + esc(d.mode || "\u2014") + "</div>" +
      '<div class="home-cols">';
    if (isKes) {
      h += colHTML(t("home.col.panel"), d.panel, [
        ["home.cell.u", "u"], ["home.cell.i", "i"], ["home.cell.p", "p", 0], ["home.cell.t", "t"]
      ], "panel", d.soc);
      // Ток АКБ — 2 знака; u/i/p бежевые, t — по темпу АКБ; SOC — как у МАП.
      h += colHTML(t("home.col.batt"), d.batt, [
        ["home.cell.u", "u"], ["home.cell.soc", "soc"], ["home.cell.i", "i", 2],
        ["home.cell.p", "p", 0], ["home.cell.t", "t"]
      ], "batt-kes", d.soc);
    } else {
      h += colHTML(t("home.col.grid"), d.grid, [
        ["home.cell.u", "u"], ["home.cell.f", "f"], ["home.cell.i", "i"],
        ["home.cell.p", "p", 0], ["home.cell.t", "t"]
      ], "grid", d.soc);
      // Ток АКБ — 2 знака; сеть/нагрузка — 1 знак.
      h += colHTML(t("home.col.batt"), d.batt, [
        ["home.cell.u", "u"], ["home.cell.soc", "soc"], ["home.cell.i", "i", 2],
        ["home.cell.p", "p", 0], ["home.cell.t", "t"]
      ], "batt", d.soc);
      h += colHTML(t("home.col.load"), d.load, [
        ["home.cell.u", "u"], ["home.cell.f", "f"], ["home.cell.i", "i"],
        ["home.cell.p", "p", 0], ["home.cell.t", "t"]
      ], "load", d.soc);
    }
    return h + "</div></div>";
  }

  function setFog(el, state) {
    var fog = el.querySelector(".home-fog");
    var txt = fog.querySelector(".home-fog-text");
    el.classList.toggle("fogged-stale", state === "stale");
    el.classList.toggle("fogged-lost", state === "lost");
    // В «тумане» плашка некликабельна (нет перехода на страницу устройства).
    el.classList.toggle("nolink", !!state);
    if (!state) {
      fog.hidden = true;
      return;
    }
    fog.hidden = false;
    txt.textContent = state === "stale" ? t("home.status.stale") : t("home.status.lost");
  }

  // Обновление одного раздела: in-place обновление, добавление новых, затуманивание
  // пропавших и устаревших, без изменения позиций существующих плиток.
  function applySection(secEl, wrapEl, arr, prefix, isKes) {
    var seen = {};
    (arr || []).forEach(function (d) {
      var key = prefix + d.id;
      seen[key] = true;
      var el = tiles[key];
      if (!el) {
        el = document.createElement("div");
        el.className = "home-tile";
        el.setAttribute("data-key", key);
        // Ссылка на страницу устройства (МАП/КЭС) по id+gen, как пункты меню.
        var page = isKes ? "kes" : "map";
        var url = page + "?id=" + encodeURIComponent(d.id) +
          "&gen=" + encodeURIComponent(d.gen || "");
        el.innerHTML = '<a class="home-tile-link" href="' + url + '">' +
          '<div class="home-card"></div></a>' +
          '<div class="home-fog" hidden><span class="home-fog-text"></span></div>';
        wrapEl.appendChild(el); // только в конец — новая плитка
        tiles[key] = el;
      }
      el.querySelector(".home-card").innerHTML = cardHTML(d, isKes);
      setFog(el, d.online ? "" : "stale");
    });
    // Пропавшие из ответа устройства — затуманиваются, но не удаляются.
    Object.keys(tiles).forEach(function (k) {
      if (k.indexOf(prefix) === 0 && !seen[k]) {
        setFog(tiles[k], "lost");
      }
    });
    // Раздел виден, если в нём есть хоть одна плитка (в т.ч. погашенная).
    var hasAny = Object.keys(tiles).some(function (k) { return k.indexOf(prefix) === 0; });
    secEl.hidden = !hasAny;
  }

  function showErr(m) {
    if (!errBox) { return; }
    if (m) { errBox.textContent = m; errBox.hidden = false; } else { errBox.hidden = true; }
  }

  function stop() {
    if (timer) { clearInterval(timer); timer = null; }
  }

  function tick() {
    fetch("api/mainlist", { cache: "no-store", credentials: "same-origin" })
      .then(function (r) {
        if (r.status === 401) { window.location.href = "login"; return null; }
        if (!r.ok) { throw new Error("HTTP " + r.status); }
        return r.json();
      })
      .then(function (d) {
        if (!d) { return; }
        showErr("");
        applySection(mapSection, mapWrap, d.maps || [], "map:", false);
        applySection(kesSection, kesWrap, d.kes || [], "kes:", true);
      })
      .catch(function (e) {
        showErr(t("home.err"));
      });
  }

  function start() {
    if (timer) { return; }
    tick();
    timer = setInterval(tick, INTERVAL);
  }

  start();
})();