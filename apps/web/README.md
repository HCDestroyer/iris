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
  espejo de la API, `version` + `Idempotency-Key`, 409 → «Recargar»); alta rápida con el selector de
  cliente o contacto a mano. «Contacto sin verificar» (correo sin DMARC/DKIM) y correos
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
- **Comercial** (`/comercial`, `/comercial/[id]`, `lib/comercial`, `componentes/comercial`,
  `prospectos.ver`): embudo por estado (una columna por estado con su cursor y conteo «20+» si hay
  más), lista con cursor y mapa (MapLibre: prospectos con coordenadas por estado y cobertura de
  `GET /comercial/cobertura` con los radios viable y con obra; con `disponible = false`, el motivo y
  ningún círculo), filtros en la URL (estado, vendedor, zona, origen, texto); en el teléfono, lista
  con filtro de estado. Alta rápida. Detalle: datos, pasos del embudo, factibilidad (evaluar; visita
  técnica con `factibilidad.registrar_manual`, que anula una automática «no viable» con motivo),
  cotización (planes de Argos por el router de la factibilidad, o a mano sin conector; totales de la
  API; revisiones; PDF; aceptar), contrato (borrador, firma en `LienzoFirma` → SVG, PDF firmado),
  ganar (qué falta, confirmar obra, router si hace falta) y alta (local u Argos con pendientes,
  «Reintentar» y error visible), actividades con próxima acción e historial. Botones = `acciones`
  de la API ∩ permisos de `GET /yo`; `version` + `Idempotency-Key`; 409 → «Recargar».
- **Reportes** (`/reportes`, `lib/reportes`, `componentes/reportes`, `reportes.ver`): SLA cumplido,
  tiempo de resolución, órdenes por técnico, primera visita resuelta, reincidencias y satisfacción (`GET
  /reportes/*`). Filtros en la URL: período en días de Guatemala (atajos «Este mes», «Mes
  anterior», «Últimos 90 días»; como mucho 366 días, validado antes de pedir), zona, cuadrilla,
  técnico (usuario), y en los de tickets categoría y prioridades (`prioridad=alta,critica`);
  `dias` en primera visita (7–90) y reincidencias (1–90). Indicadores, gráfico de barras en HTML con
  la paleta categórica de Horus (`bg-grafico-1`/`-2`, valor en texto al final, leyenda con dos
  series) y tabla de desglose; porcentaje `null` = «Sin datos», nunca 0 %. Reincidencias con
  «Cargar más». «Descargar CSV» con los mismos filtros (`formato=csv`, con la sesión) y el `detail`
  de un 422 (`exportacion_excedida`, `rango_excedido`). El ticket siguiente fuera del alcance (null)
  se muestra «Fuera de tu zona», sin enlace. 503 `reporte_demasiado_largo` → mensaje, sugerencia de
  acotar y «Reintentar». Si la lectura falla, error con «Reintentar» y ningún total. No emite ni
  consume eventos.
- **Encuestas de satisfacción** (`lib/encuestas`, `componentes/encuestas`):
  - Página pública `/encuesta#<token>` (fuera de la sesión: `middleware.ts` no la redirige): el
    token se lee del fragmento, se borra de la URL y solo va en el cuerpo de
    `POST /encuestas/publica/consultar` y `/responder`, con un cliente sin cookies
    (`credentials: "omit"`), sin `X-CSRF-Token` ni `X-Iris-Empresa`. Calificación 1–5 con texto
    (`GrupoOpciones`, radios nativos) y comentario ≤ 1000 (react-hook-form + zod). Pantallas:
    gracias, ya respondida (409), vencida y anulada (410), enlace inválido (404), demasiados
    intentos (429), error con «Reintentar».
  - Tarjeta «Encuesta» en el detalle del ticket y de la orden (`encuestas.ver`): estado,
    calificación, alerta (≤ 2) y comentario. «Copiar enlace» (`encuestas.compartir`, solo pendiente
    por enlace) pide `POST /encuestas/{id}/enlace`, lo copia y lo muestra solo en el diálogo; por
    correo, «Enviada por correo».
  - Lista `/encuestas` (`encuestas.ver`, en el menú) con cursor y filtros en la URL (fechas,
    calificación, tipo, estado), y el reporte «Satisfacción» en `/reportes`.
- **Autologin desde Argos** (`/sso/entrar#<ticket>`, `lib/sso`, fuera del middleware de sesión):
  el ticket se lee del fragmento y se borra (`lib/fragmento.ts`); `POST /sso/canjear` con cookies
  pero sin cabeceras del panel. Estados: `sesion` (al `destino.ruta`, solo si es
  `/tickets|ordenes|comercial/{id}`; si no, al inicio), `mfa` (código), `confirmar_cambio` («Entrar
  como…» repite con `confirmar: true`; «Quedarme como…»), `vincular` (datos de Argos, login normal
  y confirmación explícita «Vincular» / «No fui yo»; 409 `vinculo_requiere_admin` → «Pide a un
  administrador…»). Errores con texto y enlace al login.
- **Re-autenticación**: el cliente HTTP (`lib/api/cliente.ts` + `lib/api/reautenticacion.ts`) ante
  un 403 `reautenticacion_requerida` abre «Confirma tu contraseña» (`componentes/cuenta/
  dialogo-reautenticar.tsx`, montado en el panel), llama `POST /sesion/reautenticar` y repite la
  acción con el mismo cuerpo y una `Idempotency-Key` nueva (la API guardó el 403 con la anterior).
- **Cuentas vinculadas**: en `/usuarios/[id]` (detalle nuevo, con «Vincular cuenta de Argos» para
  `usuarios.editar`) y en «Mi perfil» (`/cuenta`, en el menú del usuario) con «Quitar».
- **Integraciones** (`/administracion/integraciones`, `integraciones.ver`): conectores; «Activar»
  avisa si reemplaza al activo del mismo sistema; «Conectar» revisa el origen del enlace
  (`conexion/previsualizar`) y solo deja seguir si está permitido.
- **Plantillas de formulario** (`/administracion/plantillas`, `lib/plantillas`, `componentes/plantillas`,
  `plantillas.ver`): lista por tipo y activas o todas; crear (borrador por omisión), editar (ítems
  `si_no`, `texto`, `numero`, `medicion`, `foto`, `seleccion` con opciones; orden con subir/bajar;
  obligatorio; firma) con zod espejo; editar una usada crea versión nueva (historial con cuántos
  formularios usa cada una y la anterior en solo lectura); activar y desactivar (422
  `plantilla_por_defecto` con el `detail`). Las comunes solo con `empresas.todas` (o sin conector).
- **Formularios y pausa en la orden**: formularios con origen y versión, «Agregar formulario» y
  «Quitar» (`ordenes.asignar_formulario`; nunca el del tipo ni uno con respuestas); pausar con
  motivo y nota y reanudar (`ordenes.pausar`/`reanudar`), tiempo pausado e historial de pausas. La
  pausada se reprograma o cancela (no se reasigna); está en el tablero, el filtro de activas, la
  agenda y el despacho, y moverla en el calendario es reprogramarla.
- **Agenda** (`/agenda`, `agenda.ver`): calendario de **mes** (por defecto; cuadrícula en escritorio,
  lista por día en el teléfono) con cuántas órdenes tiene cada día y en qué estado (icono + texto +
  color), técnicos ausentes y **conflictos** (cita viva dentro de una ausencia o cruzada con otra
  del mismo técnico; `lib/agenda/mes.ts`). `GET /agenda` en trozos de ≤ 31 días; si uno falla, falla
  el mes. Tocar un día abre la vista **Día** (por técnico y franja, con «Sin programar»: «Programar…»
  trae el día puesto). Vistas mes/semana/día, día y filtros (técnico, cuadrilla, zona) en la URL
  (`?vista=&dia=&tecnico=&cuadrilla=&zona=`).
- **Técnicos** (`/tecnicos`, `/tecnicos/[id]`; menú con los permisos de fichas `agenda.crear_tecnico`,
  `editar_tecnico`, `activar/desactivar_tecnico`, `fijar_turnos`, `registrar/anular_ausencia`):
  lista con búsqueda, cuadrilla y estado; alta (usuario de `GET /tecnicos/candidatos`, cuadrilla
  obligatoria —define sus zonas—, teléfono, habilidades, bodega), edición, activar/desactivar con
  confirmación; ficha con turnos semanales (sin cruces, hasta 24:00, atajo L–V 08–17) y ausencias
  (registrar «todo el día» o con hora; lista las citas que quedan dentro; anular).
  **Cuadrillas** y **Zonas** en `/administracion/cuadrillas` y `/administracion/zonas`.
- **Formularios en diálogo** (`componentes/cierre-seguro.tsx`): las selecciones obligatorias dejan
  el botón deshabilitado con el mensaje junto al campo, y cerrar con cambios sin guardar (Escape,
  clic fuera, «Volver») pide confirmar «¿Descartar los cambios?».
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
- **Selector de cliente** (`componentes/clientes/selector-cliente.tsx`, `lib/clientes/selector.ts`):
  el único del panel (alta rápida de ticket y nueva orden). Busca mientras se escribe en Argos
  (`GET /clientes?q=&servicios=true`: nombre, código, teléfono, NIT/CUI, dirección) o por IP
  (`resolver-ip` + ficha); cada resultado con código, dirección, teléfono, empresa y servicios con
  su estado; recientes por usuario y empresa en `localStorage`; teclado (↑ ↓ Inicio Fin Enter
  Esc); vacío con mensaje, error con «Reintentar», `stale` y conector apagado; en el teléfono,
  hoja a pantalla completa. Con varios servicios pide cuál (o «sin servicio específico») y manda
  `servicioId`.
- **Formularios en modales**: `DialogoProtegido` (confirma antes de descartar cambios),
  `BotonEnviar` (deshabilitado con «Falta elegir…» mientras falte una selección obligatoria),
  `DialogoConfirmar` en las acciones destructivas (desactivar buzón o plantilla) y `errors[]` del
  `problem+json` en su campo (`lib/formularios.ts`) o listados en el aviso. Ver `packages/ui`.
- **Comercial sin scrolls propios**: el embudo pasa columnas a la fila siguiente (sin scroll
  horizontal), cada columna muestra 5 tarjetas y «Ver N más» (sin scroll interno), y las líneas
  de la cotización son tarjetas en el teléfono. `e2e/comercial-scrolls.spec.ts` lo verifica en
  390 y 1280 px, claro y oscuro.
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
