build: build-elm build-go

ELM_OUTPUT=cmd/swe-swe/static/js/app.js
build-elm: elm/node_modules
	cd elm && elm make src/Main.elm --output=../$(ELM_OUTPUT)

build-go:
	go build -o bin/swe-swe cmd/swe-swe/*.go

test: test-elm test-go

test-elm: elm/node_modules
	cd elm && elm-test

test-go:
	go test ./...

PLAYWRIGHT_ARGS=
test-playwright: tests/playwright/node_modules tests/playwright/.playwright-installed
	cd tests/playwright && npx playwright test $(PLAYWRIGHT_ARGS)

tests/playwright/node_modules: tests/playwright/package.json
	cd tests/playwright && npm install

tests/playwright/.playwright-installed: tests/playwright/node_modules
	cd tests/playwright && npx playwright install && touch .playwright-installed

format: format-go format-elm

format-go:
	gofmt -s -w .

format-elm: elm/node_modules
	cd elm && echo y | elm-format src/

elm/node_modules: elm/package.json
	cd elm && npm ci

SWEE_SWE_FLAGS=
run:
	./bin/swe-swe $(SWE_SWE_FLAGS)

clean:
	rm -rf bin $(ELM_OUTPUT)

docker-compose-dev-up:
	WORKSPACE_DIR=${WORKSPACE_DIR} docker-compose --env-file .env -f docker/dev/docker-compose.yml up

docker-compose-dev-down:
	WORKSPACE_DIR=${WORKSPACE_DIR} docker-compose --env-file .env -f docker/dev/docker-compose.yml down

docker-compose-dev-build:
	WORKSPACE_DIR=${WORKSPACE_DIR} docker-compose --env-file .env -f docker/dev/docker-compose.yml build

docker-compose-dev-logs:
	WORKSPACE_DIR=${WORKSPACE_DIR} docker-compose --env-file .env -f docker/dev/docker-compose.yml logs -f
