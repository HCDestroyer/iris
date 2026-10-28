# Objetivos locales de apps/api. El Makefile de la raíz (I5) los llama:
#   make -C apps/api test | lint | sqlc | openapi | migrar
# Las herramientas (sqlc, goose, golangci-lint) se instalan aparte; ver README.md.

# migrar/servir/worker leen IRIS_DB_URL del entorno; las pruebas usan IRIS_DB_URL o
# IRIS_DB (y por defecto iris_test_i1), siempre una base iris_test_*.
GO       ?= go

.PHONY: compilar test lint vet sqlc sqlc-verificar openapi openapi-verificar migrar servir worker migracion verificar

compilar:
	$(GO) build -o bin/ ./cmd/...

test:
	$(GO) test -count=1 ./...

vet:
	$(GO) vet ./...

lint: vet
	golangci-lint run ./...

sqlc:
	sqlc generate

# Falla si el código generado no corresponde a las consultas y migraciones.
sqlc-verificar:
	sqlc diff

openapi:
	$(GO) run ./cmd/openapi -salida ../../packages/contratos/openapi.json

# Falla si packages/contratos/openapi.json no está al día.
openapi-verificar:
	$(GO) run ./cmd/openapi -salida - | diff -u ../../packages/contratos/openapi.json -

migrar:
	$(GO) run ./cmd/iris migrar

servir:
	$(GO) run ./cmd/iris

worker:
	$(GO) run ./cmd/worker

# make migracion NOMBRE=soporte_tickets  → migraciones/<marca de tiempo real>_soporte_tickets.sql
migracion:
	@test -n "$(NOMBRE)" || (echo "uso: make migracion NOMBRE=<nombre>" && exit 1)
	goose -dir migraciones create $(NOMBRE) sql

# Lo que CI debe correr para apps/api.
verificar: lint sqlc-verificar openapi-verificar test
