new ClipboardJS('.copy-to-clipboard');
var app = Elm.Main.init();

document.addEventListener("click", function (e) {
  if (e.button !== 0) {
    return true;
  }

  e.preventDefault();
  var target = e.target;
  while (target && !(target instanceof HTMLAnchorElement)) {
    target = target.parentNode;
  }
  if (target instanceof HTMLAnchorElement) {
    var href = target.getAttribute("href");
    if (href && href.startsWith("https://package.elm-lang.org/packages/")) {
      href = href.substring(28);
    }
    // Redirect Source link to GitHub
    if (href && href.startsWith("/source/choonkeat/swe-swe/")) {
      window.open("https://github.com/choonkeat/swe-swe", "_blank");
      return;
    }
    if (href) {
      app.ports.locationHrefRequested.send(href);
    }
  }
});

// Load docs statically instead of via WebSocket
var pkg = "/packages/choonkeat/swe-swe/2.11.0";
Promise.all([
  fetch(pkg + "/README.md").then(function (r) { return r.text(); }),
  fetch(pkg + "/elm.json").then(function (r) { return r.json(); }),
  fetch(pkg + "/docs.json").then(function (r) { return r.json(); })
]).then(function (results) {
  var readme = results[0];
  var manifest = results[1];
  var docs = results[2];

  app.ports.onReadme.send({
    author: "choonkeat",
    project: "swe-swe",
    version: "2.11.0",
    readme: readme
  });
  app.ports.onManifest.send({
    author: "choonkeat",
    project: "swe-swe",
    version: "2.11.0",
    manifest: manifest
  });
  app.ports.onDocs.send({
    author: "choonkeat",
    project: "swe-swe",
    version: "2.11.0",
    time: Math.round(Date.now() / 1000),
    docs: docs
  });

  // Re-send current URL so Elm re-routes after data is loaded (fixes direct-URL 404)
  app.ports.locationHrefRequested.send(window.location.pathname);

  // Clean up sidebar: hide Install, Dependencies sections
  requestAnimationFrame(function () {
    var nav = document.querySelector('.pkg-nav');
    if (!nav) return;
    nav.querySelectorAll('h2').forEach(function (h2) {
      var text = h2.textContent.trim();
      if (text === 'Install' || text === 'Dependencies') {
        h2.parentNode.style.display = 'none';
      }
    });
  });
});
