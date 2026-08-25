// Dropdown behavior for internal/ui/dropdown.templ. The panel is a native
// <details>, so opening, closing and keyboard activation are the browser's;
// this only adds the two things <details> has no opinion about but a menu is
// expected to do — close when the click lands outside it, and close on
// Escape. With this file blocked a dropdown still opens and still closes on a
// second click of its own trigger.
//
// Only elements marked data-menu are touched, so a disclosure nested inside a
// menu (the board's share panel) counts as "inside" and survives a click on
// its own contents.
(function () {
  "use strict";

  function closeMenus(except) {
    document.querySelectorAll("details[data-menu][open]").forEach((menu) => {
      if (!except || !menu.contains(except)) menu.open = false;
    });
  }

  document.addEventListener("click", (e) => closeMenus(e.target));

  document.addEventListener("keydown", (e) => {
    if (e.key !== "Escape") return;
    closeMenus(null);
  });
})();
