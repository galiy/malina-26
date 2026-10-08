(function () {
  "use strict";
  var I18N = window.I18N || {};
  var body = document.getElementById("svcBody");
  if (!body) { return; }

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

  function render(list) {
    var out = "";
    list.forEach(function (s) {
      var active = s.active || "unknown";
      var startDis = active === "active" || active === "activating" ? " disabled" : "";
      var stopDis = active === "inactive" || active === "failed" || active === "deactivating" ? " disabled" : "";
      out +=
        '<tr>' +
          '<td class="svc-name"><code>' + esc(s.unit) + '</code></td>' +
          '<td class="svc-desc">' + esc(s.description || "") + '</td>' +
          '<td><span class="badge ' + stateClass(active) + '" data-state="' + esc(active) + '">' +
            esc(stateLabel(active)) + '</span></td>' +
          '<td class="svc-actions">' +
            '<button type="button" data-unit="' + esc(s.unit) + '" data-action="start"' + startDis + '>' +
              esc(t("svc.start")) + '</button> ' +
            '<button type="button" class="btn-ghost" data-unit="' + esc(s.unit) + '" data-action="stop"' + stopDis + '>' +
              esc(t("svc.stop")) + '</button>' +
          '</td>' +
          '<td class="svc-note' + (s.note === "svc.note.critical" ? " crit" : "") + '">' +
            (s.note ? esc(t(s.note)) : "") + '</td>' +
        '</tr>';
    });
    body.innerHTML = out;
  }

  async function refresh() {
    try {
      var r = await fetch("/api/services", { cache: "no-store" });
      if (!r.ok) { return; }
      var d = await r.json();
      render(d.services || []);
    } catch (e) { /* сеть/перезагрузка — просто ждём следующий тик */ }
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
      // Дадим systemd коротко применить изменение, затем обновим.
      setTimeout(refresh, r.ok ? 700 : 200);
    } catch (e) {
      setTimeout(refresh, 500);
    }
  }

  body.addEventListener("click", function (ev) {
    var btn = ev.target.closest("button[data-unit]");
    if (!btn) { return; }
    act(btn.getAttribute("data-unit"), btn.getAttribute("data-action"), btn);
  });

  var form = document.getElementById("rebootForm");
  if (form) {
    form.addEventListener("submit", function (ev) {
      if (!window.confirm(t("svc.reboot_confirm"))) {
        ev.preventDefault();
      }
    });
  }

  render(window.SERVICES || []);
  setInterval(refresh, 5000);
})();
