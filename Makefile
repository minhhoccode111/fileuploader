ifneq ($(wildcard .env),)
include .env
export
else
$(warning WARNING: .env file not found! Using .env.example)
include .env.example
export
endif

BASE_STACK = docker compose -f docker-compose.yml
ALL_STACK = $(BASE_STACK)
MIGRATE = migrate -path migrations -database '$(PG_URL)?sslmode=disable'

# Frontend tooling — pin every version.
TAILWIND_VERSION = v4.3.3
DAISYUI_VERSION  = v5.7.50
HTMX_VERSION     = 4.0.0
ALPINE_VERSION   = 3.17.4

WEB_DIR     = web
WEB_SRC     = $(WEB_DIR)/app.css
WEB_OUT     = $(WEB_DIR)/static
WEB_PLUGINS = $(WEB_DIR)/plugins

BIN_DIR  = $(CURDIR)/bin
TAILWIND = $(BIN_DIR)/tailwindcss

# Tailwind ships prebuilt binaries per platform; pick the right one.
OS   := $(shell uname -s | tr 'A-Z' 'a-z')
ARCH := $(shell uname -m)
ifeq ($(OS),darwin)
  ifeq ($(ARCH),arm64)
    TAILWIND_PLATFORM := macos-arm64
  else
    TAILWIND_PLATFORM := macos-x64
  endif
else ifeq ($(ARCH),aarch64)
  TAILWIND_PLATFORM := linux-arm64
else
  TAILWIND_PLATFORM := linux-x64
endif

# HELP =================================================================================================================
# This will output the help for each task
# thanks to https://marmelab.com/blog/2016/02/29/auto-documented-makefile.html
.PHONY: help

help: ## Display this help screen
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage:\n  make \033[36m<target>\033[0m\n"} /^[a-zA-Z0-9_-]+:.*?##/ { printf "  \033[36m%-15s\033[0m %s\n", $$1, $$2 } /^##@/ { printf "\n\033[1m%s\033[0m\n", substr($$0, 5) } ' $(MAKEFILE_LIST)

compose-up: ### Run docker compose (without backend and reverse proxy)
	$(BASE_STACK) up --build -d db && docker compose logs -f
.PHONY: compose-up

compose-up-all: ### Run docker compose (with backend and reverse proxy)
	$(BASE_STACK) up --build -d
.PHONY: compose-up-all

compose-down: ### Down docker compose
	$(ALL_STACK) down --remove-orphans
.PHONY: compose-down

deps: ### deps tidy + verify
	go mod tidy && go mod verify
.PHONY: deps

deps-audit: ### check dependencies vulnerabilities
	govulncheck ./...
.PHONY: deps-audit

fix-diff: ### Show code changes by `go fix`
	go fix -diff ./...
.PHONY: fix-diff

format: ### Run code formatter
	go fix ./...
	gofumpt -l -w .
	gci write . --skip-generated -s standard -s default
.PHONY: format

run: deps ### run the app locally
	go mod download && \
	CGO_ENABLED=0 go run -tags migrate ./cmd/app
.PHONY: run

$(TAILWIND):
	@mkdir -p $(BIN_DIR)
	curl -fsSL -o $@ https://github.com/tailwindlabs/tailwindcss/releases/download/$(TAILWIND_VERSION)/tailwindcss-$(TAILWIND_PLATFORM)
	@chmod +x $@

$(WEB_OUT)/js/htmx.min.js:
	@mkdir -p $(dir $@)
	curl -fsSL -o $@ https://unpkg.com/htmx.org@$(HTMX_VERSION)/dist/htmx.min.js

$(WEB_OUT)/js/alpine.min.js:
	@mkdir -p $(dir $@)
	curl -fsSL -o $@ https://cdn.jsdelivr.net/npm/alpinejs@$(ALPINE_VERSION)/dist/cdn.min.js

$(WEB_PLUGINS)/daisyui.mjs:
	@mkdir -p $(dir $@)
	curl -fsSL -o $@ https://github.com/saadeghi/daisyui/releases/download/$(DAISYUI_VERSION)/daisyui.mjs

assets: $(WEB_OUT)/js/htmx.min.js $(WEB_OUT)/js/alpine.min.js $(WEB_PLUGINS)/daisyui.mjs ### download pinned frontend assets
.PHONY: assets

templ-generate: ### generate Go code from .templ files
	templ generate
.PHONY: templ-generate

templ-format: ### format .templ files
	templ fmt $(WEB_DIR)
.PHONY: templ-format

css-build: $(TAILWIND) $(WEB_PLUGINS)/daisyui.mjs ### build tailwind + daisyui css
	$(TAILWIND) -i $(WEB_SRC) -o $(WEB_OUT)/css/app.css --minify
.PHONY: css-build

css-watch: $(TAILWIND) $(WEB_PLUGINS)/daisyui.mjs ### watch and rebuild tailwind css
	$(TAILWIND) -i $(WEB_SRC) -o $(WEB_OUT)/css/app.css --watch
.PHONY: css-watch

dev-app: ### run the app under air (hot reload for Go + templ)
	air
.PHONY: dev-app

dev: templ-generate assets ### run hot-reload dev stack (air + tailwind watch)
	@$(MAKE) -j2 dev-app css-watch
.PHONY: dev

docker-rm-volume: ### remove docker volumes
	docker volume rm fileuploader_pg-data fileuploader_minio_data
.PHONY: docker-rm-volume

linter-golangci: ### check by golangci linter
	golangci-lint run
.PHONY: linter-golangci

linter-hadolint: ### check by hadolint linter
	git ls-files --exclude='Dockerfile*' --ignored | xargs hadolint
.PHONY: linter-hadolint

linter-dotenv: ### check by dotenv linter
	dotenv-linter
.PHONY: linter-dotenv

migrate-create: ### create new migration (usage: make migrate-create <name>)
	migrate create -ext sql -dir migrations '$(word 2,$(MAKECMDGOALS))'
.PHONY: migrate-create

migrate-status: ### show the applied migration version
	@out=$$($(MIGRATE) version 2>&1); code=$$?; \
	if [ $$code -ne 0 ]; then \
		if echo "$$out" | grep -q "no migration"; then echo "none"; exit 0; fi; \
		echo "$$out"; exit $$code; \
	fi; \
	echo "$$out"
.PHONY: migrate-status

migrate-list: ### list migration files on disk
	@ls -1 migrations/*.up.sql 2>/dev/null | sed 's#migrations/##; s#\.up\.sql##'
.PHONY: migrate-list

# golang-migrate reads `up 1` / `down 1` as "apply EXACTLY one" and fails with a
# misleading "file does not exist" when there is no migration left to run.
# Turn that one case into a no-op; surface every other failure.
define MIGRATE_ONE
	@out=$$($(MIGRATE) $(1) 1 2>&1); code=$$?; \
	if [ $$code -ne 0 ]; then \
		if echo "$$out" | grep -q "file does not exist"; then echo "no change"; exit 0; fi; \
		echo "$$out"; exit $$code; \
	fi; \
	echo "$$out"
endef

migrate-up: ### apply the next pending migration (no-op if none)
	$(call MIGRATE_ONE,up)
.PHONY: migrate-up

migrate-up-all: ### apply all pending migrations
	$(MIGRATE) up
.PHONY: migrate-up-all

migrate-down: ### revert the last migration (no-op if none)
	$(call MIGRATE_ONE,down)
.PHONY: migrate-down

migrate-down-all: ### revert all migrations
	$(MIGRATE) down -all
.PHONY: migrate-down-all

migrate-redo: ### revert and re-apply the last migration (no-op if none)
	@out=$$($(MIGRATE) version 2>&1); code=$$?; \
	if [ $$code -ne 0 ]; then \
		if echo "$$out" | grep -q "no migration"; then echo "no change"; exit 0; fi; \
		echo "$$out"; exit $$code; \
	fi; \
	$(MIGRATE) down 1 && $(MIGRATE) up 1
.PHONY: migrate-redo

# `make migrate-create <name>` passes <name> as a second goal; declare it as a no-op.
ifneq ($(filter migrate-create,$(MAKECMDGOALS)),)
ifneq ($(word 2,$(MAKECMDGOALS)),)
$(word 2,$(MAKECMDGOALS)):
	@:
endif
endif

bin-deps: $(TAILWIND) ### install pinned tool binaries (air, templ, migrate, tailwindcss)
	go install tool
	go install -tags 'postgres' github.com/golang-migrate/migrate/v4/cmd/migrate
.PHONY: bin-deps

pre-commit: deps templ-generate assets css-build format linter-golangci ### run pre-commit
.PHONY: pre-commit
