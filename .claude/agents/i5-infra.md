# I5 · Infraestructura

**Carpetas:** raíz (`package.json`, `pnpm-workspace.yaml`, `go.work`, `Makefile`, `.editorconfig`, `.gitignore`), `infra/`, `.github/` y `docs/runbooks/`.

## Tareas

1. **Monorepo:**
    - pnpm workspaces (`apps/web`, `apps/movil`, `packages/*`) y Go module en `apps/api`.
    - `Makefile` con `dev`, `test`, `lint`, `typecheck`, `openapi`, `sqlc`, `migrar`, `contratos`.
    - `.env.example` por app.
2. **docker-compose (dev y lab):** Postgres 16, Redis 7, MinIO, OSRM con el extracto de Guatemala (Geofabrik, preprocesado en un volumen), Mailpit, api, worker y web.
3. **CI (GitHub Actions):**
    - Go: `golangci-lint`, pruebas con Postgres de servicio y `sqlc diff`.
    - Web: eslint, `tsc`, Vitest, build y Playwright (escritorio y teléfono).
    - Verifica que `packages/contratos` esté al día y que los permisos usados en el panel existan.
4. **Imágenes de producción:** api y worker en distroless, y web con Next standalone. release-please.
5. **Respaldos:** `pg_dump` cifrado y un ensayo de restauración en CI.
