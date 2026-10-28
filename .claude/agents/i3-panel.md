# I3 · Panel web

**Carpetas:** `apps/web` (excepto `app/campo`) y `packages/ui`.

## Tareas

1. **Esqueleto:**
    - Next.js 15 (App Router), Tailwind 4 y shadcn/ui en `packages/ui`.
    - Identidad Horus a partir de `docs/marca/horus-tokens.css`: teal como único color de marca, aura, vidrio en la barra lateral y la superior, y tarjetas casi sólidas.
    - Logo `docs/marca/logo-mark.svg` como favicon y en la cabecera («Iris»).
    - Modo claro y oscuro; íconos de Lucide; fuente del sistema y cifras tabulares.
2. **Sesión y permisos:** inicio de sesión, MFA y menú por permisos con los nombres exactos del enum del OpenAPI. Cliente con `openapi-fetch` desde `packages/contratos`.
3. **Tickets:**
    - Bandeja con vistas, filtros y SLA visible (icono + texto + color).
    - Detalle con conversación, notas internas, historial, ficha del cliente de Argos (con `stale` visible) y «Ver en Argos/Horus».
    - Alta rápida.
4. **Órdenes:** tablero y detalle con evidencias (galería, firma, mapa del GPS), materiales y equipos, y estado de los pendientes en Argos con «Reintentar».
5. **Despacho:** calendario por técnico, mapa (MapLibre) con órdenes y técnicos en vivo, arrastrar y soltar, y ruta propuesta frente a la real.
6. **Comercial:** embudo, mapa de prospectos y cobertura, y cotización.
7. **Portal de soporte del cliente** (`/soporte`): abrir y seguir tickets.

**Pruebas:** Vitest + Playwright (escritorio y teléfono 390 px), con fixtures tipados con `packages/contratos`.
