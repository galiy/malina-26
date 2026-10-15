// BMS — настройка шлюзов: добавить/удалить строку шлюза.
(function () {
  var tbody = document.getElementById("bmsRows");
  var addBtn = document.getElementById("bmsAdd");
  if (!tbody || !addBtn) return;

  function rowHtml() {
    return '<td><input name="host"></td>' +
      '<td><input name="port" type="number" min="1" max="65535" value="502"></td>' +
      '<td><select name="mode"><option value="modbus">modbus</option><option value="none" selected>none</option></select></td>' +
      '<td><button type="button" class="mn-del" title="✕">✕</button></td>';
  }

  addBtn.addEventListener("click", function () {
    var tr = document.createElement("tr");
    tr.innerHTML = rowHtml();
    tbody.appendChild(tr);
  });

  tbody.addEventListener("click", function (e) {
    if (e.target && e.target.classList.contains("mn-del")) {
      var tr = e.target.closest("tr");
      if (tr) tr.remove();
    }
  });
})();
