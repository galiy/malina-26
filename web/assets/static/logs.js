(function () {
  "use strict";
  var sel = document.getElementById("logfile");
  var view = document.getElementById("logview");
  var status = document.getElementById("logstatus");
  var file = window.MALINA_LOG_FILE || (sel && sel.value) || "syslog";

  var PAGE = 100;
  var older = [];   // история, загруженная выше live-блока
  var live = [];    // свежий (tail) блок
  var loadingOlder = false;

  function render() { view.textContent = older.concat(live).join("\n"); }
  function nearBottom() { return (view.scrollHeight - view.scrollTop - view.clientHeight) < 8; }
  function nearTop() { return view.scrollTop < 32; }

  function setFile(f) {
    file = f;
    if (sel) sel.value = f;
    older = []; live = [];
    try {
      var u = new URL(location.href);
      u.searchParams.set("file", f);
      history.replaceState(null, "", u);
    } catch (e) { /* игнорируем */ }
    loadTail(true);
  }

  async function loadTail(scrollBottom) {
    try {
      var r = await fetch("api/log?file=" + encodeURIComponent(file) + "&lines=" + PAGE + "&offset=0",
                          { cache: "no-store" });
      if (r.status === 401) { location.href = "login"; return; }
      if (!r.ok) { if (status) status.textContent = "HTTP " + r.status; return; }
      var d = await r.json();
      live = d.lines || [];
      render();
      if (status) status.textContent = new Date().toLocaleTimeString();
      if (scrollBottom) view.scrollTop = view.scrollHeight;
    } catch (e) { /* оставляем как есть */ }
  }

  async function loadOlder() {
    if (loadingOlder) return;
    loadingOlder = true;
    try {
      var offset = older.length + live.length;
      var r = await fetch("api/log?file=" + encodeURIComponent(file) + "&lines=" + PAGE + "&offset=" + offset,
                          { cache: "no-store" });
      if (!r.ok) return;
      var d = await r.json();
      var block = d.lines || [];
      if (block.length === 0) return;
      var prevH = view.scrollHeight, prevTop = view.scrollTop;
      older = block.concat(older);
      render();
      view.scrollTop = prevTop + (view.scrollHeight - prevH);
    } catch (e) { /* игнорируем */ }
    finally { loadingOlder = false; }
  }

  view.addEventListener("scroll", function () {
    if (nearTop()) loadOlder();
  });

  async function tick() {
    // Tail только когда прижато к низу; прокрутили вверх — обновление замирает.
    if (!nearBottom()) return;
    if (loadingOlder) return;
    await loadTail(true);
  }

  if (sel) sel.addEventListener("change", function () { setFile(sel.value); });

  loadTail(true);
  setInterval(tick, 1000);
})();
