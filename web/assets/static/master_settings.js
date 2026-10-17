// Мастер-Узел — настройка: добавить/удалить строку узла.
// Делегирование на tbody — «+» работает и после восстановления строк («Отменить»).
(function () {
  var tbody = document.getElementById("mnRows");
  if (!tbody) return;

  var delTitle = (window.I18N && window.I18N["master.del"]) || "✕";

  function rowHtml() {
    return '<td><input name="name" maxlength="32"></td>' +
      '<td><select name="scheme"><option value="http" selected>http</option><option value="https">https</option></select></td>' +
      '<td><input name="host"></td>' +
      '<td><input name="port" type="number" min="1" max="65535" value="80"></td>' +
      '<td><input type="hidden" name="id" value=""><button type="button" class="mn-del" title="' + delTitle + '" aria-label="' + delTitle + '">✕</button></td>';
  }

  tbody.addEventListener("click", function (e) {
    if (e.target.closest("#mnAdd")) {
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
