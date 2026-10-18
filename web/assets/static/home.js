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
  function colorBatCur(v) {  // I/P АКБ: >0 разряд → green, <0 заряд → red, 0 → blue
    if (v > 0) { return "green"; }
    if (v < 0) { return "red"; }
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
  var bmsSection = document.getElementById("homeBms");
  var bmsWrap = document.getElementById("homeBmsTiles");
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
  // Строка таблицы колонки: [наименование влево][значение вправо][единицы влево].
  function valueHTML(v, field, dec, color) {
    if (v && v.nd) {
      return '<span class="home-cell-v c-gray">' + esc(t("home.nd")) + "</span>";
    }
    var cls = color ? (" c-" + color) : "";
    var val = (v && typeof v.v === "number") ? fmt(v.v, dec) : "\u2014";
    return '<span class="home-cell-v' + cls + '">' + val + "</span>";
  }
  function cellHTML(label, v, field, dec, kind, soc) {
    var color = (v && !v.nd) ? fieldColor(kind, field, v.v, soc) : "";
    return '<tr><td class="home-cell-l">' + esc(label) + "</td>" +
      '<td class="home-cell-v-wrap">' + valueHTML(v, field, dec, color) + "</td>" +
      '<td class="home-cell-u">' + esc(UNIT[field] || "") + "</td></tr>";
  }
  // SOC — отдельная строка колонки АКБ (корень плитки, не блок).
  function socHTML(soc) {
    var lab = esc(t("home.cell.soc"));
    if (soc === null || soc === undefined) {
      return '<tr><td class="home-cell-l">' + lab +
        '</td><td class="home-cell-v-wrap"><span class="home-cell-v c-gray">' +
        esc(t("home.nd")) + "</span></td><td class=\"home-cell-u\"></td></tr>";
    }
    var cls = " c-" + colorSoc(soc);
    return '<tr><td class="home-cell-l">' + lab +
      '</td><td class="home-cell-v-wrap"><span class="home-cell-v' + cls + '">' +
      Math.round(soc) + "</span></td><td class=\"home-cell-u\">%</td></tr>";
  }

  // Колонка из перечня [labelKey, fieldKey, decimals] по блоку mainBlock.
  // Мощность — целое (0 знаков); ток АКБ — 2 знака; остальное — 1 знак.
  // (десятичные задаются в items конкретной колонки).
  function colHTML(title, block, items, kind, soc) {
    var h = '<div class="home-col"><div class="home-col-title">' + esc(title) +
      '</div><table class="home-cols-table"><tbody>';
    items.forEach(function (it) {
      if (it[1] === "soc") { h += socHTML(soc); return; }
      h += cellHTML(t(it[0]), block[it[1]], it[1], it[2], kind, soc);
    });
    return h + "</tbody></table></div>";
  }

  // SOC на строке статуса (КЭС): вправо, по цвету §8.3.
  function statusSocHTML(soc) {
    var lab = '<span class="home-status-soc-l">' + esc(t("home.cell.soc")) + "</span> ";
    if (soc === null || soc === undefined) {
      return '<span class="home-status-soc c-gray">' + lab + esc(t("home.nd")) + "</span>";
    }
    var cls = " c-" + colorSoc(soc);
    return '<span class="home-status-soc' + cls + '">' + lab + Math.round(soc) + " %</span>";
  }

  function cardHTML(d, mode) {
    var isKes = mode === "kes", isBms = mode === "bms";
    var modeHTML = '<span class="home-status-mode">' + esc(d.mode || "\u2014") + "</span>";
    var h;
    if (isBms) {
      // Таблица: «!» слева на обе строки (rowspan=2), справа заголовок/статус;
      // SOC — на строке статуса (прижат вправо).
      h = '<table class="bms-head"><tbody>' +
        '<tr><td class="bms-alert-cell" rowspan="2">' + bmsAlert(d.bms_fault) + "</td>" +
        '<td class="home-title">' + esc(d.title) + "</td></tr>" +
        '<tr><td><div class="home-status">' + modeHTML + statusSocHTML(d.soc) + "</div></td></tr>" +
        "</tbody></table>";
    } else {
      h = '<div class="home-title">' + esc(d.title) + "</div>" +
        '<div class="home-status">' + modeHTML + (isKes ? statusSocHTML(d.soc) : "") + "</div>";
    }
    h += '<div class="home-cols">';
    if (isKes) {
      h += colHTML(t("home.col.panel"), d.panel, [
        ["home.cell.u", "u"], ["home.cell.i", "i"], ["home.cell.p", "p", 0], ["home.cell.t", "t"]
      ], "panel", d.soc);
      // Ток АКБ — 2 знака; u/i/p бежевые, t — по темпу АКБ (SOC — на строке статуса).
      h += colHTML(t("home.col.batt"), d.batt, [
        ["home.cell.u", "u"], ["home.cell.i", "i", 2],
        ["home.cell.p", "p", 0], ["home.cell.t", "t"]
      ], "batt-kes", d.soc);
    } else if (isBms) {
      // БМС: колонка АКБ (U/I/P/T) + карта ячеек (правая половина), SOC — на статусе.
      h += colHTML(t("home.col.batt"), d.pack, [
        ["home.cell.u", "u"], ["home.cell.i", "i"],
        ["home.cell.p", "p", 0], ["home.cell.t", "t"]
      ], "batt-kes", d.soc);
      h += bmsCellsHTML(d);
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

  // Индикатор ошибки BMS: «!» в круге; при ошибке — красный (мигает), иначе серый.
  function bmsAlert(on) {
    return '<span class="bms-alert' + (on ? " on" : "") + '" title="' +
      esc(t(on ? "bms.alert" : "bms.ok")) + '">!</span>';
  }

  // Правая половина плитки БМС: min/max/разница + карта ячеек.
  // Рамки: жёлтая — балансировка, красная — ошибка ячейки; обе → чередование (CSS).
  function bmsCellsHTML(d) {
    var cells = d.cells_mv || [];
    var cols = 4; // 4 ячейки в ряд
    var u = esc(t("kes.v"));
    // Таблица: [номер левой] ячейки... [номер правой] в каждой строке.
    var tbl = '<table class="bms-cells-tbl"><tbody>';
    for (var r = 0; r < cells.length; r += cols) {
      var end = Math.min(r + cols, cells.length);
      tbl += '<tr><td class="bms-num">' + (r + 1) + "</td>";
      for (var i = r; i < end; i++) {
        var bit = (i < 31) ? (1 << i) : 0;
        var cls = "bms-cell";
        if (d.cell_prot_mask & bit) { cls += " cell-prot"; }       // защита ячейки — красная рамка + мигание
        else if (d.cell_warn_mask & bit) { cls += " cell-warn"; }  // предупреждение — оранжевая рамка
        if (d.balance_mask & bit) { cls += " cell-bal"; }
        if (cells[i] === d.cell_max) { cls += " cell-max"; } // максимум — яркий красный
        if (cells[i] === d.cell_min) { cls += " cell-min"; } // минимум — светло-синий
        var vv = (cells[i] / 1000).toFixed(3); // мВ → В
        tbl += '<td><span class="' + cls + '" title="' + (i + 1) + ": " + vv + " " +
          u + '">' + vv + "</span></td>";
      }
      tbl += '<td class="bms-num">' + end + "</td></tr>";
    }
    tbl += "</tbody></table>";
    var stat = '<div class="bms-cells-stat">' +
      esc(t("bms.cell.min")) + " " + (d.cell_min / 1000).toFixed(3) + " " + u + " \u00b7 " +
      esc(t("bms.cell.max")) + " " + (d.cell_max / 1000).toFixed(3) + " " + u + " \u00b7 " +
      esc(t("bms.cell.diff")) + " " + (d.cell_diff / 1000).toFixed(3) + " " + u + "</div>";
    return '<div class="home-col home-col-cells"><div class="home-col-title">' +
      esc(t("bms.cells")) + "</div>" + stat + tbl + "</div>";
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
  function applySection(secEl, wrapEl, arr, prefix, mode) {
    var seen = {};
    (arr || []).forEach(function (d) {
      var key = prefix + d.id;
      seen[key] = true;
      // Ссылка на страницу устройства (МАП/КЭС/БМС) по id+gen, как пункты меню.
      // serial добавляем только если сервер его отдал (иначе параметр пустой).
      // URL пересчитываем на каждом обновлении: gen/serial могли измениться.
      var url = mode + "?id=" + encodeURIComponent(d.id) +
        "&gen=" + encodeURIComponent(d.gen || "");
      if (d.serial) { url += "&serial=" + encodeURIComponent(d.serial); }
      var el = tiles[key];
      if (!el) {
        el = document.createElement("div");
        el.className = "home-tile";
        el.setAttribute("data-key", key);
        el.innerHTML = '<a class="home-tile-link" href="' + url + '">' +
          '<div class="home-card"></div></a>' +
          '<div class="home-fog" hidden><span class="home-fog-text"></span></div>';
        wrapEl.appendChild(el); // только в конец — новая плитка
        tiles[key] = el;
      } else {
        var link = el.querySelector("a.home-tile-link");
        if (link) { link.setAttribute("href", url); }
      }
      el.querySelector(".home-card").innerHTML = cardHTML(d, mode);
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
        applySection(mapSection, mapWrap, d.maps || [], "map:", "map");
        applySection(kesSection, kesWrap, d.kes || [], "kes:", "kes");
        applySection(bmsSection, bmsWrap, d.bms || [], "bms:", "bms");
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