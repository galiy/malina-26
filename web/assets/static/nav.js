// Динамические подпункты меню «МАП», «КЭС», «БМС».
//
// Ссылки — только корневые абсолютные. На вложенной странице
// (/stats/tables, /bms/settings) относительный "bms?..." браузер разрешил бы
// от каталога текущей страницы: /stats/bms или /bms/bms (404). Требования —
// docs/ui-navigation.md.
//
// Опрос: сразу (если вкладка видима), 6 раз по 5 с, далее раз в 30 с.
// В фоне не опрашиваем; при focus/visibility — один опрос с debounce 2 с.
// На эндпоинт — не более одного запроса в полёте.
(function () {
  "use strict";
  var boxes = {
    map: document.getElementById("mapMenuItems"),
    kes: document.getElementById("kesMenuItems"),
    bms: document.getElementById("bmsMenuItems")
  };
  var endpoints = [
    ["map", "/api/map/devices"],
    ["kes", "/api/kes/devices"],
    ["bms", "/api/bms/devices"]
  ];
  var FAST_MS = 5000;
  var FAST_COUNT = 6;
  var SLOW_MS = 30000;
  var EVENT_GAP_MS = 2000;
  var REQUEST_TIMEOUT_MS = 10000;

  var inflight = {}; // type -> token активного запроса; 0/undefined — свободен
  var seq = 0;
  var lastPollAt = 0;
  var fastLeft = FAST_COUNT;

  function esc(s) {
    return String(s).replace(/[&<>"']/g, function (c) {
      return { "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" }[c];
    });
  }
  function label(d) {
    if (d.type === "bms") { return d.serial || d.bms_type_label || d.bms_type || ""; }
    return d.label || "";
  }
  function href(d) {
    var page = d.type === "bms" ? "bms" : d.type;
    return "/" + page + "?id=" + encodeURIComponent(d.id) +
      "&gen=" + encodeURIComponent(d.gen) +
      "&serial=" + encodeURIComponent(d.serial || "");
  }
  function render(box, list, type) {
    if (!box) { return; }
    var arr = (list || []).filter(function (d) { return d && d.type === type; });
    var html = "";
    for (var i = 0; i < arr.length; i++) {
      html += '<a class="dropdown-sub" href="' + href(arr[i]) + '">' + esc(label(arr[i])) + "</a>";
    }
    box.innerHTML = html; // подпункты — перед статической «Настройкой» (она вне box)
    box.setAttribute("data-nav-loaded", "1"); // маркер для smoke-теста
  }
  function load(type, url) {
    if (inflight[type]) { return; }
    var token = ++seq;
    inflight[type] = token;
    var ctl = (typeof AbortController !== "undefined") ? new AbortController() : null;
    var guard = setTimeout(function () {
      if (ctl) { ctl.abort(); }
      if (inflight[type] === token) { inflight[type] = 0; }
    }, REQUEST_TIMEOUT_MS);
    fetch(url, {
      cache: "no-store",
      credentials: "same-origin",
      signal: ctl ? ctl.signal : undefined
    })
      .then(function (r) { if (!r.ok) { throw 0; } return r.json(); })
      .then(function (d) { render(boxes[type], d, type); })
      .catch(function () { /* тихо: не мешаем странице */ })
      .then(function () {
        clearTimeout(guard);
        if (inflight[type] === token) { inflight[type] = 0; }
      });
  }
  function poll() {
    if (document.hidden) { return; }
    lastPollAt = Date.now();
    endpoints.forEach(function (p) { load(p[0], p[1]); });
  }
  function eventPoll() {
    if (document.hidden) { return; }
    if (Date.now() - lastPollAt < EVENT_GAP_MS) { return; }
    poll();
  }
  function schedule() {
    var delay = SLOW_MS;
    if (fastLeft > 0) {
      delay = FAST_MS;
      fastLeft--;
    }
    setTimeout(function () {
      poll();
      schedule();
    }, delay);
  }
  function start() {
    if (!document.hidden) { poll(); }
    schedule();
  }

  window.addEventListener("focus", eventPoll);
  document.addEventListener("visibilitychange", function () {
    if (!document.hidden) { eventPoll(); }
  });
  start();
})();
