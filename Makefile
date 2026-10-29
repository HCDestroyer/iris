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
# Comando que imprime el OpenAPI 3.1 de la API por stdout (cmd/openapi de I1).
OPENAPI_CMD   ?= go run ./cmd/openapi -salida -
MIGRACIONES   ?= migraciones

# Etiqueta y versión de las imágenes (make imagenes). VERSION queda en main.version de api/worker.
IMAGEN_TAG ?= dev
VERSION    ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo 0.0.0-dev)
SCRIPTS    := $(wildcard scripts/*.sh infra/*/*.sh)
# CA extra para construir detrás de un proxy que intercepta TLS (ver .env.example).
coma       := ,
BUILD_CA   := $(if $(IRIS_BUILD_CA_EXTRA),--secret id=ca_extra$(coma)src=$(IRIS_BUILD_CA_EXTRA))

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

.PHONY: dev dev-app parar logs certificados
certificados: ## Certificados de dev de Mailpit con TLS (CA para IRIS_CORREO_CA; idempotente)
	infra/mailpit/certificados.sh

dev: certificados ## Levanta postgres, redis, minio (+ bucket), mailpit (TLS) y osrm
	$(COMPOSE) up -d --wait postgres redis mailpit
	$(COMPOSE) up -d minio minio-init osrm
	@echo "Listo. Mailpit: http://localhost:8025 (SMTP :1025 con STARTTLS) · MinIO: http://localhost:9001"
	@echo "OSRM necesita datos una vez: infra/osrm/preparar.sh"

dev-app: certificados ## dev + migrar, api, worker y web construidos desde apps/ (perfil app)
	@for d in $(API_DIR) $(WEB_DIR); do \
		[ -f "$$d/Dockerfile" ] || { echo "ERROR: falta $$d/Dockerfile para el perfil app." >&2; exit 1; }; \
	done
	$(COMPOSE) --profile app up -d --build
	$(COMPOSE) --profile app up -d --wait api web
	@echo "Listo. Panel: http://localhost:$${IRIS_WEB_PUERTO:-3000} · API: http://localhost:$${IRIS_API_PUERTO:-8080}/api/v1/salud"

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

lint-infra: ## bash -n + shellcheck de scripts y docker compose config (avisa si falta una herramienta)
	@for s in $(SCRIPTS); do bash -n "$$s"; done
	@if command -v shellcheck >/dev/null 2>&1; then \
		shellcheck $(SCRIPTS) && echo "shellcheck: ok"; \
	else \
		echo "lint-infra: omitido shellcheck (no instalado; CI sí lo corre)"; \
	fi
	@if docker compose version >/dev/null 2>&1; then \
		docker compose -f infra/docker-compose.yml --env-file .env.example --profile app config --quiet && echo "compose: ok"; \
		docker compose -p iris-carga -f infra/docker-compose.yml -f infra/carga/compose.yml --env-file .env.example --profile app config --quiet && echo "compose de carga: ok"; \
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

.PHONY: e2e
e2e: ## Playwright del panel (escritorio y teléfono; construye apps/web salvo IRIS_E2E_SIN_BUILD=1)
	pnpm --filter @iris/web e2e

# --- Imágenes y respaldos ---------------------------------------------------------------------

.PHONY: imagenes respaldo ensayo-respaldo
imagenes: ## Construye iris/api, iris/worker e iris/web:$(IMAGEN_TAG) (VERSION=… para main.version)
	docker build -f $(API_DIR)/Dockerfile --target api $(BUILD_CA) --build-arg VERSION=$(VERSION) -t iris/api:$(IMAGEN_TAG) .
	docker build -f $(API_DIR)/Dockerfile --target worker $(BUILD_CA) --build-arg VERSION=$(VERSION) -t iris/worker:$(IMAGEN_TAG) .
	docker build -f $(WEB_DIR)/Dockerfile $(BUILD_CA) -t iris/web:$(IMAGEN_TAG) .

respaldo: ## pg_dump cifrado con age de IRIS_DB_URL (infra/respaldo/respaldar.sh; --compose: ARGS=--compose)
	infra/respaldo/respaldar.sh $(ARGS)

ensayo-respaldo: ## Respalda IRIS_DB_URL, restaura en <base>_ensayo y compara conteos
	$(requiere_db)
	infra/respaldo/ensayo.sh "$$IRIS_DB_URL" "$$(psql "$$IRIS_DB_URL" -XAtqc 'select current_database()')_ensayo"

# --- Pruebas de carga (docs/runbooks/carga.md) -----------------------------------------------

.PHONY: carga carga-corta carga-limpiar
carga: ## Carga completa: build, compose iris-carga, siembra, k6 15 min (50 técnicos, 20 del panel) y verificación
	infra/carga/carga.sh

carga-corta: ## Carga corta para local: 2 min, 10 técnicos y 5 del panel (misma siembra y umbrales)
	CARGA_DURACION=$${CARGA_DURACION:-2m} CARGA_TECNICOS=$${CARGA_TECNICOS:-10} CARGA_PANEL=$${CARGA_PANEL:-5} infra/carga/carga.sh

carga-limpiar: ## Baja el compose de carga y borra su base (volumen) e infra/carga/.salida
	infra/carga/carga.sh limpiar

# --- Generación -------------------------------------------------------------------------------

.PHONY: openapi sqlc migrar migracion contratos
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

migrar: ## Migraciones (goose, embebidas) y sincronización de permisos sobre IRIS_DB_URL
	$(requiere_api)
	$(requiere_db)
	cd $(API_DIR) && go run ./cmd/iris migrar

migracion: ## Crea una migración con marca de tiempo real: make migracion NOMBRE=soporte_tickets
	$(requiere_api)
	@[ -n "$(NOMBRE)" ] || { echo "uso: make migracion NOMBRE=<nombre>" >&2; exit 1; }
	cd $(API_DIR) && $(GOOSE) -dir $(MIGRACIONES) create $(NOMBRE) sql

contratos: ## make openapi (si existe apps/api) + pnpm contratos:build
ifneq ($(HAY_API),)
	$(MAKE) --no-print-directory openapi
else
	@echo "contratos: omitido make openapi ($(API_DIR) no existe); se regeneran los tipos del openapi.json actual"
endif
	pnpm contratos:build
