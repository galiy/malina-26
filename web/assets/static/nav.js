// Динамическое меню «Мониторинг → МАП»: раз в 30 с обновляет список из /api/map/devices.
(function () {
  "use strict";
  var wrap = document.getElementById("mapMenuWrap");
  var items = document.getElementById("mapMenuItems");
  if (!wrap || !items) { return; }

  function esc(s) {
    return String(s).replace(/[&<>"']/g, function (c) {
      return { "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" }[c];
    });
  }
  function render(list) {
    // Общий список устройств; сейчас в меню — только МАП (type="map").
    var maps = (list || []).filter(function (d) { return d && d.type === "map"; });
    if (!maps.length) { wrap.hidden = true; items.innerHTML = ""; return; }
    wrap.hidden = false;
    var html = "";
    for (var i = 0; i < maps.length; i++) {
      html += '<a class="dropdown-sub" href="map?id=' + encodeURIComponent(maps[i].id) + '&gen=' + encodeURIComponent(maps[i].gen) + '">' + esc(maps[i].label) + "</a>";
    }
    items.innerHTML = html;
  }
  function poll() {
    fetch("api/map/devices", { cache: "no-store", credentials: "same-origin" })
      .then(function (r) { if (!r.ok) { throw 0; } return r.json(); })
      .then(render)
      .catch(function () { /* тихо: не мешаем странице */ });
  }
  // Сразу + чаще на старте (МАП может появиться/флапать), далее раз в 30 с.
  poll();
  var fast = 0;
  var t = setInterval(function () { poll(); if (++fast >= 6) { clearInterval(t); } }, 5000);
  setInterval(poll, 30000);
  window.addEventListener("focus", poll);
  document.addEventListener("visibilitychange", function () { if (!document.hidden) { poll(); } });
})();
