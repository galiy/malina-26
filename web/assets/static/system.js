(function () {
  "use strict";
  var I18N = window.I18N || {};
  var el = document.getElementById("sysinfo");

  var sort = "cpu";
  var lastProcs = [];     // сортировка процессов: cpu | mem
  var lastData = null;
  var timer = null;
  var auto = true;

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
  function card(title, body, extra) {
    return '<section class="card' + (extra ? " " + extra : "") + '"><h2>' + esc(title) + '</h2>' + body + "</section>";
  }

  function gcolor(p) {
    if (p >= 85) return "#b23b3b";
    if (p >= 60) return "#e0a53a";
    return "#0d5637";
  }
  function gauge(percent, label, text) {
    var p = clamp(percent);
    var r = 48, len = Math.PI * r;
    var off = len * (1 - p / 100);
    var col = gcolor(p);
    return '<div class="gauge">' +
      '<svg width="120" height="70" viewBox="0 0 120 70">' +
        '<path d="M12 60 A48 48 0 0 1 108 60" fill="none" stroke="#e2e8e4" stroke-width="10" stroke-linecap="round"/>' +
        '<path d="M12 60 A48 48 0 0 1 108 60" fill="none" stroke="' + col + '" stroke-width="10" stroke-linecap="round" ' +
          'stroke-dasharray="' + len.toFixed(2) + '" stroke-dashoffset="' + off.toFixed(2) + '"/>' +
      '</svg>' +
      '<div class="gv" style="color:' + col + '">' + esc(text) + '</div>' +
      '<div class="gl">' + esc(label) + '</div>' +
    '</div>';
  }

  function procsTable(list) {
    var arr = (list || []).slice();
    if (sort === "mem") {
      arr.sort(function (a, b) { return b.mem - a.mem; });
    } else {
      arr.sort(function (a, b) { return b.cpu - a.cpu; });
    }
    var rows = arr.map(function (p) {
      return '<tr><td>' + esc(p.pid) + '</td><td>' + esc(p.user) + '</td>' +
        '<td>' + pct(p.cpu) + '</td><td>' + pct(p.mem) + '</td><td>' + kb(p.rss_kb) + '</td>' +
        '<td class="pcmd" title="' + esc(p.cmd) + '">' + esc(p.cmd) + '</td></tr>';
    }).join("");
    return '<table class="procs-table"><thead><tr>' +
      '<th>' + esc(t("si.pid")) + '</th><th>' + esc(t("si.user")) + '</th>' +
      '<th>' + esc(t("si.cpu_col")) + '</th><th>' + esc(t("si.mem_col")) + '</th>' +
      '<th>' + esc(t("si.rss")) + '</th><th>' + esc(t("si.command")) + '</th></tr></thead>' +
      '<tbody>' + rows + '</tbody></table>';
  }

  function connectedBody(c) {
    c = c || {};
    var body = '<h3>' + esc(t("si.wifi_clients")) + '</h3>';
    var wifi = c.wifi || [];
    if (wifi.length) {
      body += '<table class="conn-table"><thead><tr>' +
        '<th>' + esc(t("si.cl_host")) + '</th><th>' + esc(t("si.cl_ip")) + '</th>' +
        '<th>' + esc(t("si.cl_mac")) + '</th><th>' + esc(t("si.cl_signal")) + '</th>' +
        '<th>' + esc(t("si.cl_rate")) + '</th></tr></thead><tbody>';
      wifi.forEach(function (w) {
        var rt = (w.rx_rate || "") + (w.tx_rate ? (" / " + w.tx_rate) : "");
        body += '<tr><td>' + esc(w.host || "—") + '</td><td>' + esc(w.ip || "") + '</td>' +
          '<td class="mac">' + esc(w.mac || "") + '</td>' +
          '<td>' + (w.signal ? esc(w.signal + " dBm") : "—") + '</td>' +
          '<td>' + esc(rt || "—") + '</td></tr>';
      });
      body += '</tbody></table>';
    } else {
      body += '<p class="muted">' + esc(t("si.none")) + '</p>';
    }
    body += '<h3>' + esc(t("si.usb")) + '</h3>';
    var usb = c.usb || [];
    if (usb.length) {
      body += '<table class="conn-table"><tbody>';
      usb.forEach(function (u) {
        body += '<tr><td>' + esc(u.bus || "") + '</td><td class="mac"><code>' + esc(u.id || "") + '</code></td><td>' + esc(u.name || "") + '</td></tr>';
      });
      body += '</tbody></table>';
    } else {
      body += '<p class="muted">' + esc(t("si.none")) + '</p>';
    }
    return body;
  }

  function render(d) {
    var h = d.host || {}, c = d.cpu || {}, m = d.memory || {}, p = d.power || {};
    var out = "";

    // Ошибки внешних команд (таймаут/сбой) — сразу видно
    if (d.errors && d.errors.length) {
      out += '<section class="card card-err"><h2>' + esc(t("si.errors")) + '</h2>' +
        d.errors.map(function (msg) {
          return '<div class="srow"><span class="sv">' + esc(msg) + '</span></div>';
        }).join("") + '</section>';
    }

    // Хост + Состояние сети + Питание (объединено)
    var hostBody = row(t("si.hostname"), h.hostname) +
      row(t("si.model"), h.model) +
      row(t("si.os"), h.os) +
      row(t("si.kernel"), h.kernel) +
      row(t("si.arch"), h.arch) +
      row(t("si.uptime"), h.uptime) +
      row(t("si.boot"), h.boot_time) +
      row(t("si.time"), h.time + " (" + (h.timezone || "") + ")");
    if (d.net) {
      var n = d.net;
      function modeName(m) {
        if (m === "mode1") { return t("ns.mode1"); }
        if (m === "mode2") { return t("ns.mode2"); }
        return m || "";
      }
      var nb = '<h3>' + esc(t("ns.title")) + '</h3>' +
        row(t("ns.persistent"), modeName(n.persistent)) +
        row(t("ns.effective"), modeName(n.effective) + (n.override ? " (" + t("ns.override") + ")" : "")) +
        row(t("ns.serving"), n.serving ? t("ns.yes") : t("ns.no"));
      if (n.monitor) {
        var mm = n.monitor;
        nb += row(t("ns.monitor_last"), mm.ts ? new Date(mm.ts * 1000).toLocaleString() : "") +
          row(t("ns.monitor_foreign"), mm.foreign ? t("ns.yes") : t("ns.no")) +
          row(t("ns.monitor_clean"), mm.clean_count);
      } else {
        nb += row(t("ns.monitor"), t("ns.none_short"));
      }
      hostBody += nb;
    }
    if (p.throttled || p.volts || (p.flags && p.flags.length) || p.mem_arm_kb || p.mem_gpu_kb) {
      var pflags = (p.flags || []).map(function (f) { return t("si.thr." + f); }).join(", ");
      hostBody += '<h3>' + esc(t("si.power")) + '</h3>' +
        row(t("si.throttled"), (p.throttled || "") + " — " + (pflags || t("si.thr_ok")));
      if (p.volts) { hostBody += row(t("si.volts"), p.volts); }
      if (p.mem_arm_kb || p.mem_gpu_kb) { hostBody += row(t("si.mem_split"), kb(p.mem_arm_kb) + " / " + kb(p.mem_gpu_kb)); }
    }
    out += card(t("si.host"), hostBody);

    // Процессор + Память (объединено; память снизу)
    var tempPct = c.temp_c ? clamp(c.temp_c / 85 * 100) : 0;
    var usedPct = m.total_kb ? (m.used_kb / m.total_kb * 100) : 0;
    var swapPct = m.swap_total_kb ? (m.swap_used_kb / m.swap_total_kb * 100) : 0;
    out += card(t("si.cpu"),
      '<div class="gauges">' +
        gauge(c.usage_percent, t("si.usage"), pct(c.usage_percent)) +
        gauge(tempPct, t("si.temp"), (c.temp_c ? c.temp_c + " °C" : "—")) +
      "</div>" +
      row(t("si.cpu_model"), c.model) +
      row(t("si.cores"), c.cores) +
      row(t("si.load"), (c.load1 + " / " + c.load5 + " / " + c.load15)) +
      row(t("si.freq"), c.freq_khz ? (Math.round(c.freq_khz / 1000) + " MHz") : "") +
      '<h3>' + esc(t("si.memory")) + '</h3>' +
      '<div class="gauges">' +
        gauge(usedPct, t("si.mem_used"), pct(usedPct)) +
        gauge(swapPct, t("si.swap"), m.swap_total_kb ? pct(swapPct) : "—") +
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

    // Сбойные юниты (плашка-карточка)
    out += card(t("si.failed"),
      (d.failed_units && d.failed_units.length)
        ? d.failed_units.map(function (u) { return row("", u); }).join("")
        : '<p class="muted">' + esc(t("si.none")) + '</p>');

    // Подключённые устройства
    out += card(t("si.connected"), connectedBody(d.connected), "card-wide");

    // Процессы (сортировка)
    out += '<section class="card card-wide"><h2>' + esc(t("si.procs")) +
      ' <span class="procctl">' +
        '<span class="radios">' +
          '<label class="rad"><input type="radio" name="procsort" value="cpu"' + (sort === "cpu" ? " checked" : "") + '> ' + esc(t("si.sort_cpu")) + '</label>' +
          '<label class="rad"><input type="radio" name="procsort" value="mem"' + (sort === "mem" ? " checked" : "") + '> ' + esc(t("si.sort_mem")) + '</label>' +
        '</span>' +
        '<button type="button" id="procrefresh" class="procrefresh">' + esc(t("si.refresh")) + '</button>' +
      '</span></h2>' + procsTable(lastProcs) + '</section>';

    return out;
  }

  async function tick() {
    try {
      var r = await fetch("/api/system", { cache: "no-store" });
      if (r.status === 401) { location.href = "/login"; return; }
      if (!r.ok) { return; }
      lastData = await r.json();
      el.innerHTML = render(lastData);
    } catch (e) { /* оставляем последний вид */ }
  }

  function schedule() {
    if (auto && !timer) {
      timer = setInterval(tick, 1000);
    } else if (!auto && timer) {
      clearInterval(timer);
      timer = null;
    }
  }

  // Смена сортировки процессов (перерисовка без запроса)
  document.addEventListener("change", function (e) {
    if (e.target && e.target.name === "procsort") {
      sort = e.target.value;
      if (lastData) { el.innerHTML = render(lastData); }
    }
  });

  // Отключаемое автообновление (по умолчанию включено, запоминаем выбор)
  var chk = document.getElementById("autoref");
  if (chk) {
    var saved = null;
    try { saved = localStorage.getItem("malina_autoref"); } catch (e) { }
    auto = saved !== "0";
    chk.checked = auto;
    chk.addEventListener("change", function () {
      auto = chk.checked;
      try { localStorage.setItem("malina_autoref", auto ? "1" : "0"); } catch (e) { }
      schedule();
      if (auto) { tick(); }
    });
  }

  async function loadProcs() {
    try {
      var r = await fetch("/api/processes", { cache: "no-store" });
      if (!r.ok) { return; }
      var d = await r.json();
      lastProcs = d.processes || [];
      if (lastData) { el.innerHTML = render(lastData); }
    } catch (e) { /* оставляем как есть */ }
  }

  // кнопка «Обновить» процессов (без автообновления)
  document.addEventListener("click", function (e) {
    if (e.target && e.target.id === "procrefresh") { loadProcs(); }
  });

  (async function () {
    await tick();
    await loadProcs();
    schedule();
  })();
})();
