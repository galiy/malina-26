// Встроенная справка: большой фрейм с документом по знаку «?».
(function () {
  var btn = document.getElementById("helpBtn");
  if (!btn) return;
  var doc = btn.getAttribute("data-doc");
  if (!doc) return;
  var overlay = null;
  var lastFocus = null;

  function close() {
    if (!overlay) return;
    overlay.remove();
    overlay = null;
    document.body.classList.remove("help-open");
    if (lastFocus && lastFocus.focus) lastFocus.focus();
  }

  function open() {
    if (overlay) return;
    lastFocus = document.activeElement;
    overlay = document.createElement("div");
    overlay.className = "help-overlay";
    overlay.setAttribute("role", "dialog");
    overlay.setAttribute("aria-modal", "true");

    var modal = document.createElement("div");
    modal.className = "help-modal";

    var closeBtn = document.createElement("button");
    closeBtn.type = "button";
    closeBtn.className = "help-close";
    closeBtn.setAttribute("aria-label", "×");
    closeBtn.textContent = "×";
    closeBtn.addEventListener("click", close);

    var frame = document.createElement("iframe");
    frame.className = "help-frame";
    frame.title = btn.getAttribute("title") || "?";
    frame.src = "/help?doc=" + encodeURIComponent(doc);

    modal.appendChild(closeBtn);
    modal.appendChild(frame);
    overlay.appendChild(modal);
    overlay.addEventListener("click", function (ev) {
      if (ev.target === overlay) close();
    });
    document.body.appendChild(overlay);
    document.body.classList.add("help-open");
    closeBtn.focus();
    overlay.addEventListener("keydown", function (ev) {
      if (ev.key !== "Tab") return;
      if (ev.shiftKey && document.activeElement === closeBtn) {
        ev.preventDefault();
        frame.focus();
      } else if (!ev.shiftKey && document.activeElement === frame) {
        ev.preventDefault();
        closeBtn.focus();
      }
    });
  }

  btn.addEventListener("click", open);
  document.addEventListener("keydown", function (ev) {
    if (ev.key === "Escape") close();
  });
  window.addEventListener("message", function (ev) {
    if (ev.data && ev.data.type === "malina-help-close") close();
  });
})();
