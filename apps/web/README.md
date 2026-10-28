# @iris/web · panel

Panel web de Iris (Next.js 15, App Router, React 19, Tailwind 4) con la identidad Horus de
`@iris/ui`. Dueño: I3. `app/campo` es de I4 y `/soporte` (portal) llega en la tarea 7.

## Qué hace

- **Sesión:** `/entrar` (correo + contraseña, paso MFA, 401/422/429 con «espera N min»,
  `mfa_bloqueado`). `middleware.ts` manda a `/entrar?siguiente=…` si no hay cookie `iris_sesion`;
  el layout del panel lee `GET /api/v1/yo` y un 401 también vuelve al inicio de sesión.
- **Menú por permisos** (`lib/menu.ts`): nombres exactos del enum `Permiso` del OpenAPI. Los
  módulos sin permiso en el contrato (tickets, órdenes, agenda, despacho, comercial) se ven como
  «Próximamente» y un `satisfies` hace fallar `typecheck` cuando el permiso aparezca.
- **Usuarios** (`/usuarios`): lista de solo lectura con paginación por cursor.
- **Cliente HTTP** (`lib/api/cliente.ts`): `openapi-fetch` de `@iris/contratos`, cookies y
  `X-CSRF-Token` (cookie `iris_csrf`) en todo método que cambia estado; errores problem+json →
  `ErrorApi` (`codigo`, `estado`, `errores[]`, `reintentarEn`).
- **Proxy** (`app/api/[...ruta]/route.ts`): `/api/*` → `IRIS_API_URL/api/*` en tiempo de
  ejecución, así las cookies son del mismo origen y la imagen sirve en cualquier entorno.

No emite ni consume eventos.

## Uso

```sh
cp apps/web/.env.example apps/web/.env.local     # IRIS_API_URL=http://localhost:8080
pnpm --filter @iris/web dev                       # http://localhost:3000
pnpm --filter @iris/web lint | typecheck | test   # eslint, tsc, Vitest
pnpm --filter @iris/web e2e                       # build + Playwright (escritorio/teléfono, claro/oscuro)
IRIS_E2E_SIN_BUILD=1 pnpm --filter @iris/web e2e  # reutiliza el build
IRIS_CAPTURAS=/ruta pnpm --filter @iris/web e2e capturas   # capturas para revisión visual
```

Playwright simula la API con `page.route` (`e2e/api-falsa.ts`, fixtures tipados con
`@iris/contratos`); no necesita la API Go. Puerto de pruebas: 3503.

Imagen: `docker build -f apps/web/Dockerfile .` (Next `standalone`, `PORT=3000`).

Reglas: nada de `<button>`/`<input>` crudos (ESLint lo impide; usa `@iris/ui`), sin `any`,
4 espacios, errores visibles con «Reintentar» y nunca datos calculados sobre lecturas fallidas.
