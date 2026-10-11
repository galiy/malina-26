// Мастер-Узел — настройка: добавить/удалить строку узла.
(function () {
  var tbody = document.getElementById("mnRows");
  var addBtn = document.getElementById("mnAdd");
  if (!tbody || !addBtn) return;

  function rowHtml() {
    return '<td><input name="name" maxlength="32"></td>' +
      '<td><select name="scheme"><option value="http" selected>http</option><option value="https">https</option></select></td>' +
      '<td><input name="host"></td>' +
      '<td><input name="port" type="number" min="1" max="65535" value="80"></td>' +
      '<td><input type="hidden" name="id" value=""><button type="button" class="mn-del" title="Удалить">✕</button></td>';
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
