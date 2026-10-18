// Страница «Сетевые → Настройки»: статус из shm (5 с), динамические строки,
// определение SN логгера по IP.
(function () {
  var T = window.I18N || {};
  var statusBody = document.getElementById("gridStatusRows");
  var rowsBody = document.getElementById("gridRows");
  var placesBody = document.getElementById("gridPlacements");
  if (!statusBody || !rowsBody || !placesBody) return;

  function el(tag, cls, text) {
    var e = document.createElement(tag);
    if (cls) e.className = cls;
    if (text !== undefined) e.textContent = text;
    return e;
  }

  function placements() {
    var out = [];
    placesBody.querySelectorAll('input[name="placement_name"]').forEach(function (inp) {
      var v = inp.value.trim();
      if (v && out.indexOf(v) < 0) out.push(v);
    });
    return out;
  }

  function placementSelect(selected) {
    var sel = el("select");
    sel.name = "placement";
    placements().forEach(function (p) {
      var o = el("option", null, p);
      o.value = p;
      if (p === selected) o.selected = true;
      sel.appendChild(o);
    });
    return sel;
  }

  function refreshPlacementSelects() {
    rowsBody.querySelectorAll('select[name="placement"]').forEach(function (sel) {
      var cur = sel.value;
      sel.textContent = "";
      placements().forEach(function (p) {
        var o = el("option", null, p);
        o.value = p;
        if (p === cur) o.selected = true;
        sel.appendChild(o);
      });
    });
  }

  function inverterRow() {
    var tr = el("tr");
    var tdName = el("td"); var name = el("input"); name.name = "name"; name.maxLength = 64; tdName.appendChild(name);
    var tdIP = el("td"); var ip = el("input"); ip.name = "ip"; tdIP.appendChild(ip);
    var tdPort = el("td"); var port = el("input"); port.name = "port"; port.type = "number"; port.min = 1; port.max = 65535; port.value = 8899; tdPort.appendChild(port);
    var tdType = el("td"); var typ = el("select"); typ.name = "type";
    [["deye", "Deye"], ["sofar", "Sofar"]].forEach(function (x) { var o = el("option", null, x[1]); o.value = x[0]; typ.appendChild(o); });
    tdType.appendChild(typ);
    var tdPlace = el("td"); tdPlace.appendChild(placementSelect(placements()[0] || ""));
    var tdSN = el("td"); var sn = el("input"); sn.name = "logger_sn"; sn.inputMode = "numeric"; tdSN.appendChild(sn);
    var det = el("button", "gr-detect", T["grid.detect"] || "Определить"); det.type = "button"; tdSN.appendChild(document.createTextNode(" ")); tdSN.appendChild(det);
    var tdDel = el("td"); var del = el("button", "gr-del", "✕"); del.type = "button"; del.title = T["grid.del"] || "Удалить"; tdDel.appendChild(del);
    [tdName, tdIP, tdPort, tdType, tdPlace, tdSN, tdDel].forEach(function (td) { tr.appendChild(td); });
    return tr;
  }

  function placementRow() {
    var tr = el("tr");
    var td = el("td"); var inp = el("input"); inp.name = "placement_name"; inp.maxLength = 64; td.appendChild(inp);
    var tdDel = el("td"); var del = el("button", "gp-del", "✕"); del.type = "button"; del.title = T["grid.del"] || "Удалить"; tdDel.appendChild(del);
    tr.appendChild(td); tr.appendChild(tdDel);
    return tr;
  }

  document.getElementById("grAdd").addEventListener("click", function () {
    var n = 0;
    rowsBody.querySelectorAll("tr").forEach(function (tr) {
      var ip = tr.querySelector('input[name="ip"]');
      if (ip && ip.value.trim()) n++;
    });
    if (n >= 16) {
      alert(T["grid.max16"] || "Максимум 16 инверторов");
      return;
    }
    rowsBody.appendChild(inverterRow());
  });
  document.getElementById("gpAdd").addEventListener("click", function () {
    placesBody.appendChild(placementRow());
    refreshPlacementSelects();
  });

  rowsBody.addEventListener("click", function (e) {
    if (e.target.classList.contains("gr-del")) {
      var tr = e.target.closest("tr"); if (tr) tr.remove();
      return;
    }
    if (e.target.classList.contains("gr-detect")) {
      var tr = e.target.closest("tr"); if (!tr) return;
      var ip = tr.querySelector('input[name="ip"]').value.trim();
      var port = tr.querySelector('input[name="port"]').value.trim() || "8899";
      var snInput = tr.querySelector('input[name="logger_sn"]');
      e.target.disabled = true;
      e.target.textContent = T["grid.detecting"] || "Определение…";
      var typ = tr.querySelector('select[name="type"]').value;
      var fd = new URLSearchParams(); fd.set("ip", ip); fd.set("port", port); fd.set("type", typ);
      fetch("/api/grid/detect_sn", {method: "POST", headers: {"Content-Type": "application/x-www-form-urlencoded"}, body: fd})
        .then(function (r) { return r.json(); })
        .then(function (j) { if (j.ok) snInput.value = j.sn; else alert(j.error || T["grid.detect_fail"] || "SN не получен"); })
        .catch(function () { alert(T["grid.detect_fail"] || "SN не получен"); })
        .finally(function () { e.target.disabled = false; e.target.textContent = T["grid.detect"] || "Определить"; });
    }
  });

  placesBody.addEventListener("click", function (e) {
    if (!e.target.classList.contains("gp-del")) return;
    var tr = e.target.closest("tr");
    var inp = tr && tr.querySelector('input[name="placement_name"]');
    var name = inp ? inp.value.trim() : "";
    var used = false;
    rowsBody.querySelectorAll('select[name="placement"]').forEach(function (sel) {
      if (sel.value === name) used = true;
    });
    if (used) {
      alert(T["grid.placement_used"] || "Размещение используется инвертором — удаление запрещено");
      return;
    }
    if (tr) tr.remove();
    refreshPlacementSelects();
  });

  function statusText(code) {
    var k = "grid.st" + code;
    return T[k] || String(code);
  }
  function kindText(k) {
    if (k === 0) return "Deye";
    if (k === 1) return "Sofar";
    return T["grid.unknown"] || ("?" + k);
  }
  function fmtTime(ts) {
    if (!ts) return T["grid.never"] || "—";
    var d = new Date(ts * 1000);
    return d.toLocaleString();
  }
  function row(item) {
    var tr = el("tr");
    tr.appendChild(el("td", null, String(item.id + 1)));
    tr.appendChild(el("td", null, item.name || ""));
    tr.appendChild(el("td", null, item.serial || ""));
    tr.appendChild(el("td", null, item.addr || ""));
    tr.appendChild(el("td", null, kindText(item.kind)));
    var st = el("td", "grid-status st" + item.status, statusText(item.status));
    tr.appendChild(st);
    tr.appendChild(el("td", null, fmtTime(item.last_seen)));
    return tr;
  }
  function refreshStatus() {
    return fetch("/api/grid/status", {cache: "no-store"})
      .then(function (r) { if (!r.ok) throw new Error("http"); return r.json(); })
      .then(function (j) {
        statusBody.textContent = "";
        var list = (j && j.inverters) || [];
        if (!list.length) {
          var tr = el("tr");
          var td = el("td", "muted", T["grid.status_empty"] || "Инверторы ещё не опрошены.");
          td.colSpan = 7; tr.appendChild(td); statusBody.appendChild(tr);
          return;
        }
        list.forEach(function (x) { statusBody.appendChild(row(x)); });
      })
      .catch(function () { /* оставляем прошлые данные */ });
  }
  var timer = null;
  function schedule() { timer = setTimeout(loop, 5000); }
  function loop() {
    if (timer) clearTimeout(timer);
    if (document.hidden) {
      schedule();
      return;
    }
    refreshStatus().finally(schedule);
  }
  document.addEventListener("visibilitychange", loop);
  loop();
})();
