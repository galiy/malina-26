// Карточка статуса устройств на страницах настроек map/kes/bms.
// Данные — из shm соответствующего reader'а; обновление 1 раз в секунду.
(function () {
  var card = document.getElementById("settingsStatus");
  if (!card) return;
  var typ = card.getAttribute("data-type");
  var T = window.I18N || {};
  var head = card.querySelector("thead");
  var body = card.querySelector("tbody");
  var COLS = {
    map: [
      ["settings.status.id", "id"], ["settings.status.serial", "serial"],
      ["settings.status.serial_letter", "serial_letter"], ["settings.status.port", "port"],
      ["settings.status.model", "model"], ["settings.status.protocol", "protocol"],
      ["settings.status.unit", "unit"], ["settings.status.baud", "baud"],
      ["settings.status.gen", "gen"], ["settings.status.state", "state"],
      ["settings.status.last_seen", "last_seen"]
    ],
    kes: [
      ["settings.status.id", "id"], ["settings.status.port", "port"],
      ["settings.status.baud", "baud"], ["settings.status.serial", "serial"],
      ["settings.status.gen", "gen"], ["settings.status.state", "state"],
      ["settings.status.last_seen", "last_seen"]
    ],
    bms: [
      ["settings.status.id", "id"], ["settings.status.base", "base"],
      ["settings.status.source", "source"], ["settings.status.type", "type"],
      ["settings.status.state", "state"], ["settings.status.framing", "framing"],
      ["settings.status.gen", "gen"], ["settings.status.last_seen", "last_seen"]
    ]
  };
  var cols = COLS[typ] || [];
  function esc(s) {
    return String(s == null ? "" : s).replace(/[&<>"']/g, function (c) {
      return { "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" }[c];
    });
  }
  function tr(k) { return T[k] || k; }
  function cell(v, field) {
    if (field === "state") return v ? tr("settings.status.ok") : tr("settings.status.no_link");
    if (field === "last_seen") {
      if (!v) return tr("settings.status.never");
      return new Date(v * 1000).toLocaleString();
    }
    if (field === "framing") return v === 0 ? "native" : v === 1 ? "m-rtu" : v === 2 ? "m-tcp" : String(v);
    if (field === "model") return v ? tr("model." + v) : "";
    if (field === "protocol") return v ? tr("settings.status.proto_" + v) : "";
    return v == null ? "" : String(v);
  }
  function render(rows, failed) {
    head.innerHTML = "<tr>" + cols.map(function (c) { return "<th>" + esc(tr(c[0])) + "</th>"; }).join("") + "</tr>";
    if (failed) {
      body.innerHTML = '<tr><td colspan="' + cols.length + '" class="err">' + esc(tr("settings.status.error")) + "</td></tr>";
      return;
    }
    if (!rows.length) {
      body.innerHTML = '<tr><td colspan="' + cols.length + '" class="muted">' + esc(tr("settings.status.empty")) + "</td></tr>";
      return;
    }
    body.innerHTML = rows.map(function (r) {
      return "<tr>" + cols.map(function (c) { return "<td>" + esc(cell(r[c[1]], c[1])) + "</td>"; }).join("") + "</tr>";
    }).join("");
  }
  var timer = null;
  function load() {
    fetch("/api/settings/status?type=" + encodeURIComponent(typ), { cache: "no-store" })
      .then(function (r) { if (!r.ok) throw new Error("http"); return r.json(); })
      .then(function (j) { render((j && j.rows) || [], false); })
      .catch(function () { render([], true); })
      .finally(function () { timer = setTimeout(load, 1000); });
  }
  load();
})();
