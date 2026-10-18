// Панель сетевого инвертора: телеметрия из shm, обновление 1 с.
(function () {
  var T = window.I18N || {};
  var serial = window.GRID_SERIAL || "";
  var pause = document.getElementById("gridPause");
  var errEl = document.getElementById("gridErr");
  var subEl = document.getElementById("gridDeviceSub");
  var wrap = document.getElementById("gridSections");
  if (!wrap) return;
  function esc(s) {
    return String(s == null ? "" : s).replace(/[&<>"']/g, function (c) {
      return { "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" }[c];
    });
  }
  function t(k) { return T[k] || k; }
  function row(label, labelKey, value, unit, descKey) {
    var v = value === undefined || value === null ? "—" : value;
    return '<tr><td class="grid-p-name" title="' + esc(descKey ? t(descKey) : "") + '">' + esc(labelKey ? t(labelKey) : label) +
      '</td><td class="grid-p-val">' + esc(v) + '</td><td class="grid-p-unit">' + esc(unit || "") + "</td></tr>";
  }
  function section(titleKey, rows) {
    return '<h3 class="grid-sec-title">' + esc(t(titleKey)) + '</h3>' +
      '<table class="nettable grid-params"><thead><tr><th>' + esc(t("grid.panel.param")) +
      '</th><th>' + esc(t("grid.panel.value")) + '</th><th>' + esc(t("grid.panel.unit")) + '</th></tr></thead><tbody>' +
      rows.join("") + "</tbody></table>";
  }
  function fieldRow(f) {
    return row("", f.name_key, f.nd ? "—" : f.value, f.unit_key ? t(f.unit_key) : "", f.desc_key);
  }
  function render(d) {
    subEl.innerHTML = esc((d.type === "sofar" ? "Sofar" : "Deye") + " · " + d.serial + " · " +
      (d.status_text || "") + (d.last_seen ? " · " + new Date(d.last_seen * 1000).toLocaleString() : ""));
    var by = {};
    (d.fields || []).forEach(function (f) { by[f.id] = f; });
    var sections = [];
    sections.push(section("grid.panel.pv", [1,2,3,4,5,6].map(function (id) { return fieldRow(by[id]); })));
    sections.push(section("grid.panel.ac", [7,8,9,10,11,12,13,14,15].map(function (id) { return fieldRow(by[id]); })));
    sections.push(section("grid.panel.temp", [16,17].map(function (id) { return fieldRow(by[id]); })));
    var stRows = [row("", "grid.panel.status", d.inverter_status_text || "—", "", "")];
    [19,20,21,22,23,24].forEach(function (id) { stRows.push(fieldRow(by[id])); });
    var alarms = d.alarms || [];
    if (alarms.length) {
      stRows.push('<tr><td colspan="3"><table class="nettable grid-alarms"><thead><tr><th>' +
        esc(t("grid.panel.alarm_word")) + '</th><th>' + esc(t("grid.panel.alarm_code")) +
        '</th><th>' + esc(t("grid.panel.alarm_name")) + "</th></tr></thead><tbody>" +
        alarms.map(function (a) {
          return "<tr><td>w" + a.word + "</td><td>" + esc(a.code) + "</td><td>" + esc(t(a.name_key)) + "</td></tr>";
        }).join("") + "</tbody></table></td></tr>");
    }
    sections.push(section("grid.panel.state", stRows));
    sections.push(section("grid.panel.energy", [
      row("", "home.grid.e_today", d.energy_today_nd ? "—" : d.energy_today, t("home.grid.kwh"), ""),
      row("", "home.grid.e_total", d.energy_total_nd ? "—" : d.energy_total, t("home.grid.kwh"), "")
    ]));
    wrap.innerHTML = sections.join("");
    errEl.hidden = true;
  }
  var timer = null;
  function load() {
    if (pause && pause.checked) { timer = setTimeout(load, 1000); return; }
    fetch("/api/grid/device?serial=" + encodeURIComponent(serial), { cache: "no-store" })
      .then(function (r) { if (!r.ok) throw new Error("http"); return r.json(); })
      .then(render)
      .catch(function (e) { errEl.hidden = false; errEl.textContent = t("stats.no_data") + ": " + e.message; })
      .finally(function () { timer = setTimeout(load, 1000); });
  }
  load();
})();
