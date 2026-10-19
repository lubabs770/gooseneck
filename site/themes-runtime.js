// Applies a gooseneck theme (same palettes as the TUI) to the page.
// Runs in <head> so there is no flash of the wrong colours.
(function () {
  var T = window.GOOSE_THEMES;
  var keys = ["bg", "panel", "element", "border", "text", "muted", "subtle",
    "primary", "secondary", "accent", "err", "warn", "ok", "info"];

  function apply(name) {
    var p = T[name] || T.opencode;
    if (!T[name]) name = "opencode";
    var s = document.documentElement.style;
    keys.forEach(function (k) { s.setProperty("--" + k, p[k]); });
    document.documentElement.dataset.theme = name;
    document.documentElement.classList.toggle("light", !!p.light);
    document.documentElement.style.colorScheme = p.light ? "light" : "dark";
    var meta = document.querySelector('meta[name="theme-color"]');
    if (meta) meta.content = p.bg;
    try { localStorage.setItem("goose-theme", name); } catch (e) {}
    document.dispatchEvent(new CustomEvent("goose-theme", { detail: name }));
    return name;
  }

  var saved = null;
  try { saved = localStorage.getItem("goose-theme"); } catch (e) {}
  if (!saved && window.matchMedia && matchMedia("(prefers-color-scheme: light)").matches) saved = "latte";
  window.gooseTheme = { apply: apply, names: Object.keys(T), current: function () {
    return document.documentElement.dataset.theme; } };
  apply(saved || "opencode");
})();
