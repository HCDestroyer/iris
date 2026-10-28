# syntax=docker/dockerfile:1
# API y worker de Iris (Go). Contexto de build: la raíz del repo.
#   docker build -f apps/api/Dockerfile --target api    -t iris/api .
#   docker build -f apps/api/Dockerfile --target worker -t iris/worker .
#   --build-arg VERSION=0.1.0   (queda en main.version; se ve en el log de arranque)
#
# Imágenes finales distroless (static, nonroot: uid 65532, sin shell). Traen certificados de CA y
# tzdata. Las migraciones van embebidas en el binario: `iris migrar` (servicio `migrar` del compose).
# Para la CA extra del correo de dev (IRIS_CORREO_CA) se monta el PEM; no se copia a la imagen.
#
# Detrás de un proxy que intercepta TLS (red corporativa), pasa su CA solo para descargar módulos:
#   docker build --secret id=ca_extra,src=/ruta/ca.pem …   (compose: IRIS_BUILD_CA_EXTRA)
# No queda en ninguna capa de la imagen final.

FROM golang:1.24.13-bookworm AS build
WORKDIR /src
ENV CGO_ENABLED=0 GOFLAGS=-trimpath
COPY apps/api/go.mod apps/api/go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=secret,id=ca_extra,required=false \
    if [ -s /run/secrets/ca_extra ]; then export SSL_CERT_FILE=/tmp/ca.pem; \
        cat /etc/ssl/certs/ca-certificates.crt /run/secrets/ca_extra >"$SSL_CERT_FILE"; fi; \
    go mod download
COPY apps/api/ ./
ARG VERSION=0.0.0-dev
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build \
    go build -ldflags "-s -w -X main.version=${VERSION}" -o /salida/ ./cmd/iris ./cmd/worker

# Base común de las dos imágenes finales (fijada por digest: static-debian12:nonroot).
FROM gcr.io/distroless/static-debian12:nonroot@sha256:afa5c872c891853ca7fcf1f12c3edb23f7eeef36189728842dd51042ff57f7ab AS final
ENV TZ=UTC
USER nonroot:nonroot
WORKDIR /home/nonroot

# Worker (River: outbox, webhooks entrantes, correo, crons).
FROM final AS worker
COPY --from=build /salida/worker /usr/local/bin/worker
ENTRYPOINT ["/usr/local/bin/worker"]

# API HTTP. Órdenes: servir (por defecto), migrar, salud, clave rotar, admin crear, portal acceso,
# integracion crear…
# Va al final para que un `docker build` sin --target construya la API.
FROM final AS api
COPY --from=build /salida/iris /usr/local/bin/iris
ENV PORT=8080
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/iris"]
CMD ["servir"]
# Sin shell ni curl: la propia orden hace el GET a /api/v1/salud en $PORT (sale 0 o 1).
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 CMD ["/usr/local/bin/iris", "salud"]
