(function () {
  "use strict";
  var I18N = window.I18N || {};
  function t(k) { return I18N[k] || k; }
  function $(s, r) { return (r || document).querySelector(s); }
  function $$(s, r) { return Array.prototype.slice.call((r || document).querySelectorAll(s)); }

  var panes = { mode1: $("#pane-mode1"), mode2: $("#pane-mode2") };
  function activeMode() { var r = $('input[name="mode"]:checked'); return r ? r.value : "mode1"; }
  function showPane() {
    var m = activeMode();
    Object.keys(panes).forEach(function (k) { panes[k].style.display = (k === m) ? "" : "none"; });
    updateStatic(); validate();
  }
  $$('input[name="mode"]').forEach(function (r) { r.addEventListener("change", showPane); });

  // --- показ static-полей и значения по умолчанию ---
  var DEF = { ip: "192.168.0.222", mask: "255.0.0.0", gw: "192.168.0.1", dns: "192.168.0.1" };
  function updateStatic() {
    $$("tr.static-row").forEach(function (tr) {
      var sel = document.querySelector('select[name="' + tr.getAttribute("data-for") + '"]');
      var show = sel && sel.value === tr.getAttribute("data-when");
      tr.style.display = show ? "" : "none";
      if (show) {
        $$("input", tr).forEach(function (inp) {
          if (!inp.value) {
            if (/mask/.test(inp.name)) inp.value = DEF.mask;
            else if (/gw|gateway/.test(inp.name)) inp.value = DEF.gw;
            else if (/dns/.test(inp.name)) inp.value = DEF.dns;
            else inp.value = DEF.ip;
          }
        });
      }
    });
  }
  $$('select[name="m1_eth_mode"], select[name="m2_sta_ipmode"]').forEach(function (s) {
    s.addEventListener("change", function () { updateStatic(); validate(); });
  });

  // --- глазки для пароля ---
  $$(".pw .eye").forEach(function (b) {
    b.addEventListener("click", function () {
      var i = b.parentNode.querySelector("input");
      i.type = (i.type === "password") ? "text" : "password";
    });
  });

  // --- валидация ---
  var IP = /^(25[0-5]|2[0-4]\d|1?\d?\d)(\.(25[0-5]|2[0-4]\d|1?\d?\d)){3}$/;
  var MASK = /^(255|254|252|248|240|224|192|128|0)\.(255|254|252|248|240|224|192|128|0)\.(255|254|252|248|240|224|192|128|0)\.(255|254|252|248|240|224|192|128|0)$/;
  var SSID = /^[^\x00-\x1f]{1,32}$/;
  function ip2int(s) { var p = s.split("."); return ((+p[0]) << 24 | (+p[1]) << 16 | (+p[2]) << 8 | (+p[3])) >>> 0; }
  function ipList(v) { v = v.trim(); if (!v) return true; return v.split(",").every(function (x) { return IP.test(x.trim()); }); }
  function inNet(ip, server, mask) {
    if (!IP.test(ip) || !IP.test(server) || !MASK.test(mask)) return false;
    var m = ip2int(mask);
    return (ip2int(ip) & m) === (ip2int(server) & m);
  }
  function dhcpRangeOK(start, end, server, mask) {
    if (!inNet(start, server, mask) || !inNet(end, server, mask)) return false;
    return ip2int(start) <= ip2int(end);
  }

  var rules = {
    m1_ap_ssid: { re: SSID },
    m1_ap_pass: { req: true, min: 8, max: 63 },
    m1_lan_server: { re: IP },
    m1_lan_mask: { re: MASK },
    m1_lan_start: { dhcp: ["m1_lan_server", "m1_lan_mask", "m1_lan_end", "start"] },
    m1_lan_end: { dhcp: ["m1_lan_server", "m1_lan_mask", "m1_lan_start", "end"] },
    m1_eth_ip: { re: IP },
    m1_eth_mask: { re: MASK },
    m1_eth_gw: { re: IP },
    m1_eth_dns: { fn: ipList },
    m2_sta_ssid: { re: SSID },
    m2_sta_pass: { req: true, min: 8, max: 63 },
    m2_sta_ip: { re: IP },
    m2_sta_mask: { re: MASK },
    m2_sta_gw: { re: IP },
    m2_sta_dns: { fn: ipList },
    m2_eth_server: { re: IP },
    m2_eth_mask: { re: MASK },
    m2_eth_start: { dhcp: ["m2_eth_server", "m2_eth_mask", "m2_eth_end", "start"] },
    m2_eth_end: { dhcp: ["m2_eth_server", "m2_eth_mask", "m2_eth_start", "end"] }
  };
  function val(name) { var el = document.querySelector('[name="' + name + '"]'); return el ? (el.value || "").trim() : ""; }
  function fieldOK(el) {
    var r = rules[el.name]; if (!r) return true;
    var v = (el.value || "").trim();
    if (r.req && !v) return false;
    if (!v && !r.req) return true;
    if (r.re) return r.re.test(v);
    if (r.fn) return r.fn(v);
    if (r.min && v.length < r.min) return false;
    if (r.max && v.length > r.max) return false;
    if (r.dhcp) {
      var server = val(r.dhcp[0]), mask = val(r.dhcp[1]), other = val(r.dhcp[2]);
      if (!IP.test(v) || !inNet(v, server, mask)) return false;
      var otherVal = other;
      if (otherVal && IP.test(otherVal)) {
        if (r.dhcp[3] === "start" && ip2int(v) > ip2int(otherVal)) return false;
        if (r.dhcp[3] === "end" && ip2int(v) < ip2int(otherVal)) return false;
      }
    }
    return true;
  }
  function validate() {
    var bad = false;
    $$("#" + (activeMode() === "mode1" ? "pane-mode1" : "pane-mode2") + " input, #" + (activeMode() === "mode1" ? "pane-mode1" : "pane-mode2") + " select").forEach(function (el) {
      if (!el.name) return;
      if (el.offsetParent === null) { el.classList.remove("invalid"); return; }
      var ok = fieldOK(el);
      el.classList.toggle("invalid", !ok);
      if (!ok) bad = true;
    });
    var ferr = $("#formerr");
    if (ferr) ferr.textContent = bad ? t("net.bad_fields") : "";
    return !bad;
  }
  $$("#netform input, #netform select").forEach(function (el) {
    el.addEventListener("input", function () { if (rules[el.name]) el.classList.toggle("invalid", !fieldOK(el)); validate(); });
    el.addEventListener("change", function () { if (rules[el.name]) el.classList.toggle("invalid", !fieldOK(el)); validate(); });
  });
  $("#netform").addEventListener("submit", function (e) { if (!validate()) e.preventDefault(); });

  $("#restorebtn").addEventListener("click", function () {
    $$("#pane-mode1 input, #pane-mode1 select, #pane-mode2 input, #pane-mode2 select").forEach(function (el) {
      if (!el.name) return;
      if (el.type === "checkbox") { el.checked = el.defaultChecked; } else { el.value = el.defaultValue; }
    });
    updateStatic(); validate();
  });

  // --- скан Wi-Fi ---
  var modal = $("#scanmodal"), list = $("#scanlist"), poll = null;
  function esc(s) { return String(s == null ? "" : s).replace(/[&<>"']/g, function (c) { return { "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" }[c]; }); }
  function renderScan(arr) {
    if (!arr || !arr.length) { list.textContent = t("net.scan_empty"); return; }
    list.innerHTML = arr.map(function (n) {
      return '<a href="#" class="scanitem" data-ssid="' + encodeURIComponent(n.ssid) + '">' + esc(n.ssid) +
        ' <span class="muted">' + esc(n.signal || "") + "</span></a>";
    }).join("");
    $$(".scanitem", list).forEach(function (a) {
      a.addEventListener("click", function (e) {
        e.preventDefault();
        var i = $('input[name="m2_sta_ssid"]'); if (i) i.value = decodeURIComponent(a.getAttribute("data-ssid"));
        closeModal(); validate();
      });
    });
  }
  function fetchScan() {
    fetch("/api/scan", { cache: "no-store" }).then(function (r) { if (r.status === 401) { location.href = "/login"; return; } return r.json(); }).then(renderScan).catch(function () { });
  }
  function openModal() { modal.hidden = false; }
  function closeModal() { modal.hidden = true; if (poll) { clearInterval(poll); poll = null; } }
  $("#scanbtn").addEventListener("click", function () {
    openModal(); list.textContent = t("info.loading");
    fetch("/network/scan", { method: "POST" }).catch(function () { }).then(function () {
      fetchScan();
      var n = 0; poll = setInterval(function () { fetchScan(); if (++n > 10) { clearInterval(poll); poll = null; } }, 2000);
    });
  });
  $("#scanclose").addEventListener("click", closeModal);

  showPane();
})();
