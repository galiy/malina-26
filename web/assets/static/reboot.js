(function () {
  "use strict";
  var I18N = window.I18N || {};
  var cfg = window.REBOOT || { total: 120, interval: 5, next: "./" };
  var TOTAL = Number(cfg.total) || 120;      // окно ожидания, сек
  var EVERY = Number(cfg.interval) || 5;     // период опроса, сек

  var fill = document.getElementById("rebFill");
  var pct = document.getElementById("rebPct");
  var title = document.getElementById("rbtTitle");
  var hint = document.getElementById("rebHint");
  var errBox = document.getElementById("rebErr");

  function t(k) { return I18N[k] || k; }

  var elapsed = 0;
  var done = false;
  var tick = setInterval(onTick, 100);
  var poll = setInterval(probe, EVERY * 1000);

  function onTick() {
    if (done) { return; }
    elapsed += 0.1;
    var p = Math.min(100, (elapsed / TOTAL) * 100);
    fill.style.width = p.toFixed(1) + "%";
    pct.textContent = Math.floor(p) + "%";
    if (elapsed >= TOTAL) { fail(); }
  }

  function probe() {
    if (done) { return; }
    var ctl = ("AbortController" in window) ? new AbortController() : null;
    var to = setTimeout(function () { if (ctl) { ctl.abort(); } }, 3000);
    fetch(cfg.next || "./", { cache: "no-store", signal: ctl ? ctl.signal : undefined })
      .then(function (r) {
        clearTimeout(to);
        // Любой ответ означает, что веб-сервер снова отвечает.
        if (r && (r.ok || r.status > 0)) { success(); }
      })
      .catch(function () { clearTimeout(to); });
  }

  function success() {
    if (done) { return; }
    done = true;
    clearInterval(tick); clearInterval(poll);
    fill.style.width = "100%";
    pct.textContent = "100%";
    if (title) { title.textContent = t("reboot.ready"); }
    setTimeout(function () { window.location.href = cfg.next || "./"; }, 600);
  }

  function fail() {
    if (done) { return; }
    done = true;
    clearInterval(tick); clearInterval(poll);
    if (title) { title.style.display = "none"; }
    if (hint) { hint.style.display = "none"; }
    if (errBox) { errBox.hidden = false; }
  }

  // первый опрос чуть раньше, чтобы быстрее отреагировать на короткий ребут
  setTimeout(probe, 2000);
})();
