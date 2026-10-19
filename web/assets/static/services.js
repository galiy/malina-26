(function () {
  "use strict";
  var I18N = window.I18N || {};
  var bodyApp = document.getElementById("svcBodyApp");
  var bodySys = document.getElementById("svcBodySys");
  if (!bodyApp) { return; }

  function t(k) { return I18N[k] || k; }
  function esc(s) {
    return String(s == null ? "" : s).replace(/[&<>"']/g, function (c) {
      return { "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" }[c];
    });
  }
  function stateLabel(a) { return t("svc.state." + (a || "unknown")); }
  function stateClass(a) {
    if (a === "active") { return "ok"; }
    if (a === "failed") { return "err"; }
    if (a === "activating" || a === "deactivating" || a === "reloading") { return "warn"; }
    return "muted";
  }
  function badge(active) {
    return '<span class="badge ' + stateClass(active) + '" data-state="' + esc(active) + '">' +
      esc(stateLabel(active)) + '</span>';
  }

  function renderApp(list) {
    var out = "";
    list.forEach(function (s) {
      var active = s.active || "unknown";
      var startDis = active === "active" || active === "activating" ? " disabled" : "";
      var stopDis = active === "inactive" || active === "failed" || active === "deactivating" ? " disabled" : "";
      out +=
        '<tr>' +
          '<td class="svc-name" data-label="' + esc(t("svc.col.service")) + '"><code>' + esc(s.unit) + '</code></td>' +
          '<td class="svc-desc" data-label="' + esc(t("svc.col.description")) + '">' + esc(s.description || "") + '</td>' +
          '<td class="svc-state" data-label="' + esc(t("svc.col.state")) + '">' + badge(active) + '</td>' +
          '<td class="svc-actions" data-label="' + esc(t("svc.col.actions")) + '">' +
            '<button type="button" data-unit="' + esc(s.unit) + '" data-action="start"' + startDis + '>' +
              esc(t("svc.start")) + '</button> ' +
            '<button type="button" class="btn-ghost" data-unit="' + esc(s.unit) + '" data-action="stop"' + stopDis + '>' +
              esc(t("svc.stop")) + '</button>' +
          '</td>' +
          '<td class="svc-note' + (s.note === "svc.note.critical" ? " crit" : "") + '" data-label="' + esc(t("svc.col.comment")) + '">' +
            (s.note ? esc(t(s.note)) : "") + '</td>' +
        '</tr>';
    });
    bodyApp.innerHTML = out;
  }

  function renderSys(list) {
    if (!bodySys) { return; }
    var out = "";
    list.forEach(function (s) {
      var active = s.active || "unknown";
      out +=
        '<tr>' +
          '<td class="svc-name" data-label="' + esc(t("svc.col.service")) + '"><code>' + esc(s.unit) + '</code></td>' +
          '<td class="svc-desc" data-label="' + esc(t("svc.col.description")) + '">' + esc(s.description || "") + '</td>' +
          '<td class="svc-state" data-label="' + esc(t("svc.col.state")) + '">' + badge(active) + '</td>' +
        '</tr>';
    });
    bodySys.innerHTML = out;
  }

  function render(list) {
    var app = [], sys = [];
    list.forEach(function (s) { (s.group === "system" ? sys : app).push(s); });
    renderApp(app);
    renderSys(sys);
  }

  async function refresh() {
    try {
      var r = await fetch("/api/services", { cache: "no-store" });
      if (!r.ok) { return; }
      var d = await r.json();
      render(d.services || []);
    } catch (e) { /* сеть/перезагрузка — ждём следующий тик */ }
  }

  async function act(unit, action, btn) {
    if (btn) { btn.disabled = true; }
    try {
      var fd = new URLSearchParams();
      fd.set("unit", unit);
      fd.set("action", action);
      var r = await fetch("/services/action", {
        method: "POST",
        headers: { "Content-Type": "application/x-www-form-urlencoded" },
        body: fd.toString()
      });
      setTimeout(refresh, r.ok ? 700 : 200);
    } catch (e) {
      setTimeout(refresh, 500);
    }
  }

  bodyApp.addEventListener("click", function (ev) {
    var btn = ev.target.closest("button[data-unit]");
    if (!btn) { return; }
    act(btn.getAttribute("data-unit"), btn.getAttribute("data-action"), btn);
  });

  // Вкладки
  var tabs = document.querySelectorAll(".svc-tab");
  function show(tab) {
    Array.prototype.forEach.call(tabs, function (b) {
      b.classList.toggle("active", b.getAttribute("data-tab") === tab);
    });
    var pa = document.getElementById("pane-app"), ps = document.getElementById("pane-system");
    if (pa) { pa.hidden = tab !== "app"; }
    if (ps) { ps.hidden = tab !== "system"; }
  }
  Array.prototype.forEach.call(tabs, function (b) {
    b.addEventListener("click", function () { show(b.getAttribute("data-tab")); });
  });

  var form = document.getElementById("rebootForm");
  if (form) {
    form.addEventListener("submit", function (ev) {
      if (!window.confirm(t("svc.reboot_confirm"))) { ev.preventDefault(); }
    });
  }

  render(window.SERVICES || []);
  setInterval(refresh, 5000);
})();
