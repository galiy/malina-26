// Динамические подпункты меню «МАП», «КЭС», «БМС»: раз в 30 с обновляют списки
// устройств из /api/{map,kes,bms}/devices. Пункты меню — постоянные; подпункты
// устройств вставляются ПЕРЕД пунктом «Настройка» (он — статический, последний).
(function () {
  "use strict";
  var boxes = {
    map: document.getElementById("mapMenuItems"),
    kes: document.getElementById("kesMenuItems"),
    bms: document.getElementById("bmsMenuItems")
  };

  function esc(s) {
    return String(s).replace(/[&<>"']/g, function (c) {
      return { "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" }[c];
    });
  }
  function label(d) {
    if (d.type === "bms") { return d.bms_type_label || d.bms_type || d.serial || ""; }
    return d.label || "";
  }
  function href(d) {
    var page = d.type === "bms" ? "bms" : d.type;
    return page + "?id=" + encodeURIComponent(d.id) + "&gen=" + encodeURIComponent(d.gen) +
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
  }
  function poll() {
    [["map", "api/map/devices"], ["kes", "api/kes/devices"], ["bms", "api/bms/devices"]].forEach(function (p) {
      fetch(p[1], { cache: "no-store", credentials: "same-origin" })
        .then(function (r) { if (!r.ok) { throw 0; } return r.json(); })
        .then(function (d) { render(boxes[p[0]], d, p[0]); })
        .catch(function () { /* тихо: не мешаем странице */ });
    });
  }
  poll();
  var fast = 0;
  var t = setInterval(function () { poll(); if (++fast >= 6) { clearInterval(t); } }, 5000);
  setInterval(poll, 30000);
  window.addEventListener("focus", poll);
  document.addEventListener("visibilitychange", function () { if (!document.hidden) { poll(); } });
})();
