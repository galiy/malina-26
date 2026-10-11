// Настройка E-Mail: показать поля ручного SMTP и нужную инструкцию при смене сервиса.
(function () {
  var sel = document.getElementById("emailProvider");
  var custom = document.getElementById("emailCustom");
  var userRow = document.getElementById("emailUserRow");
  if (!sel) return;

  function apply() {
    var v = sel.value;
    if (custom) custom.hidden = v !== "custom";
    if (userRow) userRow.hidden = v !== "custom";
    var blocks = document.querySelectorAll(".email-instr .instr");
    for (var i = 0; i < blocks.length; i++) {
      blocks[i].hidden = blocks[i].getAttribute("data-prov") !== v;
    }
  }

  sel.addEventListener("change", apply);
  apply();

  // глазик: показать/скрыть пароль
  document.querySelectorAll(".pw .eye").forEach(function (b) {
    b.addEventListener("click", function () {
      var i = b.parentNode.querySelector("input");
      i.type = (i.type === "password") ? "text" : "password";
    });
  });
})();
