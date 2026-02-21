.PHONY: build preview clean

PORT ?= 8000

# Format and type-check all spec modules
build:
	cd $(CURDIR) && npx elm-format src/ --yes && elm make --docs=docs.json && rm -f docs.json

# Serve browsable docs with clickable type cross-references
preview:
	cd $(CURDIR) && elm-doc-preview --address 0.0.0.0 --port $(PORT) --no-browser

clean:
	rm -rf elm-stuff docs.json
