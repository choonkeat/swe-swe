.PHONY: build run stop test clean swe-swe-init swe-swe-test swe-swe-run swe-swe-stop swe-swe-clean

build: build-cli

RUN_ARGS ?=
run:
	go run cmd/swe-swe-server/* $(RUN_ARGS)

stop:
	lsof -ti :9898 | xargs kill -9 2>/dev/null || true

test:
	go test -v ./...

clean:
	rm -rf ./dist

# swe-swe convenience targets
SWE_SWE_PATH ?= ./tmp
SWE_SWE_CLI := ./dist/swe-swe

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
	mkdir -p ./dist
	GOOS=linux GOARCH=amd64 go build -o ./dist/swe-swe.linux-amd64 ./cmd/swe-swe
	GOOS=linux GOARCH=arm64 go build -o ./dist/swe-swe.linux-arm64 ./cmd/swe-swe
	GOOS=darwin GOARCH=amd64 go build -o ./dist/swe-swe.darwin-amd64 ./cmd/swe-swe
	GOOS=darwin GOARCH=arm64 go build -o ./dist/swe-swe.darwin-arm64 ./cmd/swe-swe
	GOOS=windows GOARCH=amd64 go build -o ./dist/swe-swe.windows-amd64.exe ./cmd/swe-swe
