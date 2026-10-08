(function () {
  "use strict";
  var I18N = window.I18N || {};
  var el = document.getElementById("sysinfo");

  function t(k) { return I18N[k] || k; }
  function esc(s) {
    return String(s == null ? "" : s).replace(/[&<>"']/g, function (c) {
      return { "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" }[c];
    });
  }
  function bytes(n) {
    n = Number(n) || 0;
    var u = ["B", "KB", "MB", "GB", "TB"], i = 0;
    while (n >= 1024 && i < u.length - 1) { n /= 1024; i++; }
    return (i > 0 && n < 10 ? n.toFixed(1) : Math.round(n)) + " " + u[i];
  }
  function kb(n) { return bytes((Number(n) || 0) * 1024); }
  function rate(n) { return bytes(n) + "/s"; }
  function pct(n) { return (Number(n) || 0).toFixed(1) + "%"; }
  function clamp(p) { p = Number(p) || 0; return Math.max(0, Math.min(100, p)); }

  function row(label, value) {
    return '<div class="srow"><span class="sk">' + esc(label) + '</span>' +
           '<span class="sv">' + esc(value) + "</span></div>";
  }
  function card(title, body) {
    return '<section class="card"><h2>' + esc(title) + '</h2>' + body + "</section>";
  }

  // Градусник: полукруглая шкала, цвет по порогу.
  function gcolor(p) {
    if (p >= 85) return "#b23b3b";
    if (p >= 60) return "#e0a53a";
    return "#0d5637";
  }
  function gauge(percent, label, text) {
    var p = clamp(percent);
    var r = 48, len = Math.PI * r;      // длина полуокружности
    var off = len * (1 - p / 100);
    var col = gcolor(p);
    return '<div class="gauge">' +
      '<svg width="120" height="70" viewBox="0 0 120 70">' +
        '<path d="M12 60 A48 48 0 0 1 108 60" fill="none" stroke="#e2e8e4" ' +
          'stroke-width="10" stroke-linecap="round"/>' +
        '<path d="M12 60 A48 48 0 0 1 108 60" fill="none" stroke="' + col + '" ' +
          'stroke-width="10" stroke-linecap="round" stroke-dasharray="' + len.toFixed(2) +
          '" stroke-dashoffset="' + off.toFixed(2) + '"/>' +
      '</svg>' +
      '<div class="gv" style="color:' + col + '">' + esc(text) + '</div>' +
      '<div class="gl">' + esc(label) + '</div>' +
    '</div>';
  }

  function render(d) {
    var h = d.host || {}, c = d.cpu || {}, m = d.memory || {}, p = d.power || {};
    var out = "";

    // Состояние сети (из /run/malina-net через /api/system)
    if (d.net) {
      var n = d.net;
      var nb = row(t("ns.persistent"), n.persistent) +
        row(t("ns.effective"), n.effective + (n.override ? " (" + t("ns.override") + ")" : "")) +
        row(t("ns.serving"), n.serving ? t("ns.yes") : t("ns.no"));
      if (n.monitor) {
        var mm = n.monitor;
        nb += row(t("ns.monitor_last"), mm.ts ? new Date(mm.ts * 1000).toLocaleString() : "") +
          row(t("ns.monitor_foreign"), mm.foreign ? t("ns.yes") : t("ns.no")) +
          row(t("ns.monitor_clean"), mm.clean_count);
      } else {
        nb += row(t("ns.monitor"), t("ns.none_short"));
      }
      out += card(t("ns.title"), nb);
    }

    // Хост
    out += card(t("si.host"),
      row(t("si.hostname"), h.hostname) +
      row(t("si.model"), h.model) +
      row(t("si.os"), h.os) +
      row(t("si.kernel"), h.kernel) +
      row(t("si.arch"), h.arch) +
      row(t("si.uptime"), h.uptime) +
      row(t("si.boot"), h.boot_time) +
      row(t("si.time"), h.time + " (" + (h.timezone || "") + ")")
    );

    // Процессор
    var tempPct = c.temp_c ? clamp(c.temp_c / 85 * 100) : 0;
    out += card(t("si.cpu"),
      '<div class="gauges">' +
        gauge(c.usage_percent, t("si.usage"), pct(c.usage_percent)) +
        gauge(tempPct, t("si.temp"), (c.temp_c ? c.temp_c + " °C" : "—")) +
      "</div>" +
      row(t("si.cpu_model"), c.model) +
      row(t("si.cores"), c.cores) +
      row(t("si.load"), (c.load1 + " / " + c.load5 + " / " + c.load15)) +
      row(t("si.freq"), c.freq_khz ? (Math.round(c.freq_khz / 1000) + " MHz") : "")
    );

    // Память
    var usedPct = m.total_kb ? (m.used_kb / m.total_kb * 100) : 0;
    var swapPct = m.swap_total_kb ? (m.swap_used_kb / m.swap_total_kb * 100) : 0;
    out += card(t("si.memory"),
      '<div class="gauges">' +
        gauge(usedPct, t("si.mem_used"), pct(usedPct)) +
        gauge(swapPct, t("si.swap"),
          m.swap_total_kb ? pct(swapPct) : "—") +
      "</div>" +
      row(t("si.mem_total"), kb(m.total_kb)) +
      row(t("si.mem_used"), kb(m.used_kb)) +
      row(t("si.mem_free"), kb(m.free_kb)) +
      row(t("si.mem_avail"), kb(m.available_kb)) +
      row(t("si.mem_buffers"), kb(m.buffers_kb)) +
      row(t("si.mem_cached"), kb(m.cached_kb)) +
      row(t("si.swap"), kb(m.swap_used_kb) + " / " + kb(m.swap_total_kb))
    );

    // Диски
    var diskGauges = (d.disks || []).map(function (dk) {
      return gauge(dk.used_percent, dk.mount, pct(dk.used_percent));
    }).join("");
    var diskRows = (d.disks || []).map(function (dk) {
      return row(dk.mount, kb(dk.used_kb) + " / " + kb(dk.total_kb) +
        " (" + pct(dk.used_percent) + "), " + t("si.disk_free") + ": " + kb(dk.free_kb));
    }).join("");
    out += card(t("si.disks"),
      (diskGauges ? '<div class="gauges">' + diskGauges + "</div>" : "") +
      (diskRows || '<p class="muted">' + esc(t("si.na")) + "</p>")
    );

    // Сеть
    var net = d.network || {};
    var netBody = row(t("si.gateway"), net.gateway) +
      row(t("si.dns"), (net.dns || []).join(", "));
    (net.interfaces || []).forEach(function (i) {
      var addrs = (i.addrs || []).join(", ");
      netBody += '<div class="iface"><h3>' + esc(i.name) + " (" + esc(i.state) + ")</h3>" +
        row(t("si.mac"), i.mac) +
        row(t("si.mtu"), i.mtu) +
        row(t("si.speed"), i.speed) +
        row(t("si.addresses"), addrs) +
        row(t("si.rx"), bytes(i.rx_bytes) + " (" + rate(i.rx_bps) + ")") +
        row(t("si.tx"), bytes(i.tx_bytes) + " (" + rate(i.tx_bps) + ")") +
        "</div>";
    });
    out += card(t("si.network"), netBody);

    // Питание — однострочная плашка справа от заголовка
    var pb = document.getElementById("powerbar");
    if (pb) {
      if (p.throttled || p.volts) {
        var parts = '<span class="pk">' + esc(t("si.power")) + '</span>';
        if (p.throttled) parts += '<span class="pv">' + esc(t("si.throttled")) + ": " + esc(p.throttled) + "</span>";
        if (p.volts) parts += '<span class="pv">' + esc(t("si.volts")) + ": " + esc(p.volts) + "</span>";
        pb.innerHTML = parts;
        pb.style.display = "";
      } else {
        pb.style.display = "none";
      }
    }

    // Сбойные юниты
    if ((d.failed_units || []).length) {
      out += card(t("si.failed"), (d.failed_units || []).map(function (u) {
        return row("", u);
      }).join(""));
    }

    return out;
  }

  async function tick() {
    try {
      var r = await fetch("/api/system", { cache: "no-store" });
      if (r.status === 401) { location.href = "/login"; return; }
      if (!r.ok) { return; }
      var d = await r.json();
      el.innerHTML = render(d);
    } catch (e) { /* оставляем последний вид */ }
  }

  tick();
  setInterval(tick, 1000);
})();
