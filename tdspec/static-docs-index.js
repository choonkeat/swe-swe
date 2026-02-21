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
    if (href) {
      app.ports.locationHrefRequested.send(href);
    }
  }
});

// Load docs statically instead of via WebSocket
var pkg = "/packages/tdspec/websocket-architecture/1.0.0";
Promise.all([
  fetch(pkg + "/README.md").then(function (r) { return r.text(); }),
  fetch(pkg + "/elm.json").then(function (r) { return r.json(); }),
  fetch(pkg + "/docs.json").then(function (r) { return r.json(); })
]).then(function (results) {
  var readme = results[0];
  var manifest = results[1];
  var docs = results[2];

  app.ports.onReadme.send({
    author: "tdspec",
    project: "websocket-architecture",
    version: "1.0.0",
    readme: readme
  });
  app.ports.onManifest.send({
    author: "tdspec",
    project: "websocket-architecture",
    version: "1.0.0",
    manifest: manifest
  });
  app.ports.onDocs.send({
    author: "tdspec",
    project: "websocket-architecture",
    version: "1.0.0",
    time: Math.round(Date.now() / 1000),
    docs: docs
  });
});
