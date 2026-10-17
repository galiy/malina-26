// BMS — настройка шлюзов: добавить/удалить строку шлюза.
// Добавление/удаление делегируются на tbody, поэтому «+» (внутри #bmsRows)
// продолжает работать и после восстановления строк при «Отменить».
(function () {
  var tbody = document.getElementById("bmsRows");
  if (!tbody) return;

  var delTitle = (window.I18N && window.I18N["bms.del"]) || "✕";

  function rowHtml() {
    return '<td><input name="host"></td>' +
      '<td><input name="port" type="number" min="1" max="65535" value="502"></td>' +
      '<td><select name="mode"><option value="modbus">modbus</option><option value="none" selected>none</option></select></td>' +
      '<td><button type="button" class="mn-del" title="' + delTitle + '" aria-label="' + delTitle + '">✕</button></td>';
  }

  tbody.addEventListener("click", function (e) {
    if (e.target.closest("#bmsAdd")) {
      var tr = document.createElement("tr");
      tr.innerHTML = rowHtml();
      tbody.appendChild(tr);
      return;
    }
    if (e.target && e.target.classList.contains("mn-del")) {
      var del = e.target.closest("tr");
      if (del) del.remove();
    }
  });
})();
