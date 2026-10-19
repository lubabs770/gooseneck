// Site behaviour: copy buttons, theme picking, the ctrl+k palette, scroll touches.
(function () {
  var root = window.GOOSE_ROOT || "";
  var $ = function (s, el) { return (el || document).querySelector(s); };
  var $$ = function (s, el) { return Array.prototype.slice.call((el || document).querySelectorAll(s)); };
  var themes = window.gooseTheme;

  // ---------- copy
  function copy(text, btn) {
    var done = function () {
      var old = btn.textContent;
      btn.textContent = "copied"; btn.classList.add("copied");
      setTimeout(function () { btn.textContent = old; btn.classList.remove("copied"); }, 1400);
    };
    if (navigator.clipboard) navigator.clipboard.writeText(text).then(done, done);
  }
  $$(".code .copy").forEach(function (b) {
    b.addEventListener("click", function () {
      var text = $("pre", b.parentNode).innerText
        .split("\n").map(function (l) { return l.replace(/^\$ /, ""); }).join("\n");
      copy(text, b);
    });
  });
  $$("[data-copy]").forEach(function (b) {
    b.addEventListener("click", function () { copy(b.getAttribute("data-copy"), b); });
  });

  // ---------- themes
  function markTheme() {
    var cur = themes.current();
    $$("[data-theme-pick]").forEach(function (el) {
      el.setAttribute("aria-pressed", el.getAttribute("data-theme-pick") === cur ? "true" : "false");
    });
    $$("[data-theme-name]").forEach(function (el) { el.textContent = cur; });
  }
  document.addEventListener("goose-theme", markTheme);
  markTheme();
  document.addEventListener("click", function (e) {
    var pick = e.target.closest("[data-theme-pick]");
    if (pick) themes.apply(pick.getAttribute("data-theme-pick"));
    if (e.target.closest("[data-cycle-theme]")) cycle(1);
  });
  function cycle(dir) {
    var n = themes.names, i = n.indexOf(themes.current());
    themes.apply(n[(i + dir + n.length) % n.length]);
  }

  // ---------- command palette (ctrl+k)
  var pal = $(".palette"), input = $(".palette-input"), list = $(".palette-list");
  var index = null, items = [], sel = 0;
  function load() {
    if (index) return Promise.resolve(index);
    return fetch(root + "search.json").then(function (r) { return r.json(); })
      .then(function (j) { index = j; return j; }).catch(function () { index = []; return index; });
  }
  function score(q, it) {
    var t = it.t.toLowerCase(), x = (it.x || "").toLowerCase();
    if (t === q) return 100;
    if (t.indexOf(q) === 0) return 80;
    if (t.indexOf(q) >= 0) return 60;
    if (it.s.toLowerCase().indexOf(q) >= 0) return 30;
    if (x.indexOf(q) >= 0) return 20;
    return 0;
  }
  function render() {
    var q = input.value.trim().toLowerCase();
    var res = [];
    themes.names.forEach(function (n) {
      if (!q || n.indexOf(q) >= 0 || (q.indexOf("theme") === 0))
        res.push({ t: "theme: " + n, s: "apply", theme: n, sc: q ? 70 : 1 });
    });
    (index || []).forEach(function (it) {
      var sc = q ? score(q, it) : (it.x ? 50 : 0);
      if (sc) res.push({ t: it.t, s: it.s, u: it.u, sc: sc });
    });
    res.sort(function (a, b) { return b.sc - a.sc; });
    items = res.slice(0, 40); sel = 0;
    list.innerHTML = items.length ? "" : '<li class="empty">No results. Try “radio”, “themes” or “scrobble”.</li>';
    items.forEach(function (it, i) {
      var li = document.createElement("li");
      var a = document.createElement("a");
      a.href = it.u ? root + it.u : "#";
      a.textContent = it.t;
      var sm = document.createElement("small"); sm.textContent = it.s; a.appendChild(sm);
      a.addEventListener("click", function (e) { if (it.theme) { e.preventDefault(); pick(i); } else close(); });
      a.addEventListener("mousemove", function () { if (sel !== i) { sel = i; mark(); } });
      li.appendChild(a); list.appendChild(li);
    });
    mark();
  }
  function mark() {
    $$("li", list).forEach(function (li, i) { li.classList.toggle("on", i === sel); });
    var on = $("li.on", list); if (on) on.scrollIntoView({ block: "nearest" });
  }
  function pick(i) {
    var it = items[i]; if (!it) return;
    if (it.theme) { themes.apply(it.theme); close(); return; }
    location.href = root + it.u; close();
  }
  function open() {
    if (!pal) return;
    pal.hidden = false; input.value = ""; input.focus();
    load().then(render); render();
  }
  function close() { if (pal) pal.hidden = true; }
  if (pal) {
    pal.addEventListener("click", function (e) { if (e.target === pal) close(); });
    input.addEventListener("input", render);
    input.addEventListener("keydown", function (e) {
      if (e.key === "ArrowDown" || (e.ctrlKey && e.key === "n")) { sel = Math.min(sel + 1, items.length - 1); mark(); e.preventDefault(); }
      else if (e.key === "ArrowUp" || (e.ctrlKey && e.key === "p")) { sel = Math.max(sel - 1, 0); mark(); e.preventDefault(); }
      else if (e.key === "Enter") { pick(sel); e.preventDefault(); }
    });
  }
  $$("[data-open-palette]").forEach(function (b) { b.addEventListener("click", open); });
  document.addEventListener("keydown", function (e) {
    var typing = /INPUT|TEXTAREA|SELECT/.test(document.activeElement.tagName);
    if ((e.ctrlKey || e.metaKey) && (e.key === "k" || e.key === "p")) { e.preventDefault(); pal.hidden ? open() : close(); return; }
    if (e.key === "Escape") { close(); return; }
    if (typing || e.ctrlKey || e.metaKey || e.altKey) return;
    if (e.key === "/") { e.preventDefault(); open(); }
    else if (e.key === "t") cycle(1);
    else if (e.key === "T") cycle(-1);
  });

  // ---------- marquee: duplicate the faces so the loop is seamless
  $$("[data-dup]").forEach(function (t) { t.innerHTML += t.innerHTML; });

  // ---------- scroll reveal + hero terminal
  if ("IntersectionObserver" in window) {
    var io = new IntersectionObserver(function (es) {
      es.forEach(function (en) { if (en.isIntersecting) { en.target.classList.add("in"); io.unobserve(en.target); } });
    }, { threshold: 0.12 });
    $$(".reveal").forEach(function (el) { io.observe(el); });
  } else $$(".reveal").forEach(function (el) { el.classList.add("in"); });

  var term = $(".term");
  if (term) {
    var onScroll = function () { term.classList.toggle("flat", window.scrollY > 40); };
    window.addEventListener("scroll", onScroll, { passive: true }); onScroll();
  }

  // card spotlight follows the mouse
  $$(".card").forEach(function (c) {
    c.addEventListener("pointermove", function (e) {
      var r = c.getBoundingClientRect();
      c.style.setProperty("--mx", (e.clientX - r.left) + "px");
      c.style.setProperty("--my", (e.clientY - r.top) + "px");
    });
  });

  // ---------- docs: mobile menu + scrollspy
  var tog = $(".side-toggle");
  if (tog) tog.addEventListener("click", function () { $(".side").classList.toggle("open"); });
  var tocLinks = $$(".toc a");
  if (tocLinks.length) {
    var heads = tocLinks.map(function (a) { return document.getElementById(a.getAttribute("href").slice(1)); });
    var spy = function () {
      var cur = heads[0];
      heads.forEach(function (h) { if (h && h.getBoundingClientRect().top < 140) cur = h; });
      tocLinks.forEach(function (a) { a.classList.toggle("on", !!cur && a.getAttribute("href") === "#" + cur.id); });
    };
    window.addEventListener("scroll", spy, { passive: true }); spy();
  }
})();
