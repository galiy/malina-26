// Динамическое меню «МАП» и «КЭС»: раз в 30 с обновляют списки устройств из
// /api/map/devices и /api/kes/devices (алгоритм заполнения/обновления одинаков).
// Пункты — ссылки на страницу конкретного устройства (/map?id&gen, /kes?id&gen).
(function () {
  "use strict";
  var wrap = document.getElementById("mapMenuWrap");
  var items = document.getElementById("mapMenuItems");
  var kesWrap = document.getElementById("kesMenuWrap");
  var kesItems = document.getElementById("kesMenuItems");

  function esc(s) {
    return String(s).replace(/[&<>"']/g, function (c) {
      return { "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" }[c];
    });
  }
  function renderList(w, box, list, type, prefix) {
    if (!w || !box) { return; }
    var arr = (list || []).filter(function (d) { return d && d.type === type; });
    if (!arr.length) { w.hidden = true; box.innerHTML = ""; return; }
    w.hidden = false;
    var html = "";
    for (var i = 0; i < arr.length; i++) {
      html += '<a class="dropdown-sub" href="' + prefix + '?id=' +
        encodeURIComponent(arr[i].id) + '&gen=' + encodeURIComponent(arr[i].gen) +
        '&serial=' + encodeURIComponent(arr[i].serial || "") + '">' +
        esc(arr[i].label) + "</a>";
    }
    box.innerHTML = html;
  }
  function poll() {
    fetch("api/map/devices", { cache: "no-store", credentials: "same-origin" })
      .then(function (r) { if (!r.ok) { throw 0; } return r.json(); })
      .then(function (d) { renderList(wrap, items, d, "map", "map"); })
      .catch(function () { /* тихо: не мешаем странице */ });
    fetch("api/kes/devices", { cache: "no-store", credentials: "same-origin" })
      .then(function (r) { if (!r.ok) { throw 0; } return r.json(); })
      .then(function (d) { renderList(kesWrap, kesItems, d, "kes", "kes"); })
      .catch(function () { /* тихо */ });
  }
  // Сразу + чаще на старте (устройства могут появиться/флапать), далее раз в 30 с.
  poll();
  var fast = 0;
  var t = setInterval(function () { poll(); if (++fast >= 6) { clearInterval(t); } }, 5000);
  setInterval(poll, 30000);
  window.addEventListener("focus", poll);
  document.addEventListener("visibilitychange", function () { if (!document.hidden) { poll(); } });
})();