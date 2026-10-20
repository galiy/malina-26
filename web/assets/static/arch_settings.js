// Настройки резервного копирования: показ пароля, скрытие нерелевантного
// target (сеть/флешка) и полей, специфичных для протокола.
(function () {
  var target = document.getElementById("arch_target");
  var proto = document.getElementById("arch_protocol");
  var net = document.querySelector(".arch-net");
  var flash = document.querySelector(".arch-flash");

  function updateTarget() {
    if (!target) return;
    var isFlash = target.value === "flash";
    if (net) net.hidden = isFlash;
    if (flash) flash.hidden = !isFlash;
  }

  function updateProtocol() {
    if (!proto) return;
    var smb = proto.value === "smb2";
    document.querySelectorAll(".arch-sftp").forEach(function (el) { el.hidden = smb; });
    document.querySelectorAll(".arch-smb").forEach(function (el) { el.hidden = !smb; });
  }

  if (target) {
    target.addEventListener("change", updateTarget);
    updateTarget();
  }
  if (proto) {
    proto.addEventListener("change", updateProtocol);
    updateProtocol();
  }

  // Результат действия не должен переживать F5: убираем ok/err из адреса,
  // страница остаётся той же.
  if (/[?&](ok|err|why|raw|avg)=/.test(location.search)) {
    history.replaceState(null, "", location.pathname);
  }

  document.querySelectorAll(".pw .eye").forEach(function (b) {
    b.addEventListener("click", function () {
      var i = b.parentNode.querySelector("input");
      i.type = (i.type === "password") ? "text" : "password";
    });
  });
})();
