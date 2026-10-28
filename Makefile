# Iris · tareas del monorepo. `make ayuda` lista los objetivos.
#
# Objetivos de Go (lint-go, test-go, typecheck-go, openapi, sqlc, migrar) corren en apps/api y
# fallan con un mensaje claro si apps/api todavía no existe. Los agregados (lint, typecheck, test,
# contratos) corren lo que exista y dicen explícitamente qué omitieron.
#
# Variables: IRIS_DB_URL (pruebas y migraciones; en pruebas, una base propia iris_test_<agente>),
# ENV_FILE (compose; por defecto .env o .env.example).

SHELL := /usr/bin/env bash
.SHELLFLAGS := -euo pipefail -c
.DEFAULT_GOAL := ayuda

API_DIR      := apps/api
WEB_DIR      := apps/web
CONTRATOS    := packages/contratos
ENV_FILE     ?= $(if $(wildcard .env),.env,.env.example)
COMPOSE      := docker compose -f infra/docker-compose.yml --env-file $(ENV_FILE)

# Herramientas de Go fijadas (las últimas compatibles con Go 1.24). Se pueden reemplazar por un
# binario local: make sqlc SQLC=sqlc
SQLC_VERSION  ?= v1.30.0
GOOSE_VERSION ?= v3.26.0
SQLC          ?= go run github.com/sqlc-dev/sqlc/cmd/sqlc@$(SQLC_VERSION)
GOOSE         ?= go run github.com/pressly/goose/v3/cmd/goose@$(GOOSE_VERSION)
GOLANGCI_LINT ?= golangci-lint
# Comando que imprime el OpenAPI 3.1 de la API por stdout (lo implementa I1 en cmd/iris).
OPENAPI_CMD   ?= go run ./cmd/iris openapi
MIGRACIONES   ?= migraciones

HAY_API := $(wildcard $(API_DIR)/go.mod)
HAY_WEB := $(wildcard $(WEB_DIR)/package.json)

define requiere_api
	@if [ ! -f "$(API_DIR)/go.mod" ]; then \
		echo "ERROR: '$@' necesita $(API_DIR) (módulo Go con go.mod), que todavía no existe." >&2; \
		echo "       Lo crea I1 (.claude/agents/i1-nucleo.md)." >&2; \
		exit 1; \
	fi
endef

define requiere_db
	@if [ -z "$${IRIS_DB_URL:-}" ]; then \
		echo "ERROR: '$@' necesita IRIS_DB_URL (ver .env.example)." >&2; \
		exit 1; \
	fi
endef

.PHONY: ayuda
ayuda: ## Lista los objetivos
	@grep -hE '^[a-z0-9-]+:.*## ' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*## "} {printf "  %-14s %s\n", $$1, $$2}'

# --- Entorno local ----------------------------------------------------------------------------

.PHONY: dev dev-app parar logs
dev: ## Levanta postgres, redis, minio (+ bucket), mailpit y osrm
	$(COMPOSE) up -d --wait postgres redis mailpit
	$(COMPOSE) up -d minio minio-init osrm
	@echo "Listo. Mailpit: http://localhost:8025 · MinIO: http://localhost:9001"
	@echo "OSRM necesita datos una vez: infra/osrm/preparar.sh"

dev-app: ## Igual que dev y además api, worker y web (perfil app; requiere apps/api y apps/web)
	@for d in $(API_DIR) $(WEB_DIR); do \
		[ -f "$$d/Dockerfile" ] || { echo "ERROR: falta $$d/Dockerfile para el perfil app." >&2; exit 1; }; \
	done
	$(COMPOSE) --profile app up -d --build

parar: ## Detiene el compose de Iris (no borra volúmenes)
	$(COMPOSE) --profile app stop

logs: ## Sigue los logs del compose
	$(COMPOSE) --profile app logs -f --tail 100

# --- Verificación -----------------------------------------------------------------------------

.PHONY: lint lint-go lint-ts lint-infra
lint: lint-infra lint-ts ## golangci-lint + eslint + scripts y compose (lo que exista)
ifneq ($(HAY_API),)
	$(MAKE) --no-print-directory lint-go
else
	@echo "lint: omitido Go ($(API_DIR) no existe)"
endif

lint-go: ## golangci-lint en apps/api
	$(requiere_api)
	cd $(API_DIR) && $(GOLANGCI_LINT) run ./...

lint-ts: ## eslint de los paquetes del workspace (pnpm lint)
	pnpm lint

lint-infra: ## shellcheck de scripts y docker compose config
	shellcheck scripts/*.sh infra/osrm/*.sh infra/minio/*.sh
	@if docker compose version >/dev/null 2>&1; then \
		docker compose -f infra/docker-compose.yml --env-file .env.example --profile app config --quiet && echo "compose: ok"; \
	else \
		echo "lint-infra: omitido docker compose config (docker compose no instalado)"; \
	fi

.PHONY: typecheck typecheck-go
typecheck: ## go vet + tsc (lo que exista)
	pnpm typecheck
ifneq ($(HAY_API),)
	$(MAKE) --no-print-directory typecheck-go
else
	@echo "typecheck: omitido Go ($(API_DIR) no existe)"
endif

typecheck-go: ## go build y go vet en apps/api
	$(requiere_api)
	cd $(API_DIR) && go build ./... && go vet ./...

.PHONY: test test-go test-ts
test: test-ts ## Pruebas de Go (con IRIS_DB_URL) y de los paquetes TS (lo que exista)
ifneq ($(HAY_API),)
	$(MAKE) --no-print-directory test-go
else
	@echo "test: omitido Go ($(API_DIR) no existe)"
endif

test-go: ## go test ./... en apps/api contra IRIS_DB_URL
	$(requiere_api)
	$(requiere_db)
	cd $(API_DIR) && go test -count=1 ./...

test-ts: ## pnpm test
	pnpm test

# --- Generación -------------------------------------------------------------------------------

.PHONY: openapi sqlc migrar contratos
openapi: ## Escribe packages/contratos/openapi.json desde apps/api
	$(requiere_api)
	@tmp="$$(mktemp)"; \
	if ! (cd $(API_DIR) && $(OPENAPI_CMD)) >"$$tmp"; then rm -f "$$tmp"; echo "ERROR: falló '$(OPENAPI_CMD)'" >&2; exit 1; fi; \
	if ! node -e 'JSON.parse(require("fs").readFileSync(process.argv[1], "utf8"))' "$$tmp"; then \
		rm -f "$$tmp"; echo "ERROR: '$(OPENAPI_CMD)' no imprimió JSON válido" >&2; exit 1; \
	fi; \
	mv "$$tmp" $(CONTRATOS)/openapi.json; \
	echo "openapi: $(CONTRATOS)/openapi.json actualizado"

sqlc: ## sqlc generate en apps/api
	$(requiere_api)
	cd $(API_DIR) && $(SQLC) generate

migrar: ## goose up sobre IRIS_DB_URL con apps/api/migraciones
	$(requiere_api)
	$(requiere_db)
	@[ -d "$(API_DIR)/$(MIGRACIONES)" ] || { echo "ERROR: falta $(API_DIR)/$(MIGRACIONES)" >&2; exit 1; }
	cd $(API_DIR) && $(GOOSE) -dir $(MIGRACIONES) postgres "$$IRIS_DB_URL" up

contratos: ## make openapi (si existe apps/api) + pnpm contratos:build
ifneq ($(HAY_API),)
	$(MAKE) --no-print-directory openapi
else
	@echo "contratos: omitido make openapi ($(API_DIR) no existe); se regeneran los tipos del openapi.json actual"
endif
	pnpm contratos:build
