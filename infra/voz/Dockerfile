# syntax=docker/dockerfile:1
# «Iris Voz»: imagen del Servidor de IA (contrato voz.md §4.7). Contexto de build: la raíz del repo.
#   docker build -f infra/voz/Dockerfile --build-arg VERSION=0.1.0 -t iris-voz .
#   docker build -f infra/voz/Dockerfile --target ollama-falso -t iris-voz-ollama-falso .   (solo CI)
#
# Un solo binario, voz-puerta, con las órdenes `servir` (la puerta), `medidor` (lee la GPU, sin red)
# y `proxy-descarga` (única salida, solo al registro de modelos). No trae Postgres, Redis ni nada de
# Iris: el paquete internal/voz no importa mensajería ni la base (lo comprueba su prueba).
#
# La imagen final es distroless con glibc (base-debian12:nonroot, uid 65532, sin shell): el medidor
# ejecuta el nvidia-smi que inyecta el runtime de NVIDIA, que necesita glibc.

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
    go build -ldflags "-s -w -X main.version=${VERSION}" -o /salida/ ./cmd/voz-puerta ./cmd/voz-ollama-falso
# Directorios de los volúmenes, con el dueño que tendrá el proceso (Docker copia el dueño al crear
# un volumen con nombre).
RUN mkdir -p /vol/estado /vol/medidor /vol/modelos/blobs /vol/modelos/manifests && chown -R 65532:65532 /vol && chmod 700 /vol/estado

# Ollama falso para probar el paquete sin GPU (compose de CI). No se publica.
FROM gcr.io/distroless/static-debian12:nonroot@sha256:afa5c872c891853ca7fcf1f12c3edb23f7eeef36189728842dd51042ff57f7ab AS ollama-falso
COPY --from=build /salida/voz-ollama-falso /usr/local/bin/voz-ollama-falso
USER nonroot:nonroot
EXPOSE 11434
ENTRYPOINT ["/usr/local/bin/voz-ollama-falso"]

FROM gcr.io/distroless/base-debian12:nonroot@sha256:7f0c72cd138b442ae0deeb69c08b1acf5525439ba251a49ad93c320a061567e5 AS voz
ENV TZ=UTC
COPY --from=build /salida/voz-puerta /usr/local/bin/voz-puerta
COPY --from=build --chown=65532:65532 /vol/estado /estado
COPY --from=build --chown=65532:65532 /vol/medidor /medidor
COPY --from=build --chown=65532:65532 /vol/modelos /modelos
COPY infra/voz/manifiesto.json /etc/iris-voz/manifiesto.json
USER nonroot:nonroot
WORKDIR /home/nonroot
EXPOSE 8443
ENTRYPOINT ["/usr/local/bin/voz-puerta"]
CMD ["servir"]
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 CMD ["/usr/local/bin/voz-puerta", "salud"]
