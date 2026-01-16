.PHONY: build run stop test clean swe-swe-init swe-swe-test swe-swe-run swe-swe-stop swe-swe-clean golden-update

build: build-cli

RUN_ARGS ?=
run:
	@cp cmd/swe-swe/templates/host/swe-swe-server/go.mod.txt cmd/swe-swe/templates/host/swe-swe-server/go.mod
	@cp cmd/swe-swe/templates/host/swe-swe-server/go.sum.txt cmd/swe-swe/templates/host/swe-swe-server/go.sum
	go run cmd/swe-swe/templates/host/swe-swe-server/* $(RUN_ARGS)

stop:
	lsof -ti :9898 | xargs kill -9 2>/dev/null || true

test:
	go test -v ./...

clean:
	rm -rf ./dist

# swe-swe convenience targets
SWE_SWE_PATH ?= ./tmp
SWE_SWE_GOOS := $(shell go env GOOS)
SWE_SWE_GOARCH := $(shell go env GOARCH)
SWE_SWE_EXT := $(if $(filter windows,$(SWE_SWE_GOOS)),.exe,)
SWE_SWE_CLI := ./dist/swe-swe.$(SWE_SWE_GOOS)-$(SWE_SWE_GOARCH)$(SWE_SWE_EXT)

$(SWE_SWE_CLI): build-cli

swe-swe-init: $(SWE_SWE_CLI)
	$(SWE_SWE_CLI) init --path $(SWE_SWE_PATH)

swe-swe-test: swe-swe-init
	cd $(SWE_SWE_PATH) && docker-compose -f .swe-swe/docker-compose.yml config > /dev/null
	@echo "✓ docker-compose.yml is valid"
	@test -f $(SWE_SWE_PATH)/.swe-swe/Dockerfile && echo "✓ Dockerfile exists"
	@test -f $(SWE_SWE_PATH)/.swe-swe/traefik-dynamic.yml && echo "✓ traefik-dynamic.yml exists"
	@test -d $(SWE_SWE_PATH)/.swe-swe/swe-swe-server && echo "✓ swe-swe-server source exists"
	@test -d $(SWE_SWE_PATH)/.swe-swe/home && echo "✓ home directory exists"
	cd $(SWE_SWE_PATH) && docker-compose -f .swe-swe/docker-compose.yml build --no-cache
	@echo "✓ docker-compose build successful"

swe-swe-run: swe-swe-init
	$(SWE_SWE_CLI) run --path $(SWE_SWE_PATH)

swe-swe-stop: $(SWE_SWE_CLI)
	$(SWE_SWE_CLI) stop --path $(SWE_SWE_PATH)

swe-swe-clean:
	rm -rf $(SWE_SWE_PATH)/.swe-swe

build-cli:
	@rm -f cmd/swe-swe/templates/host/swe-swe-server/go.mod cmd/swe-swe/templates/host/swe-swe-server/go.sum
	mkdir -p ./dist
	GOOS=linux GOARCH=amd64 go build -o ./dist/swe-swe.linux-amd64 ./cmd/swe-swe
	GOOS=linux GOARCH=arm64 go build -o ./dist/swe-swe.linux-arm64 ./cmd/swe-swe
	GOOS=darwin GOARCH=amd64 go build -o ./dist/swe-swe.darwin-amd64 ./cmd/swe-swe
	GOOS=darwin GOARCH=arm64 go build -o ./dist/swe-swe.darwin-arm64 ./cmd/swe-swe
	GOOS=windows GOARCH=amd64 go build -o ./dist/swe-swe.windows-amd64.exe ./cmd/swe-swe

# Golden file generation for testing
GOLDEN_TESTDATA := ./cmd/swe-swe/testdata/golden

golden-update: build-cli
	@rm -rf /tmp/swe-swe-golden
	@ln -sfn $(abspath $(GOLDEN_TESTDATA)) /tmp/swe-swe-golden
	@$(MAKE) _golden-variant NAME=default FLAGS=
	@$(MAKE) _golden-variant NAME=claude-only FLAGS="--agents claude"
	@$(MAKE) _golden-variant NAME=aider-only FLAGS="--agents aider"
	@$(MAKE) _golden-variant NAME=goose-only FLAGS="--agents goose"
	@$(MAKE) _golden-variant NAME=nodejs-agents FLAGS="--agents claude,gemini,codex"
	@$(MAKE) _golden-variant NAME=exclude-aider FLAGS="--exclude-agents aider"
	@$(MAKE) _golden-variant NAME=with-apt FLAGS="--apt-get-install vim,curl"
	@$(MAKE) _golden-variant NAME=with-npm FLAGS="--npm-install typescript"
	@$(MAKE) _golden-variant NAME=with-both-packages FLAGS="--apt-get-install vim --npm-install typescript"
	@$(MAKE) _golden-variant NAME=with-docker FLAGS="--with-docker"
	@$(MAKE) _golden-variant NAME=with-slash-commands FLAGS="--agents all --with-slash-commands ck@https://github.com/choonkeat/slash-commands.git"
	@$(MAKE) _golden-variant NAME=with-slash-commands-multi FLAGS='--agents all --with-slash-commands "ck@https://github.com/choonkeat/slash-commands.git https://github.com/org/team-cmds.git"'
	@$(MAKE) _golden-variant NAME=with-slash-commands-claude-only FLAGS="--agents claude --with-slash-commands ck@https://github.com/choonkeat/slash-commands.git"
	@$(MAKE) _golden-variant NAME=with-slash-commands-codex-only FLAGS="--agents codex --with-slash-commands ck@https://github.com/choonkeat/slash-commands.git"
	@$(MAKE) _golden-variant NAME=with-slash-commands-no-alias FLAGS="--agents all --with-slash-commands https://github.com/choonkeat/slash-commands.git"
	@$(MAKE) _golden-variant NAME=with-slash-commands-claude-codex FLAGS="--agents claude,codex --with-slash-commands ck@https://github.com/choonkeat/slash-commands.git"
	@rm -f /tmp/swe-swe-golden
	@echo "Golden files updated in $(GOLDEN_TESTDATA)"

_golden-variant:
	@rm -rf $(GOLDEN_TESTDATA)/$(NAME)/home $(GOLDEN_TESTDATA)/$(NAME)/target
	@mkdir -p $(GOLDEN_TESTDATA)/$(NAME)/home $(GOLDEN_TESTDATA)/$(NAME)/target
	@HOME=/tmp/swe-swe-golden/$(NAME)/home $(SWE_SWE_CLI) init $(FLAGS) --path /tmp/swe-swe-golden/$(NAME)/target
