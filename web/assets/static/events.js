// Менеджер событий: тип события, действие, условные поля, добавить/удалить строку.
(function () {
  var tbody = document.getElementById("evRows");
  var add = document.getElementById("evAdd");
  var L = window.EV_I18N || {}, P = window.EV_PARAMS || [], M = window.EV_MODES || [], A = window.EV_ACTIONS || [], SERIALS = window.EV_SERIALS || [];
  if (!tbody || !add) return;

  // Комбобокс: выпадающий список серийников + возможность ввести своё.
  function enhanceCombo(inp) {
    if (!inp || inp.dataset.combo) return;
    inp.dataset.combo = "1";
    inp.setAttribute("autocomplete", "off");
    var wrap = document.createElement("span"); wrap.className = "ev-combo";
    inp.parentNode.insertBefore(wrap, inp); wrap.appendChild(inp);
    var btn = document.createElement("button"); btn.type = "button"; btn.className = "ev-combo-btn"; btn.textContent = "\u25be"; wrap.appendChild(btn);
    var list = document.createElement("div"); list.className = "ev-combo-list"; list.hidden = true; wrap.appendChild(list);
    function place() {
      if (wrap.hidden) return;
      var r = inp.getBoundingClientRect();
      list.style.position = "fixed";
      list.style.left = r.left + "px";
      list.style.top = (r.bottom + 2) + "px";
      list.style.minWidth = Math.max(r.width, 96) + "px";
    }
    function build() {
      var q = (inp.value || "").toLowerCase();
      list.innerHTML = "";
      for (var i = 0; i < SERIALS.length; i++) {
        var s = String(SERIALS[i]);
        if (q && s.toLowerCase().indexOf(q) < 0) continue;
        var d = document.createElement("div"); d.className = "ev-combo-opt"; d.textContent = s;
        d.addEventListener("mousedown", function (e) { e.preventDefault(); inp.value = this.textContent; list.hidden = true; inp.dispatchEvent(new Event("change", { bubbles: true })); });
        list.appendChild(d);
      }
      list.hidden = list.children.length === 0;
    }
    function open() { if (wrap.hidden) return; place(); list.hidden = false; build(); }
    btn.addEventListener("click", function () { if (list.hidden) { inp.focus(); open(); } else { list.hidden = true; } });
    inp.addEventListener("input", function () { open(); });
    inp.addEventListener("focus", function () { open(); });
    inp.addEventListener("blur", function () { setTimeout(function () { list.hidden = true; }, 150); });
    window.addEventListener("scroll", function () { list.hidden = true; }, true);
  }
  function enhanceAll(root) { Array.prototype.forEach.call((root || tbody).querySelectorAll('input[name="source_serial"], input[name="target_serial"]'), enhanceCombo); }
  var form = add.closest("form");
  var saveBtn = document.getElementById("evSave");
  var cancelBtn = document.getElementById("evCancel");
  var clearBtn = document.getElementById("evClear");

  function setDirty(d) { if (saveBtn) saveBtn.disabled = !d; if (cancelBtn) cancelBtn.disabled = !d; }
  function updateClear() { if (clearBtn) clearBtn.disabled = tbody.querySelectorAll(".ev-row").length === 0; }
  if (form) {
    form.addEventListener("input", function () { setDirty(true); });
    form.addEventListener("change", function () { setDirty(true); });
  }
  if (cancelBtn) { cancelBtn.addEventListener("click", function () { location.reload(); }); }
  if (clearBtn) {
    // Локальная очистка: применяется только после «Сохранить»; откат — «Отменить».
    clearBtn.addEventListener("click", function () {
      tbody.innerHTML = "";
      setDirty(true);
      updateClear();
    });
  }

  function esc(s) { return String(s == null ? "" : s).replace(/&/g, "&amp;").replace(/</g, "&lt;").replace(/>/g, "&gt;").replace(/"/g, "&quot;"); }
  function opts(arr, cur) {
    var h = "";
    for (var i = 0; i < arr.length; i++) { h += '<option value="' + arr[i][0] + '"' + (String(arr[i][0]) === String(cur) ? " selected" : "") + ">" + arr[i][1] + "</option>"; }
    return h;
  }
  function isMac(a) { return a === "mac_stop_gen" || a === "mac_stop" || a === "mac_start"; }
  function isRelay(a) { return a.indexOf("relay") === 0; }
  function needsTarget(a) { return isMac(a) || isRelay(a); }

  function toggle(row) {
    var t = row.querySelector(".ev-type").value;
    row.querySelector(".ev-param").hidden = t !== "param";
    row.querySelector(".ev-time").hidden = t !== "time";
    row.querySelector(".ev-mode").hidden = t !== "mode";
    var a = row.querySelector(".ev-action").value;
    row.querySelector(".ev-recipient").hidden = a !== "email";
    row.querySelector(".ev-target").hidden = !needsTarget(a);
    row.querySelector(".ev-trap").hidden = a !== "snmp";
    var tk = row.querySelector('select[name="target_kind"]');
    var ts = row.querySelector('input[name="target_serial"]');
    if (ts) {
      ts.hidden = false; // видимостью управляет обёртка
      var combo = ts.closest(".ev-combo");
      var off = !tk || tk.value === "";
      (combo || ts).hidden = off;
    }
  }

  function rowHtml() {
    var types = [["param", L.param || "param"], ["time", L.time || "time"], ["mode", L.mode || "mode"]];
    var ops = [["le", "\u2264"], ["ge", "\u2265"], ["eq", "="], ["ne", "\u2260"]];
    var kinds = [["map", L.kind_map || "MAP"], ["kes", L.kind_kes || "KES"]];
    var tkind = [["", L.target_cur || "current"]].concat(kinds);
    var modeArr = [];
    for (var i = 0; i < M.length; i++) { modeArr.push([String(M[i][0]), M[i][1]]); }
    var h = "";
    h += "<td><select name=\"enabled\">" + opts([["1", L.yes || "yes"], ["0", L.no || "no"]], "1") + "</select></td>";
    h += '<td><input type="hidden" name="id" value=""><select name="source_kind">' + opts(kinds, "map") +
      '</select><input name="source_serial" size="7" placeholder="' + esc(L.serial_ph) + '"></td>';
    h += "<td><select name=\"type\" class=\"ev-type\">" + opts(types, "param") + "</select></td>";
    h += '<td class="ev-ctl"><span class="ev-param"><select name="field">' + opts([["", ""]].concat(P), "") + "</select>" +
      '<select name="op">' + opts(ops, "le") + '</select><input name="value" size="8"></span>' +
      '<span class="ev-time" hidden><input name="time" type="time"></span>' +
      '<span class="ev-mode" hidden><select name="mode">' + opts(modeArr, "0") + "</select></span></td>";
    h += "<td><select name=\"action\" class=\"ev-action\">" + opts(A, "email") + "</select></td>";
    h += '<td class="ev-actctl">' +
      '<span class="ev-recipient" hidden><input name="recipient" size="16" placeholder="' + esc(L.recipient_default) + '"></span>' +
      '<span class="ev-target" hidden><select name="target_kind">' + opts(tkind, "") + '</select><input name="target_serial" size="7" placeholder="' + esc(L.serial_ph) + '" hidden></span>' +
      '<span class="ev-trap" hidden><input name="trap_host" size="12" placeholder="IP"></span></td>';
    h += '<td><input name="cooldown" type="number" min="0" max="86400" value="30" size="5"></td>';
    h += '<td><span class="ev-del" title="' + esc(L.del) + '">\u2715</span></td>';
    return h;
  }

  add.addEventListener("click", function () {
    var tr = document.createElement("tr");
    tr.className = "ev-row";
    tr.innerHTML = rowHtml();
    tbody.appendChild(tr);
    toggle(tr);
    enhanceAll(tr);
    setDirty(true);
    updateClear();
  });
  tbody.addEventListener("click", function (e) {
    if (e.target && e.target.classList.contains("ev-del")) { var tr = e.target.closest("tr"); if (tr) { tr.remove(); setDirty(true); updateClear(); } }
  });
  tbody.addEventListener("change", function (e) {
    if (e.target && (e.target.classList.contains("ev-type") || e.target.classList.contains("ev-action") || e.target.name === "target_kind")) { toggle(e.target.closest("tr")); }
  });
  Array.prototype.forEach.call(tbody.querySelectorAll(".ev-row"), toggle);
  enhanceAll(tbody);
  setDirty(false);
  updateClear();
})();
