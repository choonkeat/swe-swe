.PHONY: build run stop test test-cli test-server clean swe-swe-init swe-swe-test swe-swe-run swe-swe-stop swe-swe-clean golden-update deploy/digitalocean

build: build-cli

RUN_ARGS ?=
run:
	@cp cmd/swe-swe/templates/host/swe-swe-server/go.mod.txt cmd/swe-swe/templates/host/swe-swe-server/go.mod
	@cp cmd/swe-swe/templates/host/swe-swe-server/go.sum.txt cmd/swe-swe/templates/host/swe-swe-server/go.sum
	go run cmd/swe-swe/templates/host/swe-swe-server/* $(RUN_ARGS)

stop:
	lsof -ti :9898 | xargs kill -9 2>/dev/null || true

test: test-cli test-server

test-cli:
	go test -v ./cmd/swe-swe

# Test the swe-swe-server template code
# Copies template to temp dir, sets up go.mod, runs tests, syncs go.sum back
SERVER_TEMPLATE := cmd/swe-swe/templates/host/swe-swe-server
TEST_SERVER_ARGS ?=
test-server:
	@rm -rf /tmp/swe-swe-server-test
	@mkdir -p /tmp/swe-swe-server-test
	@cp -r $(SERVER_TEMPLATE)/* /tmp/swe-swe-server-test/
	@mv /tmp/swe-swe-server-test/go.mod.txt /tmp/swe-swe-server-test/go.mod
	@mv /tmp/swe-swe-server-test/go.sum.txt /tmp/swe-swe-server-test/go.sum
	cd /tmp/swe-swe-server-test && go mod tidy && go test -v $(TEST_SERVER_ARGS) ./...
	@cp /tmp/swe-swe-server-test/go.sum $(SERVER_TEMPLATE)/go.sum.txt
	@rm -rf /tmp/swe-swe-server-test

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
	$(SWE_SWE_CLI) init --project-directory $(SWE_SWE_PATH)

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
	$(SWE_SWE_CLI) run --project-directory $(SWE_SWE_PATH)

swe-swe-stop: $(SWE_SWE_CLI)
	$(SWE_SWE_CLI) stop --project-directory $(SWE_SWE_PATH)

swe-swe-clean:
	rm -rf $(SWE_SWE_PATH)/.swe-swe

# DigitalOcean Packer build target
deploy/digitalocean: build
	$(MAKE) -C deploy/digitalocean build \
		REGION="$(REGION)" \
		DROPLET_SIZE="$(DROPLET_SIZE)" \
		INIT_FLAGS="$(INIT_FLAGS)" \
		IMAGE_NAME="$(IMAGE_NAME)" \
		SWE_SWE_PASSWORD="$(SWE_SWE_PASSWORD)" \
		ENABLE_HARDENING="$(ENABLE_HARDENING)" \
		$(if $(GIT_CLONE_URL), GIT_CLONE_URL="$(GIT_CLONE_URL)")

GIT_COMMIT := $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
BUILD_TIME := $(shell date -u +"%Y-%m-%dT%H:%M:%SZ")
VERSION := dev
LDFLAGS := -X main.Version=$(VERSION) -X main.GitCommit=$(GIT_COMMIT) -X main.BuildTime=$(BUILD_TIME)

build-cli:
	@rm -f cmd/swe-swe/templates/host/swe-swe-server/go.mod cmd/swe-swe/templates/host/swe-swe-server/go.sum
	mkdir -p ./dist
	GOOS=linux GOARCH=amd64 go build -ldflags "$(LDFLAGS)" -o ./dist/swe-swe.linux-amd64 ./cmd/swe-swe
	GOOS=linux GOARCH=arm64 go build -ldflags "$(LDFLAGS)" -o ./dist/swe-swe.linux-arm64 ./cmd/swe-swe
	GOOS=darwin GOARCH=amd64 go build -ldflags "$(LDFLAGS)" -o ./dist/swe-swe.darwin-amd64 ./cmd/swe-swe
	GOOS=darwin GOARCH=arm64 go build -ldflags "$(LDFLAGS)" -o ./dist/swe-swe.darwin-arm64 ./cmd/swe-swe
	GOOS=windows GOARCH=amd64 go build -ldflags "$(LDFLAGS)" -o ./dist/swe-swe.windows-amd64.exe ./cmd/swe-swe

# Golden file generation for testing
GOLDEN_TESTDATA := ./cmd/swe-swe/testdata/golden

golden-update: build-cli
	@rm -rf /tmp/swe-swe-golden
	@ln -sfn $(abspath $(GOLDEN_TESTDATA)) /tmp/swe-swe-golden
	@$(MAKE) _golden-variant NAME=default FLAGS=
	@$(MAKE) _golden-variant NAME=claude-only FLAGS="--agents claude"
	@$(MAKE) _golden-variant NAME=aider-only FLAGS="--agents aider"
	@$(MAKE) _golden-variant NAME=goose-only FLAGS="--agents goose"
	@$(MAKE) _golden-variant NAME=opencode-only FLAGS="--agents opencode"
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
	@$(MAKE) _golden-variant NAME=with-slash-commands-opencode-only FLAGS="--agents opencode --with-slash-commands ck@https://github.com/choonkeat/slash-commands.git"
	@$(MAKE) _golden-variant NAME=with-slash-commands-claude-opencode FLAGS="--agents claude,opencode --with-slash-commands ck@https://github.com/choonkeat/slash-commands.git"
	@$(MAKE) _golden-variant NAME=with-ssl-selfsign FLAGS="--ssl=selfsign"
	@$(MAKE) _golden-variant NAME=with-copy-home-paths FLAGS="--copy-home-paths .gitconfig,.ssh"
	@$(MAKE) _golden-variant NAME=with-status-bar-color FLAGS="--status-bar-color \#dc2626"
	@$(MAKE) _golden-variant NAME=with-terminal-font FLAGS="--terminal-font-size 16 --terminal-font-family 'JetBrains Mono'"
	@$(MAKE) _golden-variant NAME=with-status-bar-font FLAGS="--status-bar-font-size 14 --status-bar-font-family monospace"
	@$(MAKE) _golden-variant NAME=with-all-ui-options FLAGS="--status-bar-color red --terminal-font-size 18 --status-bar-font-size 14"
	@$(MAKE) _golden-certs-no-certs
	@$(MAKE) _golden-certs-node-extra-ca-certs
	@$(MAKE) _golden-certs-ssl-cert-file
	@$(MAKE) _golden-previous-init-flags-reuse
	@$(MAKE) _golden-variant NAME=previous-init-flags-reuse-no-config FLAGS="--previous-init-flags=reuse"
	@$(MAKE) _golden-previous-init-flags-ignore
	@$(MAKE) _golden-variant NAME=previous-init-flags-ignore-claude FLAGS="--previous-init-flags=ignore --agents=claude"
	@$(MAKE) _golden-variant NAME=previous-init-flags-invalid FLAGS="--previous-init-flags=invalid"
	@$(MAKE) _golden-variant NAME=previous-init-flags-reuse-with-other-flags FLAGS="--previous-init-flags=reuse --agents=claude"
	@$(MAKE) _golden-already-initialized
	@# Normalize TLS files to avoid flip-flopping due to random cert generation
	@find $(GOLDEN_TESTDATA) -name "server.crt" -exec cp $(GOLDEN_TESTDATA)/../standard-tls/server.crt {} \;
	@find $(GOLDEN_TESTDATA) -name "server.key" -exec cp $(GOLDEN_TESTDATA)/../standard-tls/server.key {} \;
	@rm -f /tmp/swe-swe-golden
	@echo "Golden files updated in $(GOLDEN_TESTDATA)"

_golden-variant:
	@rm -rf $(GOLDEN_TESTDATA)/$(NAME)/home $(GOLDEN_TESTDATA)/$(NAME)/target
	@mkdir -p $(GOLDEN_TESTDATA)/$(NAME)/home $(GOLDEN_TESTDATA)/$(NAME)/target
	@unset NODE_EXTRA_CA_CERTS SSL_CERT_FILE NODE_EXTRA_CA_CERTS_BUNDLE && \
	HOME=/tmp/swe-swe-golden/$(NAME)/home $(SWE_SWE_CLI) init $(FLAGS) --project-directory /tmp/swe-swe-golden/$(NAME)/target \
		2> $(GOLDEN_TESTDATA)/$(NAME)/stderr.txt || true

# Multi-step golden test: init with flags, then reuse to verify config is restored
_golden-previous-init-flags-reuse:
	@rm -rf $(GOLDEN_TESTDATA)/previous-init-flags-reuse/home $(GOLDEN_TESTDATA)/previous-init-flags-reuse/target
	@mkdir -p $(GOLDEN_TESTDATA)/previous-init-flags-reuse/home $(GOLDEN_TESTDATA)/previous-init-flags-reuse/target
	@unset NODE_EXTRA_CA_CERTS SSL_CERT_FILE NODE_EXTRA_CA_CERTS_BUNDLE && \
	HOME=/tmp/swe-swe-golden/previous-init-flags-reuse/home $(SWE_SWE_CLI) init --agents=claude --with-docker --project-directory /tmp/swe-swe-golden/previous-init-flags-reuse/target \
		2> /dev/null || true
	@unset NODE_EXTRA_CA_CERTS SSL_CERT_FILE NODE_EXTRA_CA_CERTS_BUNDLE && \
	HOME=/tmp/swe-swe-golden/previous-init-flags-reuse/home $(SWE_SWE_CLI) init --previous-init-flags=reuse --project-directory /tmp/swe-swe-golden/previous-init-flags-reuse/target \
		2> $(GOLDEN_TESTDATA)/previous-init-flags-reuse/stderr.txt || true

# Multi-step golden test: init with flags, then ignore to verify config is overwritten
_golden-previous-init-flags-ignore:
	@rm -rf $(GOLDEN_TESTDATA)/previous-init-flags-ignore/home $(GOLDEN_TESTDATA)/previous-init-flags-ignore/target
	@mkdir -p $(GOLDEN_TESTDATA)/previous-init-flags-ignore/home $(GOLDEN_TESTDATA)/previous-init-flags-ignore/target
	@HOME=/tmp/swe-swe-golden/previous-init-flags-ignore/home $(SWE_SWE_CLI) init --agents=claude --project-directory /tmp/swe-swe-golden/previous-init-flags-ignore/target \
		2> /dev/null || true
	@HOME=/tmp/swe-swe-golden/previous-init-flags-ignore/home $(SWE_SWE_CLI) init --previous-init-flags=ignore --agents=aider --project-directory /tmp/swe-swe-golden/previous-init-flags-ignore/target \
		2> $(GOLDEN_TESTDATA)/previous-init-flags-ignore/stderr.txt || true

# Multi-step golden test: init twice to test "already initialized" error
_golden-already-initialized:
	@rm -rf $(GOLDEN_TESTDATA)/already-initialized/home $(GOLDEN_TESTDATA)/already-initialized/target
	@mkdir -p $(GOLDEN_TESTDATA)/already-initialized/home $(GOLDEN_TESTDATA)/already-initialized/target
	@HOME=/tmp/swe-swe-golden/already-initialized/home $(SWE_SWE_CLI) init --agents=claude --project-directory /tmp/swe-swe-golden/already-initialized/target \
		2> /dev/null || true
	@HOME=/tmp/swe-swe-golden/already-initialized/home $(SWE_SWE_CLI) init --project-directory /tmp/swe-swe-golden/already-initialized/target \
		2> $(GOLDEN_TESTDATA)/already-initialized/stderr.txt || true

# Certificate test variants: with and without enterprise certificates
_golden-certs-no-certs:
	@rm -rf $(GOLDEN_TESTDATA)/with-certs-no-certs/home $(GOLDEN_TESTDATA)/with-certs-no-certs/target
	@mkdir -p $(GOLDEN_TESTDATA)/with-certs-no-certs/home $(GOLDEN_TESTDATA)/with-certs-no-certs/target
	@unset NODE_EXTRA_CA_CERTS SSL_CERT_FILE NODE_EXTRA_CA_CERTS_BUNDLE && \
	HOME=/tmp/swe-swe-golden/with-certs-no-certs/home $(SWE_SWE_CLI) init --project-directory /tmp/swe-swe-golden/with-certs-no-certs/target \
		2> $(GOLDEN_TESTDATA)/with-certs-no-certs/stderr.txt || true

_golden-certs-node-extra-ca-certs:
	@rm -rf $(GOLDEN_TESTDATA)/with-certs-node-extra-ca-certs/home $(GOLDEN_TESTDATA)/with-certs-node-extra-ca-certs/target
	@mkdir -p $(GOLDEN_TESTDATA)/with-certs-node-extra-ca-certs/home $(GOLDEN_TESTDATA)/with-certs-node-extra-ca-certs/target
	@mkdir -p /tmp/swe-swe-test-certs && \
	echo "-----BEGIN CERTIFICATE-----" > /tmp/swe-swe-test-certs/test.pem && \
	echo "test certificate content" >> /tmp/swe-swe-test-certs/test.pem && \
	echo "-----END CERTIFICATE-----" >> /tmp/swe-swe-test-certs/test.pem && \
	NODE_EXTRA_CA_CERTS=/tmp/swe-swe-test-certs/test.pem \
	HOME=/tmp/swe-swe-golden/with-certs-node-extra-ca-certs/home $(SWE_SWE_CLI) init --project-directory /tmp/swe-swe-golden/with-certs-node-extra-ca-certs/target \
		2> $(GOLDEN_TESTDATA)/with-certs-node-extra-ca-certs/stderr.txt || true

_golden-certs-ssl-cert-file:
	@rm -rf $(GOLDEN_TESTDATA)/with-certs-ssl-cert-file/home $(GOLDEN_TESTDATA)/with-certs-ssl-cert-file/target
	@mkdir -p $(GOLDEN_TESTDATA)/with-certs-ssl-cert-file/home $(GOLDEN_TESTDATA)/with-certs-ssl-cert-file/target
	@mkdir -p /tmp/swe-swe-test-certs && \
	echo "-----BEGIN CERTIFICATE-----" > /tmp/swe-swe-test-certs/test.pem && \
	echo "test certificate content" >> /tmp/swe-swe-test-certs/test.pem && \
	echo "-----END CERTIFICATE-----" >> /tmp/swe-swe-test-certs/test.pem && \
	SSL_CERT_FILE=/tmp/swe-swe-test-certs/test.pem \
	HOME=/tmp/swe-swe-golden/with-certs-ssl-cert-file/home $(SWE_SWE_CLI) init --project-directory /tmp/swe-swe-golden/with-certs-ssl-cert-file/target \
		2> $(GOLDEN_TESTDATA)/with-certs-ssl-cert-file/stderr.txt || true
