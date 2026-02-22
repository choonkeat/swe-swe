.PHONY: build preview clean docs audit

EDP_STATIC := $(shell npm root -g 2>/dev/null || echo /home/app/.swe-swe/lib/node_modules)/elm-doc-preview/static

PORT ?= 8000

# Format and type-check all spec modules
build:
	cd $(CURDIR) && npx elm-format src/ --yes && elm make --docs=docs.json

# Serve browsable docs with clickable type cross-references
preview:
	cd $(CURDIR) && elm-doc-preview --address 0.0.0.0 --port $(PORT) --no-browser

# Generate static HTML docs into ../www/ (root level — Elm app uses absolute paths)
WWW := $(CURDIR)/../www
PKG := $(WWW)/packages/choonkeat/swe-swe

docs: build
	mkdir -p $(WWW)/css $(WWW)/fonts $(WWW)/highlight $(WWW)/js
	mkdir -p $(PKG)/2.0.0
	cp $(EDP_STATIC)/css/* $(WWW)/css/
	cp $(EDP_STATIC)/fonts/* $(WWW)/fonts/
	cp $(EDP_STATIC)/highlight/highlight.pack.js $(WWW)/highlight/
	cp $(EDP_STATIC)/js/elm.js $(EDP_STATIC)/js/clipboard.min.js $(WWW)/js/
	cp static-docs-index.js $(WWW)/js/index.js
	cp static-docs-mobile.css $(WWW)/css/mobile.css
	sed 's|</head>|<meta name="viewport" content="width=device-width, initial-scale=1"><link href="/css/mobile.css" rel="stylesheet">\n</head>|' $(EDP_STATIC)/index.html > $(WWW)/tdspec.html
	cp docs.json README.md elm.json $(PKG)/2.0.0/
	echo '{"2.0.0":$(shell date +%s)}' > $(PKG)/releases.json
	echo '[]' > $(WWW)/search.json

clean:
	rm -rf elm-stuff


audit: build
	@echo ''
	@echo ''
	@echo '    now, do another thorough tallying of current state of our source code'
	@echo '    (including @/repos/agent-reverse-proxy/workspace)'
	@echo '    against our @tdspec for accuracy. point out and/or discuss where source code'
	@echo '    and tdspec does not tally (this is not about what a design "should be", it is'
	@echo '    about reflecting ground truth)'
	@echo ''
	@echo '    write the audit report in tdspec/audit/{yyyy-mm-dd}-{title}.md'
	@echo ''
	@echo ''
