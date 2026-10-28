# syntax=docker/dockerfile:1
# Panel web de Iris: Next.js `output: "standalone"`. Contexto de build: la raíz del repo.
#   docker build -f apps/web/Dockerfile -t iris/web .
# Variables en ejecución: PORT (3000), IRIS_API_URL (proxy /api → API), IRIS_BASE_URL.

FROM node:22-alpine AS base
ENV PNPM_HOME=/pnpm PATH=/pnpm:$PATH NEXT_TELEMETRY_DISABLED=1
RUN corepack enable
WORKDIR /repo

# Dependencias solo con los manifiestos (capa cacheable).
FROM base AS deps
COPY package.json pnpm-lock.yaml pnpm-workspace.yaml ./
COPY apps/web/package.json apps/web/
COPY packages/contratos/package.json packages/contratos/
COPY packages/ui/package.json packages/ui/
RUN --mount=type=cache,id=pnpm,target=/pnpm/store pnpm install --frozen-lockfile --filter "@iris/web..."

FROM deps AS build
COPY packages/contratos packages/contratos
COPY packages/ui packages/ui
COPY apps/web apps/web
RUN pnpm --filter @iris/web build

FROM node:22-alpine AS web
WORKDIR /app
ENV NODE_ENV=production PORT=3000 HOSTNAME=0.0.0.0 NEXT_TELEMETRY_DISABLED=1
COPY --from=build --chown=node:node /repo/apps/web/.next/standalone ./
COPY --from=build --chown=node:node /repo/apps/web/.next/static ./apps/web/.next/static
USER node
EXPOSE 3000
CMD ["node", "apps/web/server.js"]
