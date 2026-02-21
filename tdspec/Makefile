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


audit: build
	@echo ''
	@echo ''
	@echo '    now, do another thorough tallying of current state of our source code'
	@echo '    (including @/repos/agent-reverse-proxy/workspace)'
	@echo '    against our @tdspec for accuracy. point out and/or discuss where source code'
	@echo '    and tdspec does not tally (this is not about what a design "should be", it is'
	@echo '    about reflecting ground truth)'
	@echo ''
	@echo ''
