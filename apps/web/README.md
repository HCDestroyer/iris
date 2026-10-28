# @iris/web · panel

Panel web de Iris (Next.js 15, App Router, React 19, Tailwind 4) con la identidad Horus de
`@iris/ui`. Dueño: I3. `app/campo` es de I4. `/soporte` es el portal de soporte del cliente final.

## Qué hace

- **Sesión:** `/entrar` (correo + contraseña, paso MFA, 401/422/429 con «espera N min»,
  `mfa_bloqueado`). `middleware.ts` manda a `/entrar?siguiente=…` si no hay cookie `iris_sesion`;
  el layout del panel lee `GET /api/v1/yo` y un 401 también vuelve al inicio de sesión.
- **Menú por permisos** (`lib/menu.ts`): nombres exactos del enum `Permiso` del OpenAPI. Los
  módulos sin permiso en el contrato (agenda, despacho, comercial) se ven como
  «Próximamente» y un `satisfies` hace fallar `typecheck` cuando el permiso aparezca.
- **Empresa activa** (`lib/empresa.ts`, `componentes/selector-empresa.tsx`): selector en la barra
  superior (varias empresas), nombre (una) o nada (sin conector). La elegida se guarda en
  `localStorage` (`iris-empresa`) y va en `X-Iris-Empresa`; cambiarla relee `/yo` y remonta la
  pantalla. 422 `empresa_requerida` → elegir; 403 `empresa_no_permitida` → se olvida y se vuelve
  a elegir; 403 `sin_empresa` → estado vacío. `/usuarios` y `/administracion/*` no exigen empresa (salvo `/administracion/buzones`).
  Con `hayDatosSinEmpresa`, aviso y `/administracion/datos-sin-empresa` (adopción con confirmación).
- **Tickets** (`/tickets`, `/tickets/[id]`): bandeja (tabla / tarjetas en teléfono) con filtros,
  vistas guardadas en `localStorage`, cursor «Cargar más» y SLA visible (`lib/tickets/sla.ts`);
  detalle con conversación (notas internas solo con `tickets.ver_notas`), historial, ficha de
  Argos (`stale`, `conector_apagado`) y acciones explícitas (diálogos con `react-hook-form` + zod
  espejo de la API, `version` + `Idempotency-Key`, 409 → «Recargar»); alta rápida con búsqueda de
  cliente en Argos o contacto a mano. «Contacto sin verificar» (correo sin DMARC/DKIM) y correos
  retenidos como nota interna; «Ver en Argos/Horus» (`GET /integraciones/enlaces`, `clientes.ver`)
  con `rel="noopener noreferrer"`; incidente maestro y sus tickets. «Crear orden» desde el ticket.
- **Órdenes** (`/ordenes`, `/ordenes/[id]`, `lib/ordenes`, `componentes/ordenes`): tablero en
  lista o columnas por estado con filtros, cursor y marca «Pendiente en Argos»; detalle con
  checklist, galería de fotos y firma (descarga autenticada a `blob:`, visor dentro del panel),
  GPS con enlace a OpenStreetMap, materiales y equipos, historial y pendientes en Argos con
  «Reintentar»; acciones asignar, programar, reprogramar, cancelar y reabrir; alta desde cero o
  desde un ticket.
- **Portal de soporte** (`/soporte`, `app/soporte`, `componentes/portal`, `lib/portal`): otro
  actor, separado del panel (sin menú ni empresa). `/soporte/entrar#<ticket>` lee el ticket del
  fragmento (nunca de la query), lo borra con `history.replaceState` y lo canjea con
  `POST /portal/sesion` (401 → «El enlace venció o ya se usó», 429 → «espera N min», red →
  «Reintentar»). Cliente propio (`lib/portal/cliente.ts`): `X-CSRF-Token` desde
  `iris_portal_csrf`, nunca `X-Iris-Empresa`, y un 401 en `/portal/*` vuelve a la entrada.
  `middleware.ts` no exige `iris_sesion` en `/soporte` y, sin `iris_portal`, manda a
  `/soporte/entrar` («Entra desde el portal de tu proveedor»). Mis casos (estado en lenguaje del
  cliente, «Hay una falla general en tu zona», cursor), reportar un problema (zod espejo,
  `Idempotency-Key`, 429 `demasiados_tickets`), detalle con conversación (tú / soporte / aviso
  automático), responder (429 `demasiados_mensajes`), «Confirmar que quedó resuelto» y 409 →
  «Recargar»; «Salir» (`DELETE /portal/sesion`).
- **Administración** (`/administracion`): índice por permisos y **buzones de correo**
  (`/administracion/buzones`): lista, alta/edición (contraseña de solo escritura que se exige al
  cambiar host/puerto/TLS/usuario, `authservId`), activar/desactivar y «Probar conexión»; 403 por
  alcance explicado.
- **Usuarios** (`/usuarios`): lista con paginación por cursor y **«Reiniciar MFA»**
  (`usuarios.reiniciar_mfa`, solo sobre otro usuario con MFA activo): motivo, contraseña temporal
  (generada en el navegador con `crypto.getRandomValues`, copiar, «entrégala en persona o por
  teléfono; no se vuelve a mostrar») y «Revocar también sus tokens» marcada; `version` +
  `Idempotency-Key`, 409 → «Recargar» (relee con `GET /usuarios?q=`), 404 → «ya no está en tu
  alcance» + recargar la lista, 403 y 422 (`mfa_no_configurado`, `reinicio_mfa_propio`) explicados.
- **Cuenta propia** (`lib/cuenta`, `componentes/cuenta`): menú del usuario en la barra superior
  (cambiar contraseña, configurar la verificación en dos pasos, cerrar sesión).
  `/cambiar-contrasena` (fuera del layout del panel) es la pantalla obligatoria con contraseña
  temporal: se llega desde el login (`usuario.cambioContrasenaRequerido`), desde `GET /yo` o desde
  cualquier 403 `cambio_contrasena_requerido` (el cliente lo avisa al panel); solo deja cambiarla
  (`POST /yo/contrasena`, zod: 12+ runas, distinta y confirmada; 422 por campo) o salir, y luego
  vuelve a `siguiente` (también `/campo`) ofreciendo el MFA. 429 `demasiados_intentos` → «vuelve a
  entrar en N min» e «Ir a iniciar sesión» (la API ya cerró la sesión). `/cuenta/mfa`: QR del
  `otpauth://` (dibujado en el navegador con `uqr`) y la clave en grupos, `POST /yo/mfa` y
  `/yo/mfa/activar`.
- **Cliente HTTP** (`lib/api/cliente.ts`): `openapi-fetch` de `@iris/contratos`, cookies,
  `X-CSRF-Token` (cookie `iris_csrf`) en todo método que cambia estado y `X-Iris-Empresa`; los
  arreglos de la query van separados por coma (`estado=nuevo,abierto`); errores problem+json →
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
