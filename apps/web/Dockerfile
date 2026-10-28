# syntax=docker/dockerfile:1
# Panel web de Iris: Next.js `output: "standalone"`. Contexto de build: la raíz del repo.
#   docker build -f apps/web/Dockerfile -t iris/web .
# Variables en ejecución: PORT (3000), HOSTNAME (0.0.0.0: escucha en todas las interfaces del
# contenedor; con el hostname del contenedor Next solo escucharía en esa IP), IRIS_API_URL (proxy
# /api → API, leída en cada petición) e IRIS_BASE_URL.
#
# Detrás de un proxy que intercepta TLS, su CA solo para instalar dependencias:
#   docker build --secret id=ca_extra,src=/ruta/ca.pem …   (compose: IRIS_BUILD_CA_EXTRA)

FROM node:22.23.4-alpine AS base
ENV PNPM_HOME=/pnpm PATH=/pnpm:$PATH NEXT_TELEMETRY_DISABLED=1 COREPACK_ENABLE_DOWNLOAD_PROMPT=0
RUN corepack enable
WORKDIR /repo

# Dependencias solo con los manifiestos (capa cacheable).
FROM base AS deps
COPY package.json pnpm-lock.yaml pnpm-workspace.yaml ./
COPY apps/web/package.json apps/web/
COPY packages/contratos/package.json packages/contratos/
COPY packages/sync/package.json packages/sync/
COPY packages/ui/package.json packages/ui/
RUN --mount=type=cache,id=pnpm,target=/pnpm/store --mount=type=secret,id=ca_extra,required=false \
    if [ -s /run/secrets/ca_extra ]; then export NODE_EXTRA_CA_CERTS=/run/secrets/ca_extra; fi; \
    pnpm install --frozen-lockfile --filter "@iris/web..."

FROM deps AS build
COPY packages/contratos packages/contratos
COPY packages/sync packages/sync
COPY packages/ui packages/ui
COPY apps/web apps/web
RUN pnpm --filter @iris/web build

FROM node:22.23.4-alpine AS web
WORKDIR /app
ENV NODE_ENV=production PORT=3000 HOSTNAME=0.0.0.0 NEXT_TELEMETRY_DISABLED=1 TZ=UTC
COPY --from=build --chown=node:node /repo/apps/web/.next/standalone ./
COPY --from=build --chown=node:node /repo/apps/web/.next/static ./apps/web/.next/static
# public/ lo genera el build (sw.js de la app de campo, Serwist); standalone no lo incluye.
COPY --from=build --chown=node:node /repo/apps/web/public ./apps/web/public
USER node
EXPOSE 3000
HEALTHCHECK --interval=10s --timeout=3s --start-period=20s --retries=5 \
    CMD ["node", "-e", "fetch('http://127.0.0.1:'+(process.env.PORT||3000)+'/entrar').then(r=>process.exit(r.ok?0:1),()=>process.exit(1))"]
CMD ["node", "apps/web/server.js"]
