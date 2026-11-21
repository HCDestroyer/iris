# Propuesta de Iris para el contrato 0.9.0 con ArgosISP

Autor: arquitecto de integración de Iris (solo lectura del repositorio, `origin/main` = `0cb70c0`, 21 nov 2026).
Para: líder de Iris, que la lleva al líder de Argos y al PM.
Base: contrato `argosisp.md` 0.8.1 (19 nov 2026). Último pedido A = **A29**; último pedido B = **B7**. Esta propuesta usa **A30–A35** y **B8–B13**.

Lleva dos bloques independientes, que pueden salir juntos o por separado:

- **Bloque P · Portal del cliente de Argos:** tickets y solicitudes de servicio desde `apps/portal` de Argos (§1 a §4 de esta propuesta y §11 del texto para el contrato).
- **Bloque E · Equipos del cliente y activación desde la orden:** el técnico pide desde la orden de Iris que Argos aprovisione la ONT o el router (SmartOLT / TR-069), con estado del equipo y cambio de Wi-Fi (§5 de esta propuesta y §12 del texto para el contrato).

Decisiones del PM que ya vienen dadas (llegaron por Argos y se aplican aquí; no están en la lista abierta):

1. «Solicitar servicio» desde el portal de Argos entra a Iris como **seguimiento comercial**: un asesor de ventas lo revisa, cotiza y lo convierte en orden. Alta nueva, cambio de plan, traslado y «otro» van a ventas.
2. El **cambio de contraseña Wi-Fi que hace el cliente desde su portal** lo resuelve Argos (SmartOLT/TR-069). Queda **fuera del contrato**.
3. El **control de los equipos del cliente** (SmartOLT para ONU de fibra, TR-069/ACS para routers) es de Argos. **Iris nunca guarda credenciales de SmartOLT ni del ACS** y todo lo pide a Argos por API.
4. El **técnico, desde la orden de trabajo en Iris**, solicita el alta y la activación del servicio, y Argos automatiza la IP (IPAM), el aprovisionamiento de la ONT por la serie o MAC que el técnico escanea, el Wi-Fi inicial y la activación.

---

## 1. Inventario: lo que ya existe en Iris y lo que falta

### 1.1 Ya existe

| Qué | Dónde (archivo:línea en `origin/main`) | Detalle |
| --- | --- | --- |
| **Portal `/soporte` de Iris con autologin desde Argos** | `apps/api/internal/nucleo/identidad/portal.go:21-41`, `:115`; ruta `rutas_portal.go:32` | `POST /api/v1/portal/tickets-acceso { clienteId, empresaId, nombre? }` con el `iris_pat_` de Argos y el permiso `portal.emitir` → `{ url: <IRIS_BASE_URL>/soporte/entrar#<ticket>, expiraEn }` (60 s, un solo uso). El canje (`POST /api/v1/portal/sesion`) abre una sesión de **actor cliente** (cookie `iris_portal`, 2 h, CSRF propio). Valida que el cliente sea de la empresa (instantánea o lectura a Argos). **No está en el contrato**: el código lo llama «pedido A9» y `i1.md:591` lo propuso como «A10», pero en el contrato A9 y A10 ya son otra cosa. |
| Casos del portal (actor cliente) | `apps/api/internal/soporte/portal.go:149-390`; rutas `soporte/rutas_portal.go:37-84` | `GET /portal/tickets` (cursor, `estado`), `GET /portal/tickets/{id}`, `GET /portal/tickets/{id}/mensajes`, `POST /portal/tickets` (asunto, descripción, `categoriaId?`, `servicioId?` del cliente, origen `portal`, prioridad media, `Idempotency-Key`), `POST /portal/tickets/{id}/responder` (reanuda o reabre), `POST /portal/tickets/{id}/confirmar-solucion`, `GET /portal/categorias` (`visible_portal`). |
| Vista reducida para el cliente | `soporte/portal.go:27-44` (`TicketPortal`), `:66-90` (`MensajePortal`) | Sin asignación, SLA, prioridad, notas internas, historial ni datos de Horus. Mensajes con autor `cliente | agente | sistema`, sin ids de usuario. Ya trae `afectadoPorIncidente` y `puedeConfirmar`. |
| Notas internas nunca al portal | `soporte/listar_conversacion.go:19-22` | Requieren `tickets.ver_notas` y nunca por el canal `portal`. |
| Topes anti spam por cliente | `soporte/portal.go:437-470`; `soporte/config_correo.go:65-77` | `IRIS_PORTAL_TICKETS_ABIERTOS` (10), `IRIS_PORTAL_TICKETS_POR_HORA` (20), `IRIS_PORTAL_MENSAJES_POR_MINUTO` (10), contados en la base por `cliente:<empresa>:<cliente>` → 429. |
| Evento `ticket.mensaje_cliente` | `soporte/portal.go:349`; `contratos/eventos.md:483` | `ticket_id`, `mensaje_id`, `canal` (`portal`). |
| Eventos `ticket.estado_cambiado` y `ticket.respondido` | `soporte/comun.go:280`; `soporte/responder_ticket.go:75`; `soporte/avisos_hijos.go:109` | Payload mínimo (`ticket_id`, `de`, `a`, `motivo` / `ticket_id`, `mensaje_id`). **No van a Argos.** `ticket.respondido` sale también, uno por hijo, al resolver un incidente maestro (puede ser masivo). |
| Rutas de tickets para integraciones | `soporte/rutas.go:76-104`; permisos del rol en `nucleo/permisos.go:763-766` | `integracion_argos` tiene `tickets.ver`, `tickets.crear`, `ordenes.ver`, `portal.emitir`, `sso.emitir`, `mensajeria.enviar`, `mensajeria.registrar_consentimiento`, `mensajeria.ver`. `POST /tickets` acepta `clienteId`, `servicioId` y `origen: argos` (solo integración o sistema, `soporte/crear_ticket.go:30,41-49`). `GET /tickets` filtra por `clienteId` (`soporte/listar_tickets.go:22`). |
| Webhooks Iris → Argos | `apps/api/internal/mensajeria/webhooks.go:25-66` | Mecanismo completo (outbox → `mensajeria.webhooks_salida` → firma HMAC, reintentos 1 min…12 h, solo conector activo con contrato ≥ 0.6.0 y receptor). **Solo** `aviso.entregado`, `aviso.fallido`, `contacto.baja` (`:44`). |
| Prospectos | `apps/api/internal/comercial/prospectos.go:22,43,197-210`; migración `20261028230550_comercial_embudo.sql:25,49` | Origen `panel | web | referido | argos | otro` (`argos` solo integración o sistema). Ya existen las columnas `cliente_argos_id` / `servicio_argos_id`, pero hoy las llena **solo el alta al ganar**. No hay campo `tipo` de solicitud. |
| Ganar → alta en Argos y orden de instalación | `comercial/ganar.go:243,316-379` | Siempre `POST /clientes` + `POST /servicios` (`pendiente_instalacion`) por `integracion.pendientes`, y después la orden de instalación. **Siempre crea un cliente nuevo.** |
| Adjuntos del núcleo | `apps/api/internal/nucleo/adjuntos/modulo.go:76-90`, `rutas.go:54-190` | Subida reanudable (tus 1.0) por entidad registrada. Entidades registradas: `orden`, `traslado`, chat, conversación, `cotizacion`, `contrato`. **`ticket` no está registrada**: un ticket hoy no acepta adjuntos (la columna `adjuntos` de los mensajes existe, pero nadie la llena). |
| Equipos en la orden | `campo/modelo.go:180-190` (`EquipoOrden`: `unidadId`, `serie`, `mac`, `modelo`, `condicion`, `validado`); `campo/cerrar_orden.go:215-237` | El técnico registra `equipo.instalar` / `equipo.retirar` por la cola sin conexión. |
| Activación del servicio al cerrar | `integracion/pendientes/cierre_orden.go:43,310-318` | Al cerrar una instalación exitosa: `POST /servicios/:id/activar-instalacion` (`iris:instalacion:<orden_id>`) y `POST /servicios/:id/equipos` por equipo (`iris:comodato:…`). **Nunca antes del cierre** y sin `planta` (A9 no se manda hoy). |
| Versión del contrato que informa Iris | `integracion/argos/contrato.go:34` | `ContratoIris = "0.8.1"`. |

### 1.2 Falta

| # | Falta | Bloque |
| --- | --- | --- |
| F1 | Rutas para que Argos actúe **por un cliente concreto** con la vista reducida del portal. Las rutas generales (`GET /tickets/{id}`) devuelven el ticket completo (asignado, SLA, prioridad e incluso `incidente` con IP y enlace a Horus, `soporte/modelo.go:75-77`): sirven para la ficha del panel de Argos (A7), **no** para el cliente. | P |
| F2 | Adjuntos en tickets (registrar la entidad `ticket`, subida desde Argos y descarga para el cliente). | P |
| F3 | Webhooks de tickets hacia Argos (`ticket.estado_cambiado`, `ticket.respondido`) y de solicitudes. | P |
| F4 | Solicitudes de servicio: prospecto con `tipo` y cliente de Argos ya conocido; ganar sin crear otro cliente. | P |
| F5 | Origen `portal_argos` en tickets y prospectos, y canal `portal_argos` en `ticket.mensaje_cliente`. | P |
| F6 | Escribir en el contrato lo que ya existe y no está (`portal.emitir`, `/soporte`) y corregir A7. | P |
| F7 | Aprovisionamiento desde la orden, estado del equipo y Wi-Fi desde Iris (todo nuevo en los dos lados). | E |

### 1.3 Hallazgo que hay que corregir ya (no espera a la 0.9.0)

**A7 del contrato dice `GET /api/v1/tickets?cliente_id=`, pero Iris filtra por `clienteId`** (`soporte/listar_tickets.go:22`). Un parámetro desconocido se ignora: si Argos llama con `cliente_id`, **recibe todos los tickets de la empresa**, no solo los del cliente. En la ficha del panel de Argos es un dato de más; en un portal sería una fuga entre clientes. Propuesta: el contrato dice `clienteId` y, además, Iris acepta `cliente_id` como alias (o responde `422` a un parámetro desconocido en esa ruta). Pedido **B11**.

---

## 2. API directa o autologin al portal `/soporte` de Iris

### 2.1 Recomendación

**API directa** como camino principal (lo que propone Argos), **construida sobre la misma lógica del portal de Iris**, y el autologin a `/soporte` **se conserva** como respaldo y se escribe en el contrato (hoy existe sin estar acordado).

### 2.2 Por qué

| Criterio | API directa (Argos llama a Iris) | Autologin a `/soporte` de Iris |
| --- | --- | --- |
| Experiencia del cliente | Un solo portal, la marca y la sesión de Argos; los avisos salen por el `notify` de Argos. | Salta a otro sitio con otra cabecera; la sesión de Iris dura 2 h y es aparte; al vencer, «vuelve a entrar desde Argos». |
| Lo que hay que construir | Rutas nuevas finas en Iris (reusan `crearTicket`, `reactivarPorCliente`, `tomar`, `cerrar`, `TicketPortal`, `MensajePortal` y los topes) y las pantallas en Argos. | Casi nada (ya está), pero el cliente vive en dos portales. |
| Seguridad | El servidor de Argos ya identifica al cliente; Iris confía en el token de Argos **acotado** (abajo). La cookie del cliente nunca toca Iris. | Igual de confiado (Argos ya dice quién es el cliente al emitir el acceso), más superficie pública en Iris (canje, cookies). |
| Adjuntos | `multipart/form-data` servidor a servidor, como los comprobantes de §7.2 (Argos ya lo hace). | Subida directa del navegador a Iris (tus); falta construirla también. |
| Idempotencia | Claves de Argos por formulario (`argos:portal:…`). | La del portal de Iris. |
| Multiempresa | `X-Iris-Empresa` por petición, como el resto de la integración. | La empresa queda fija en la sesión. |

El autologin sigue sirviendo para: instalaciones donde Argos aún no habla 0.9.0, pruebas (`iris portal acceso`) y como plan B si las pantallas de Argos fallan. No se promociona.

### 2.3 Cómo se protege que el token de Argos actúe «por un cliente»

Argos pide una cabecera `X-Argos-Cliente` firmada. **No la recomiendo como control principal:** el `iris_pat_` y cualquier secreto de firma viven en el **mismo servidor de Argos**; quien robe uno roba el otro, y una firma no agrega nada frente a un Argos comprometido. Lo que sí protege:

1. **El cliente va en la ruta**, no en una cabecera: `/api/v1/integraciones/argosisp/portal/clientes/{cliente_id}/…`. Cada caso filtra por `cliente_id` **y** la empresa de `X-Iris-Empresa`; un ticket de otro cliente o de otra empresa responde **404** (igual que el portal de Iris: no se puede sondear qué ids existen).
2. **Permisos propios** (`portal_argos.*`, §11.3) que solo tiene el rol `integracion_argos`. Además, el caso exige que el actor sea **el usuario de integración de un conector Argos** (403 `solo_integracion_argos` para cualquier otro, aunque sea admin). Un token de Argos sin estos permisos (anterior a la migración) recibe 403.
3. **Cliente verificado contra la empresa** con la misma regla de `portal.emitir`: instantánea de Argos o, si Iris no lo conoce, lectura a Argos (`GET /clientes/{id}`); de otra empresa → 422 `empresa_no_coincide`; inexistente → 422 `cliente_no_existe`. Si Argos no responde, se acepta (Argos es quien llama) y se audita `cliente_validado: false`.
4. **Cabecera `X-Argos-Portal-Sesion`** obligatoria: `^(documento|ip):[A-Za-z0-9_-]{8,64}$`, el método con el que Argos identificó al cliente y un identificador **opaco** de su sesión del portal (un hash; nunca el documento, la IP ni la cookie). Sirve para auditar, para los topes por sesión y para limitar lo que se puede hacer con una sesión identificada solo por IP (decisión del PM, §6, punto P2).
5. **Topes**: los del portal de Iris, **compartidos** entre los dos portales (misma clave `cliente:<empresa>:<cliente>`), más un tope global por conector (600 peticiones por minuto, configurable) con `429` y `Retry-After`.
6. **Auditoría** «integración en nombre del cliente X»: `actor_tipo = integracion`, `actor_id` = el usuario de integración, `actor_nombre` = «Argos (portal) · cliente 1201», canal `integracion`, y en `entrada.auditoria`: `{ en_nombre_de: { cliente_id, empresa_id, metodo: documento|ip, sesion } }`. Se puede buscar por cliente. Los mensajes del cliente quedan con `autor_tipo = cliente` (no «Argos»).
7. **Nunca sale al cliente:** notas internas, asignado, cuadrilla, SLA, prioridad, historial, motivo de cierre o de fusión, datos de Horus (IP, `url_horus`, hallazgos), ids de usuarios de Iris, tickets de otros clientes (también cuando un ticket se fusionó con el de otro cliente: el cliente solo ve «cerrado»), y nada de otra empresa.

Si el PM o Argos quieren igualmente una firma por petición (defensa extra si el token se filtra por un log), se puede agregar después sin romper nada (decisión P3, §6).

---

## 3. «Solicitar servicio»: cómo se modela en Iris

### 3.1 Recomendación

**Toda solicitud es un prospecto** (una oportunidad del embudo comercial) con dos datos nuevos: `tipo` (`alta`, `cambio_plan`, `traslado`, `otro`) y el **cliente de Argos ya conocido** (`cliente_argos_id`, y `servicio_argos_id` del servicio afectado en cambio de plan y traslado). Origen nuevo `portal_argos`. No es un ticket.

### 3.2 Por qué prospecto y no ticket

- El PM decidió que lo revisa **ventas**, que **cotiza** y que lo **convierte en orden**. Eso es exactamente el embudo de `comercial`: vendedor, actividades, factibilidad, cotización con PDF, contrato con firma, «ganar» → orden (`comercial/README.md`). Un ticket no tiene cotización, ni contrato, ni ganar; su SLA y su bandeja son de soporte.
- El alcance por zona y por vendedor de los prospectos ya existe (un vendedor ve solo lo suyo).
- Los reportes de ventas (embudo, ganados, perdidos por motivo) cuentan las solicitudes del portal sin trabajo extra.

### 3.3 Qué cambia en el prospecto

| Dato | Hoy | Con la 0.9.0 |
| --- | --- | --- |
| `tipo` | no existe | `alta` (por defecto, también para los existentes), `cambio_plan`, `traslado`, `otro` |
| `origen` | `panel`, `web`, `referido`, `argos`, `otro` | + `portal_argos` (solo la integración de Argos) |
| `cliente_argos_id` | lo llena el alta al ganar | también al crear, cuando viene de Argos (cliente ya conocido) |
| `servicio_afectado_id` (nuevo) | — | el servicio de Argos que cambia (`cambio_plan`, `traslado`) |
| Nombre, teléfono, correo | se escriben | se copian de la instantánea de Argos (el cliente no los teclea) |
| `direccion` (obligatoria) | se escribe | `alta` y `traslado`: la nueva dirección que da el cliente; `cambio_plan`: la del servicio |

### 3.4 Qué hace «ganar» según el tipo

| Tipo | Requisitos para ganar | Escritura en Argos | Orden |
| --- | --- | --- | --- |
| `alta` (servicio nuevo para un cliente que ya existe) | Como hoy: factibilidad viable, cotización aceptada, contrato firmado | **Solo** `POST /servicios` (`iris:servicio:<prospecto_id>`) con el `cliente_id` conocido; **nunca** `POST /clientes`. `alta.estado` empieza en `esperando_servicio` | Instalación, como hoy |
| `cambio_plan` | Cotización aceptada (con el plan nuevo). Factibilidad: no (misma dirección). Contrato: lo decide el PM (P5) | **Ninguna en 0.9.0**: Argos no tiene una ruta para que Iris cambie el plan. Queda una **tarea «Hacer en Argos»** visible para el vendedor con «Abrir en Argos» (§1.3). Pedido A34 (sin forma) para hacerlo por API más adelante | Opcional: `cambio_equipo` si el plan nuevo pide otro equipo (casilla al ganar) |
| `traslado` | Factibilidad viable en la dirección nueva, cotización aceptada (cargo de traslado), contrato según P5 | **Ninguna en 0.9.0** (igual que arriba; A34) | `visita` con el formulario «Traslado» (retira en la dirección vieja e instala en la nueva) |
| `otro` | — | Ninguna | Ninguna automática: el vendedor decide (o lo marca perdido) |

### 3.5 Lo que ve el cliente

El cliente nunca ve los estados internos ni el motivo de pérdida (puede decir «precio» o «competencia»). Ve `estado_cliente`:

| Estado del prospecto | `estado_cliente` |
| --- | --- |
| `nuevo` | `recibida` |
| `contactado`, `factibilidad` | `en_revision` |
| `cotizado`, `contrato` | `cotizada` |
| `ganado` con alta, tarea o orden abiertas | `en_proceso` |
| `ganado` con todo hecho (alta completa / orden cerrada / tarea hecha) | `completada` |
| `perdido` | `no_procede` (con un texto fijo de Iris, nunca el motivo) |

Una sola solicitud abierta por cliente, tipo y servicio: repetirla responde `409 solicitud_abierta` con el id de la existente (para que el portal diga «ya la recibimos»).

---

## 4. Webhooks Iris → Argos para avisar al cliente

Se reutiliza el mecanismo de §6.6 (mismo receptor, firma, reintentos y `X-Webhook-Id`), sumando tres eventos y **solo a conectores con contrato ≥ 0.9.0**. Payloads armados para el cliente: sin textos libres de agentes, sin el cuerpo del mensaje (Argos lo lee con la API si lo quiere mostrar), sin motivo.

- `ticket.estado_cambiado` y `ticket.respondido`: **solo tickets con `cliente_id`** dentro del alcance del conector, de cualquier origen (también los que abrió soporte por teléfono: el cliente los ve en su portal). Nunca los de un incidente maestro sin cliente.
- `ticket.respondido` lleva `masivo: true` cuando es el aviso a los hijos de una falla general, y `aviso_por_correo: true` cuando Iris ya le mandó ese mensaje por correo (ticket de origen `correo`): Argos no repite el correo.
- `ticket.mensaje_cliente` con canal `portal_argos` **no** vuelve a Argos (sería su propio eco).
- `solicitud_servicio.estado_cambiado` para las solicitudes que nacieron en el portal de Argos.

---

## 5. Equipos del cliente y activación desde la orden (bloque E)

### 5.1 Principio

Argos es el único que habla con SmartOLT y con el ACS (TR-069). Iris **nunca** guarda credenciales de SmartOLT ni del ACS, ni llama a esos sistemas; pide a Argos por API y recibe el resultado. El detalle de SmartOLT/TR-069 (perfiles, VLAN por plan y router/OLT, IPAM) lo diseña Argos. Esta propuesta define **cómo lo pide el técnico, qué ve en la orden y cómo encaja con la cola sin conexión, el cierre y los pendientes**.

### 5.2 Flujo del técnico

1. En la orden de instalación, ya en sitio, el técnico **escanea la serie de la ONT** (o la MAC del router TR-069) con la cámara; elige caja/puerto PON si la plantilla lo pide; opcionalmente escribe el SSID y la clave Wi-Fi que el cliente quiere.
2. Pulsa **«Activar servicio»**. La app guarda la operación en la cola (`orden.activar_servicio`) y la manda en cuanto hay señal. Sin señal, la orden muestra «Activación en cola: se enviará al volver la señal».
3. Iris la acepta, la deja en `integracion.pendientes` (operación `aprovisionar`) y llama a Argos. Argos responde `202 en_proceso`.
4. La orden muestra los pasos que informa Argos («IP asignada», «ONT autorizada», «Wi-Fi configurado», «Servicio activo») y, al final, **«Servicio activo · señal −19.8 dBm · en línea»** o **«No se pudo activar: la ONT no aparece en la OLT. Revisa la fibra y pulsa Reintentar»**.
5. Con la activación `lista`, el técnico termina el checklist (fotos obligatorias, firma) y cierra. El cierre ya **no** vuelve a pedir `activar-instalacion` ni el comodato de ese equipo.

Si Argos no tiene aprovisionamiento para esa empresa (sin SmartOLT/ACS, contrato < 0.9.0 o conector apagado), el botón no aparece y todo sigue como hoy: la activación sale al cerrar (§3 del contrato).

### 5.3 Qué hace Iris

| Pieza | Propuesta |
| --- | --- |
| Operación de la cola | `orden.activar_servicio` `{ equipo: { tipo: ont | router_tr069, serie?, mac?, unidadId?, modelo? }, planta?: { olt?, slot?, pon?, caja?, puerto? }, wifi?: { ssid, clave } }`. Idempotente por `id` de la operación. Solo en una orden `instalacion` propia, en `en_sitio`, no pausada, con servicio de Argos. |
| Rechazos (reglas para `contratos/sync.md` §3) | `argos_sin_aprovisionamiento` (422, definitivo: cierra como hoy); `orden_pausada`, `orden_reasignada`, `orden_cancelada` (como el resto); `aprovisionamiento_en_curso` (409 → `aplicada` sin cambios, devuelve el que sigue); `aprovisionamiento_listo` (409 → `aplicada` sin cambios); `equipo_invalido` (422: ni serie ni MAC, o formato). Un `fallido` de Argos **no** es un rechazo de la operación: la operación queda aplicada y el estado de la activación dice `fallido` con la causa. «Reintentar» manda **otra** operación (otro `id`), que genera otro intento. |
| Estado en el teléfono | `GET /campo/paquete` y `GET /campo/cambios` suman `aprovisionamientos[]` `{ ordenId, intento, estado: en_cola | enviando | en_proceso | listo | fallido, pasos[], causa?, detalle?, equipo: { serie?, mac? }, senal?, actualizadoEn }` (como `recepciones[]` de los traslados). Nunca trae la clave Wi-Fi. |
| Tabla | `campo.aprovisionamientos` (orden, intento, pendiente, estado, causa, `aprovisionamiento_id` de Argos, equipo, pasos, fechas). Una activa por orden (índice único parcial). |
| Pendiente | Operación nueva `aprovisionar` en `integracion.pendientes`, `Idempotency-Key = iris:aprovisionar:<orden_id>:<intento>`. El `202` cuenta como **hecho** del pendiente (el pedido llegó); el resultado lo trae el webhook. Sin webhook en 10 min, Iris consulta `GET /aprovisionamientos/:id` (y después cada 10 min hasta 2 h; luego avisa al supervisor). |
| Clave Wi-Fi | Viaja solo al pendiente, **cifrada** con `IRIS_CLAVE_CIFRADO`. En auditoría, eventos, logs, respuestas y `aprovisionamientos[]`: `***`. Al recibir el `2xx` de Argos, Iris borra la clave del payload guardado. En el teléfono se borra de la cola al aplicarse la operación. |
| Cierre de la orden | Una instalación `exitosa` con una activación pedida exige `listo` (si no: `rechazada` con `activacion_pendiente`, 422, y el teléfono reintenta al llegar `listo`, igual que con `faltan_adjuntos`). Los adjuntos obligatorios siguen siendo los de la plantilla (por ejemplo, foto de la etiqueta de la ONT y de la potencia); la activación no los reemplaza. Con `fallido`, el técnico reintenta, cierra como `parcial` o falla la orden. Excepción del supervisor: P10. |
| `orden.cerrada` | `equipos[]` suma `aprovisionado` (bool) y la orden suma `aprovisionamiento_id` (o null). El plan de pendientes (`cierre_orden.go:310-318`) **omite** `activar_instalacion` y el `asignar_comodato` del equipo aprovisionado cuando hay una activación `lista` (Argos ya activó el servicio y asignó ese equipo). El resto (otros equipos, consumos, cargo) sigue igual. Si aun así llega `409 servicio_no_pendiente`, se concilia como hecho cuando la activación está `lista`. |
| Permisos (Iris) | `ordenes.activar_servicio` (técnico en sus órdenes, supervisor, admin); `servicios.ver_equipo` (técnico en sus órdenes, soporte, supervisor, admin); `servicios.cambiar_wifi` (técnico en sus órdenes, soporte, supervisor, admin). El asistente de IA **no** los tiene (P12). |
| Eventos (Iris) | `orden.activacion_solicitada` (orden_id, intento, equipo_tipo, origen: campo | panel) y `orden.activacion_resuelta` (orden_id, intento, estado: listo | fallido, causa o null); `servicio.wifi_solicitado` / `servicio.wifi_resuelto` (servicio_id, origen: orden | ticket, entidad_id, estado, causa). **Nunca** la clave, el SSID ni la IP. |
| Panel | En la orden: el mismo bloque «Activación» con los pasos, «Reintentar» (supervisor y despacho) y «Estado del equipo». En el ticket de un cliente: «Estado del equipo» y «Cambiar Wi-Fi» (soporte). La IP solo con `servicios.ver_red` (B6). |
| Ver el equipo | `GET /api/v1/servicios/{id}/equipo` en Iris, que llama a Argos al momento (plazo 10 s, sin guardar más que 60 s en caché). Sin señal en el teléfono: «Sin conexión: no se puede leer el equipo». |

### 5.4 Facturación

Hoy la facturación arranca cuando Iris pide `activar-instalacion` al cerrar. Con este flujo, Argos activa el servicio al terminar el aprovisionamiento (el técnico aún está en sitio). Es decisión del PM (P9): **recomiendo que empiece al quedar activo** (el cliente ya tiene internet), que es lo que Argos hace naturalmente.

---

## 6. Decisiones para el PM (lenguaje llano, con mi recomendación)

**Portal del cliente**

- **P1. ¿El cliente usa un solo portal?** Recomiendo que sí: el portal de Argos muestra los tickets y las solicitudes; el de Iris queda solo como respaldo.
- **P2. Cliente que entró solo por su IP (sin contraseña).** Recomiendo: puede abrir un ticket y ver la lista con su estado, pero no leer las respuestas, no responder y no pedir servicios. Para eso, entrar con documento y contraseña. (Cualquiera conectado a ese internet, por ejemplo un vecino con la clave Wi-Fi, quedaría identificado como el titular.) **Decidido por el PM (21 nov): sí, aprobada la recomendación. Además, para pedir o hacer un cambio de contraseña (WiFi) desde el portal, el cliente vuelve a confirmar su contraseña del portal o su identidad aunque ya tenga sesión (lado de Argos).**
- **P3. ¿Firma extra por cada petición de Argos?** Recomiendo que no por ahora: no protege más de lo que ya protege el token, y se puede agregar después.
- **P4. Fotos en los tickets.** Recomiendo hasta 3 archivos por mensaje (fotos o PDF, 10 MB cada uno) desde el primer día.
- **P5. ¿Cambio de plan y traslado necesitan contrato firmado?** Recomiendo: el traslado sí (nueva dirección); el cambio de plan solo con la cotización aceptada, y cada empresa puede exigir contrato si quiere.
- **P6. Cambio de plan y traslado en Argos.** En esta versión los hace a mano alguien en Argos (Iris le deja la tarea con «Abrir en Argos»). Recomiendo pedirle a Argos que lo automatice en la siguiente versión (A34).
- **P7. ¿El cliente ve la cotización en su portal?** Recomiendo que no en esta versión: el vendedor se la manda y la explica. Se puede sumar después.
- **P8. ¿Avisar al cliente cuando le responden?** Recomiendo que avise Argos (correo, Telegram o WhatsApp por Iris) y que, en una falla general, mande un solo aviso por cliente.

**Equipos y activación**

- **P9. ¿Cuándo empieza a cobrarse una instalación?** Recomiendo: cuando el servicio queda activo con la activación del técnico, no al cerrar la orden. **Decidido por el PM (21 nov): el cobro de la instalación lo registra el agente (persona) o sale de un formulario de la orden, y se guarda en Argos como factura única (cargo de una sola vez), con su seguimiento; el cobro mensual sigue empezando con el servicio activo.**
- **P10. ¿Se puede cerrar una instalación sin la activación lista?** Recomiendo que no; solo un supervisor puede cerrarla «con activación pendiente», dejando el motivo. **Decidido por el PM (21 nov): configurable. La activación la puede hacer el técnico desde la orden o un agente desde el panel de Iris; la orden puede cerrarse sin activación cuando la hará un agente. Aplica a FTTH (ONT por SmartOLT) y a wireless (CPE/radio, TR-069 si lo soporta).**
- **P11. ¿Exigir señal de la ONT antes de cerrar?** Recomiendo que cada empresa lo elija en la plantilla (por ejemplo, «en línea y mejor que −25 dBm»); apagado de fábrica.
- **P12. ¿El asistente de IA puede cambiar el Wi-Fi o ver el equipo?** Recomiendo que no por ahora: solo personas (técnico en su orden, soporte). El cliente lo hace desde el portal de Argos. **Decidido por el PM (21 nov): sí, el asistente de IA puede consultar el equipo y cambiar el Wi-Fi, solo si la empresa lo habilita y con roles y permisos propios (configurable), con verificación de identidad del cliente antes de cambiar el Wi-Fi.**
- **P13. ¿El técnico puede escribir el Wi-Fi del cliente?** Recomiendo que sí, opcional, y que la clave nunca quede guardada en Iris después de enviarla.

---

## 7. Texto propuesto para `argosisp.md` 0.9.0 (listo para pegar)

> Indicaciones de pegado: (a) reemplaza la línea de versión; (b) agrega el párrafo de «Gobierno»; (c) agrega la entrada al inicio del historial de §4 y la sección §4.3; (d) agrega las filas a §1.1, §1.2, §2.3, §3, §5.1, §5.2 y §6.6; (e) agrega las secciones nuevas §11 y §12 al final (antes de nada se renumera); (f) en §5.1, A7 cambia `cliente_id` por `clienteId`; (g) §8 «Cambios en el equipo del cliente (Wi-Fi)» se reemplaza por la remisión a §12.

### 7.1 Línea de versión

```markdown
Versión: 0.9.0 (borrador, 21 nov 2026) · Iris ↔ ArgosISP
```

### 7.2 Gobierno (párrafo nuevo, después del de la 0.8.1)

```markdown
- **0.9.0 (borrador de Iris, 21 nov 2026; pedido del PM por el líder de Argos): aditiva sobre la 0.8.1.** Dos bloques que pueden publicarse por separado, cada uno con su propia marca en `capacidades` (§4.3): **P**, el portal del cliente de Argos con tickets y solicitudes de servicio de Iris (§11); **E**, equipos del cliente y activación desde la orden de Iris, con Argos como único dueño de SmartOLT y del ACS (§12). Decisiones del PM ya tomadas: una solicitud de servicio la atiende ventas en Iris (prospecto) y la convierte en orden; el cambio de Wi-Fi que hace el cliente desde su portal es solo de Argos y queda fuera del contrato; Iris nunca guarda credenciales de SmartOLT ni del ACS; el técnico pide la activación desde la orden y Argos la automatiza.
```

### 7.3 §1.1 (agregar al final de la lista de permisos)

```markdown
- **0.9.0, bloque E (§12):** `servicios.aprovisionar`, `servicios.ver_equipo` y `servicios.cambiar_wifi`. Son permisos nuevos: Argos migra los tokens que ya existen (como en 0.7.0) y solo los usa con un Iris que informó contrato ≥ 0.9.0. El token nunca recibe credenciales de SmartOLT ni del ACS.
```

### 7.4 §1.2 (reemplazar el primer párrafo y agregar)

```markdown
Token `iris_pat_<32B base62>` con `tickets.ver`, `tickets.crear`, `ordenes.ver`, `sso.emitir` y `portal.emitir`. Argos lo usa para mostrar los tickets y las órdenes de un cliente en la ficha de su panel (A7: `GET /api/v1/tickets?clienteId=`; desde 0.9.0 Iris acepta también `cliente_id` como alias) y para el acceso al portal de soporte de Iris (§11.9).

**0.9.0:** el mismo token suma `portal_argos.ver_tickets`, `portal_argos.crear_ticket`, `portal_argos.responder_ticket`, `portal_argos.confirmar_solucion`, `portal_argos.solicitar_servicio` y `portal_argos.ver_solicitudes` para el portal del cliente de Argos (§11). Son permisos del rol `integracion_argos`: ningún rol de persona de Iris los recibe, y aunque un admin los tenga, las rutas de §11 exigen al usuario de integración de un conector Argos (`403 solo_integracion_argos`). Iris los agrega al rol al actualizarse; no hay que volver a conectar.
```

### 7.5 §4, entrada nueva al inicio del historial

```markdown
- **0.9.0** (borrador, 21 nov 2026; pedido del PM por el líder de Argos): aditiva sobre 0.8.1, en dos bloques con su propia marca en `capacidades` (§4.3). **P · Portal del cliente de Argos (§11):** rutas de Iris `/api/v1/integraciones/argosisp/portal/clientes/{cliente_id}/…` para listar, ver, abrir, responder y confirmar tickets del cliente con la vista reducida del portal (sin notas internas, asignación, SLA, prioridad, historial ni datos de Horus), con adjuntos por `multipart/form-data`, y para crear y seguir **solicitudes de servicio** (`alta`, `cambio_plan`, `traslado`, `otro`) que en Iris son prospectos de ventas con origen `portal_argos`; cabeceras `X-Iris-Empresa`, `X-Argos-Portal-Sesion` e `Idempotency-Key = argos:portal:<operacion>:<id>`; permisos `portal_argos.*` en el token de §1.2; webhooks Iris → Argos `ticket.estado_cambiado`, `ticket.respondido` y `solicitud_servicio.estado_cambiado` (§6.6); `portal.emitir` y el portal `/soporte` de Iris quedan escritos (§11.9); A7 usa `clienteId`. **E · Equipos y activación desde la orden (§12):** `POST /servicios/:id/aprovisionar` asíncrono con la serie de la ONT o la MAC del router TR-069, `GET /aprovisionamientos/:id`, `GET /servicios/:id/equipo`, `POST /servicios/:id/wifi`, webhooks Argos → Iris `instalacion.aprovisionada`, `instalacion.fallida` y `equipo.wifi_resuelto`, y permisos `servicios.aprovisionar`, `servicios.ver_equipo` y `servicios.cambiar_wifi` en el token de §1.1. Pedidos A30–A35 y B8–B13. Fuera del contrato: el cambio de Wi-Fi que el cliente hace desde el portal de Argos.
```

### 7.6 §4.3 Compatibilidad de la 0.9.0 (sección nueva)

```markdown
### 4.3 Compatibilidad de la 0.9.0

La 0.9.0 es aditiva sobre la 0.8.1. Ningún campo ni ruta de la 0.8.1 cambia de forma ni de significado. Cada lado usa una función de la 0.9.0 **solo si el otro informó un contrato ≥ 0.9.0** (canje, `PUT /integraciones/iris/contrato` y `GET /integraciones/iris/estado`, como en §3.3) **y**, para cada bloque, la marca de `capacidades`:

- `GET /integraciones/iris/estado` (Argos) y la respuesta del canje suman `capacidades: { aprovisionamiento: [{ empresa_id, ont: boolean, tr069: boolean }] }`. Sin la marca, o con `false` para esa empresa y ese tipo de equipo, Iris no ofrece «Activar servicio» y activa al cerrar como en §3.
- `GET /api/v1/integraciones/argosisp/estado` (Iris, ya existe) suma `capacidades: { portal_argos: boolean, solicitudes_servicio: boolean, adjuntos_ticket: boolean }`. Argos muestra cada sección de su portal solo con su marca en `true`.

| Función 0.9.0 | Iris 0.9.0 con un Argos < 0.9.0 | Argos 0.9.0 con un Iris < 0.9.0 |
| --- | --- | --- |
| Rutas `…/portal/clientes/{cliente_id}/…` (§11) | Existen; nadie las llama | No existen (404). Argos no muestra tickets ni solicitudes en su portal; si Iris tiene `portal.emitir`, puede mostrar el botón «Soporte» hacia `/soporte` (§11.9) |
| Webhooks `ticket.*` y `solicitud_servicio.*` (§6.6) | Iris no los envía (Argos los recibiría como desconocidos) | No llegan; el portal de Argos relee al abrir la pantalla |
| `orden.activar_servicio` (§12) | Iris no lo ofrece: la activación sale al cerrar (§3) | Nadie llama a `aprovisionar`; Argos sigue recibiendo `activar-instalacion` al cierre |
| `GET /servicios/:id/equipo`, `POST /servicios/:id/wifi` | Iris no muestra el equipo ni ofrece cambiar el Wi-Fi | Nadie los llama |
| Webhooks `instalacion.*`, `equipo.wifi_resuelto` | No llegan; Iris no los espera | Argos no los envía a conectores con contrato < 0.9.0 |
| Alias `cliente_id` en `GET /tickets` | No aplica | Iris anterior lo ignora y devuelve toda la empresa: Argos manda **siempre** `clienteId` |

**Errores nuevos de la 0.9.0.** Todos `application/problem+json` (RFC 9457) con `type` `urn:iris:problema:<codigo>` (rutas de Iris) o `urn:argosisp:problema:<codigo>` (rutas de Argos). Los de negocio no se reintentan; solo `5xx`, timeouts, `429` (con `Retry-After`) y el `409` de idempotencia en curso (§3.1).

| Dónde | Status | Código | Cuándo |
| --- | --- | --- | --- |
| §11, todas | 403 | `solo_integracion_argos` | La credencial no es el usuario de integración de un conector Argos |
| §11, todas | 403 | `empresa_no_permitida` | `X-Iris-Empresa` fuera del alcance del token |
| §11, todas | 400 | `cabecera_requerida` | Falta o tiene mal formato `X-Iris-Empresa` o `X-Argos-Portal-Sesion` |
| §11, todas | 422 | `empresa_no_coincide` / `cliente_no_existe` | El cliente es de otra empresa / Argos no lo tiene |
| §11, ticket o solicitud | 404 | `no_encontrado` | No existe, es de otro cliente o de otra empresa (mismo `detail` en los tres casos) |
| §11, escrituras con sesión `ip` | 403 | `sesion_insuficiente` | La acción exige una sesión con `documento` (§11.2) |
| §11.4 | 422 | `validacion` | Campos (`errors[]` con `field` y `rule`); `servicio_id` que no es del cliente: `rule: "servicio"`; categoría no visible en el portal: `rule: "categoria"` |
| §11.4, §11.5 | 422 | `transicion_invalida` | Responder un ticket cerrado; confirmar uno que no está resuelto |
| §11.5 | 409 | `conflicto_version` | `version` vieja |
| §11.4, §11.5 | 429 | `demasiados_tickets` / `demasiados_mensajes` | Topes por cliente (§11.2) |
| §11, todas | 429 | `demasiadas_peticiones` | Tope del conector (§11.2) |
| §11.7 | 429 | `demasiadas_solicitudes` | Tope de solicitudes por cliente (§11.2) |
| §11, todas | 409 | `argos_apagado` | El conector Argos está apagado en Iris |
| §11.6 | 413 / 422 | `tamano_excedido` / `mime_no_permitido` / `demasiados_adjuntos` | Adjuntos fuera de los límites |
| §11.7 | 409 | `solicitud_abierta` | Ya hay una solicitud abierta de ese tipo y servicio (`detail` y `extensions.solicitud_id`) |
| §11.7 | 422 | `servicio_requerido` | `cambio_plan` o `traslado` sin `servicio_id` |
| Idempotencia (Iris) | 422 / 409 | `idempotencia_reutilizada` / `idempotencia_en_proceso` | Misma clave con otro cuerpo / la primera sigue en curso (se reintenta) |
| §12.2 | 202 | — | Aprovisionamiento aceptado (`en_proceso`) |
| §12.2 | 409 | `servicio_no_pendiente` | El servicio ya no está `pendiente_instalacion` |
| §12.2 | 409 | `aprovisionamiento_en_curso` | Hay otro en curso para el servicio (`extensions.aprovisionamiento_id`) |
| §12.2 | 422 | `equipo_invalido` | Ni serie ni MAC, formato, o el tipo no coincide con la tecnología del servicio |
| §12.2 | 422 | `aprovisionamiento_no_disponible` | La empresa no tiene SmartOLT o ACS configurado |
| §12.2 | 409 | `unidad_en_otra_bodega` | El equipo escaneado está en inventario, pero no en la bodega del técnico |
| §12.4 | 422 | `wifi_invalido` | SSID de 1 a 32 caracteres, clave de 8 a 63 (WPA2), sin caracteres de control |
| §12.3, §12.4 | 409 | `equipo_sin_gestion` | El servicio no tiene un equipo gestionado por SmartOLT o el ACS |
| §12.3 | 503 | `equipo_sin_respuesta` | SmartOLT o el ACS no respondieron a tiempo (se puede reintentar a mano) |
```

### 7.7 §5.1 Iris solicita a ArgosISP (filas nuevas, y corrección de A7)

```markdown
| A7 | (corrección 0.9.0) El filtro es `clienteId` (`GET /api/v1/tickets?clienteId=`). Con `cliente_id` un Iris anterior a 0.9.0 lo ignora y devuelve todos los tickets de la empresa. | solicitado |
| A30 | **Portal del cliente con tickets de Iris** (0.9.0, §11): pantallas «Mis tickets», «Nuevo ticket» (categorías de `GET …/categorias`, servicio del cliente, hasta 3 adjuntos), detalle con mensajes públicos, responder y «Confirmar solución»; `X-Argos-Portal-Sesion` en cada llamada; `Idempotency-Key` por envío de formulario; con sesión `ip`, solo lo que permite §11.2. Recibir `ticket.estado_cambiado` y `ticket.respondido` (§6.6) para avisar por `notify` (sin repetir el correo cuando `aviso_por_correo = true`, y un solo aviso por cliente cuando `masivo = true`). Usar las rutas solo con Iris en contrato ≥ 0.9.0 y `capacidades.portal_argos`. | solicitado |
| A31 | **Solicitar servicio desde el portal** (0.9.0, §11.7): formulario con tipo (`alta`, `cambio_plan`, `traslado`, `otro`), servicio afectado, dirección y coordenadas nuevas (alta y traslado), plan de interés y comentario; lista «Mis solicitudes» con `estado_cliente`; recibir `solicitud_servicio.estado_cambiado` para avisar. | solicitado |
| A32 | **Aprovisionar desde la orden de Iris** (0.9.0, §12.2): `POST /servicios/:id/aprovisionar` asíncrono (IPAM, autorización de la ONT en SmartOLT por serie o del router por TR-069 con el perfil y la VLAN del plan y del router/OLT, Wi-Fi inicial y activación con el inicio de facturación), `GET /aprovisionamientos/:id`, webhooks `instalacion.aprovisionada` / `instalacion.fallida` y `capacidades.aprovisionamiento` por empresa. Permiso `servicios.aprovisionar` con migración de tokens. | solicitado |
| A33 | **Estado del equipo** (0.9.0, §12.3): `GET /servicios/:id/equipo` con en línea, señal óptica, firmware y último contacto. Permiso `servicios.ver_equipo`. | solicitado |
| A34 | **Cambio de plan y traslado por API** (sin forma; para después de 0.9.0): rutas para que Iris aplique en Argos un cambio de plan o de dirección ganado en ventas (§11.7). Mientras no existan, lo hace una persona en Argos. | propuesto |
| A35 | **Cambiar el Wi-Fi pedido desde Iris** (0.9.0, §12.4): `POST /servicios/:id/wifi` asíncrono y webhook `equipo.wifi_resuelto`. Permiso `servicios.cambiar_wifi`. La clave nunca se guarda en claro ni sale en auditoría, eventos ni logs. | solicitado |
```

### 7.8 §5.2 ArgosISP solicita a Iris (filas nuevas)

```markdown
| B8 | **Rutas del portal del cliente** (0.9.0, §11.3–§11.6): tickets del cliente con la vista reducida del portal de Iris, adjuntos por `multipart/form-data`, permisos `portal_argos.*`, auditoría «en nombre del cliente», topes compartidos con `/soporte` y `capacidades.portal_argos`. | propuesto por Iris |
| B9 | **Webhooks de tickets y solicitudes a Argos** (0.9.0, §6.6): `ticket.estado_cambiado`, `ticket.respondido` y `solicitud_servicio.estado_cambiado`, solo a conectores con contrato ≥ 0.9.0. | propuesto por Iris |
| B10 | **Solicitudes de servicio como prospectos** (0.9.0, §11.7): `tipo`, cliente de Argos conocido, origen `portal_argos`, `estado_cliente`, una abierta por cliente, tipo y servicio, y «ganar» sin crear otro cliente en Argos. | propuesto por Iris |
| B11 | **Escribir lo que ya existe y corregir A7** (0.9.0, §11.9 y §1.2): `portal.emitir` y el portal `/soporte` de Iris; alias `cliente_id` en `GET /tickets`. | propuesto por Iris |
| B12 | **Activación desde la orden, lado Iris** (0.9.0, §12): operación sin conexión `orden.activar_servicio`, estado en la orden, cierre que exige la activación lista, `orden.cerrada` con `aprovisionado` y sin repetir la activación ni el comodato de ese equipo, estado del equipo y cambio de Wi-Fi en la orden y en el ticket. | propuesto por Iris |
| B13 | **Origen `portal_argos` en tickets** y canal `portal_argos` en `ticket.mensaje_cliente` (0.9.0, §11.4). | propuesto por Iris |
```

### 7.9 §2.3 Webhooks Argos → Iris (filas nuevas)

```markdown
| `instalacion.aprovisionada` / `instalacion.fallida` (0.9.0, §12.2): aprovisionamiento_id, servicio_id, empresa_id, cliente_id, origen_tipo (`iris_orden`), origen_id, idempotency_key; aprovisionada: servicio_estado, equipo `{ tipo, serie, mac, unidad_id }`, senal `{ rx_dbm, tx_dbm }` (o null), activado_en; fallida: causa (§12.2), reintentable, detalle (texto sin secretos). Nunca la clave Wi-Fi ni credenciales. Solo a conectores con contrato ≥ 0.9.0 | Actualiza la activación de la orden; con `aprovisionada`, deja la orden lista para cerrar. |
| `equipo.wifi_resuelto` (0.9.0, §12.4): solicitud_id, servicio_id, empresa_id, origen_tipo (`iris_orden` \| `iris_ticket`), origen_id, idempotency_key, estado (`aplicado` \| `fallido`), causa (o null). Nunca el SSID ni la clave | Muestra el resultado en la orden o el ticket. |
```

### 7.10 §3 (fila nueva en la tabla y nota)

```markdown
| Técnico pulsa «Activar servicio» en una instalación (0.9.0, §12) | `POST /servicios/:id/aprovisionar` | `iris:aprovisionar:<orden_id>:<intento>` |

**0.9.0:** si la orden tiene una activación `lista` (§12), el cierre **no** manda `activar-instalacion` ni el `POST /servicios/:id/equipos` del equipo aprovisionado: Argos ya activó el servicio y asignó ese equipo en comodato. Un `409 servicio_no_pendiente` de una orden con activación `lista` es conciliado, no error.
```

### 7.11 §6.6 Webhooks Iris → Argos (párrafo y filas nuevas)

```markdown
**0.9.0:** suma tres eventos, **solo a conectores con contrato ≥ 0.9.0** (§4.3). Solo de tickets con `cliente_id` dentro del alcance del conector, de cualquier origen. Nunca llevan el cuerpo de un mensaje, notas internas, un motivo escrito por una persona, ni datos de otro cliente; Argos lee lo que muestra con §11.

| Evento | Payload | Efecto en Argos |
| --- | --- | --- |
| `ticket.estado_cambiado` | ticket_id, numero, empresa_id, cliente_id, servicio_id (o null), origen, de, a, puede_confirmar (`a = resuelto`) | Actualiza el portal y, si corresponde, avisa al cliente (`resuelto`: «revisa y confirma»; `cerrado`) |
| `ticket.respondido` | ticket_id, numero, empresa_id, cliente_id, mensaje_id, autor_tipo (`agente` \| `sistema`), masivo (true en el aviso de una falla general), aviso_por_correo (true si Iris ya lo mandó por correo al contacto del ticket) | Avisa «tienes una respuesta en tu ticket TK-…»; con `aviso_por_correo` no manda correo; con `masivo`, un solo aviso por cliente |
| `solicitud_servicio.estado_cambiado` | solicitud_id, numero, empresa_id, cliente_id, tipo, servicio_id (o null), de, a (`estado_cliente` de §11.7) | Actualiza «Mis solicitudes» y avisa al cliente |

Un mensaje que el cliente escribió desde el portal de Argos (`ticket.mensaje_cliente` con canal `portal_argos`) no vuelve como webhook. Un ticket fusionado con el de **otro** cliente solo llega como `ticket.estado_cambiado` a `cerrado`, sin el destino.
```

### 7.12 §8 (reemplazar el primer punto)

```markdown
- **Cambios en el equipo del cliente:** pasan a §12 (0.9.0). El cambio de Wi-Fi que el cliente hace desde el portal de Argos es solo de Argos y no forma parte de este contrato.
```

### 7.13 §11 Portal del cliente de Argos (0.9.0, bloque P) — sección nueva

````markdown
## 11. Portal del cliente de Argos (0.9.0, bloque P)

El cliente abre tickets de soporte, los sigue y pide servicios **desde el portal de Argos**, sin salir de él. Tickets y seguimiento comercial son de Iris: Argos llama a Iris servidor a servidor con el token de §1.2, **en nombre de un cliente que su portal ya identificó**. Iris nunca recibe la contraseña, el documento, la cookie ni la IP del cliente.

### 11.1 Principios

| Regla | Detalle |
| --- | --- |
| Quién identifica | Argos. Iris confía en el token de §1.2 y en el `cliente_id` de la ruta, y lo comprueba contra la empresa (§11.2). |
| Lo que ve el cliente | Lo mismo que el portal de soporte de Iris: el ticket reducido y sus mensajes públicos. **Nunca** notas internas, asignado, cuadrilla, prioridad, SLA, historial, motivos escritos por el personal, datos de Horus (IP, hallazgos, enlaces), ids de usuarios de Iris, ni nada de otro cliente o de otra empresa. |
| Un solo cliente por ruta | El cliente va en la ruta. Un ticket o una solicitud de otro cliente o de otra empresa responde `404 no_encontrado`, igual que uno que no existe. |
| Auditoría | Iris audita con actor `integracion` (el usuario de integración del conector), canal `integracion`, nombre «Argos (portal) · cliente `<id>`» y `en_nombre_de { cliente_id, empresa_id, metodo, sesion }`. Lo que escribe el cliente queda como mensaje **del cliente**. |
| Formato | Cuerpos y respuestas en `snake_case` (como §6). Listas `{ data, nextCursor }` con `?cursor=&limit=` (`limit` de 1 a 100, por defecto 20). Fechas RFC 3339 en UTC. |

### 11.2 Cabeceras, alcance y límites

- `Authorization: Bearer iris_pat_…` (§1.2).
- `X-Iris-Empresa: <empresa_id>`: **obligatoria**, la empresa en que se atiende (la del servicio). Fuera del alcance del token: `403 empresa_no_permitida`. Un cliente con servicios en varias empresas se consulta empresa por empresa.
- `X-Argos-Portal-Sesion: <metodo>:<id>`: **obligatoria**. `^(documento|ip):[A-Za-z0-9_-]{8,64}$`. `metodo` es cómo identificó Argos al cliente; `id` es un identificador opaco y estable de la sesión del portal (por ejemplo, un hash de su id). Nunca el documento, la IP ni la cookie. Iris lo guarda en la auditoría y lo usa en los topes.
- **Sesión `ip`** (el cliente entró por su IP, sin contraseña): solo `GET …/tickets` (lista, sin mensajes), `GET …/categorias` y `POST …/tickets`. El resto responde `403 sesion_insuficiente` (decisión del PM, P2).
- `Idempotency-Key`: **obligatoria** en todo `POST` (`400 idempotency_key_requerida`). Forma: `argos:portal:<operacion>:<id>`, con `<id>` un UUID que el portal genera **por envío de formulario** y repite en cada reintento: `argos:portal:ticket:<uuid>`, `argos:portal:respuesta:<uuid>`, `argos:portal:confirmar:<ticket_id>:<version>`, `argos:portal:solicitud:<uuid>`. La guarda Iris por credencial y cliente. Misma clave y mismo cuerpo: la respuesta original (mismo status y cuerpo). Misma clave con otro cuerpo: `422 idempotencia_reutilizada`. Primera aún en curso: `409 idempotencia_en_proceso` (se reintenta con la misma clave y los mismos bytes, §3.1).
- **Cliente:** si Iris lo conoce (instantánea), debe ser de la empresa (`422 empresa_no_coincide`). Si no lo conoce, lo lee en Argos (`GET /clientes/:id`): inexistente `422 cliente_no_existe`, de otra empresa `422 empresa_no_coincide`. Si Argos no responde, Iris lo acepta y lo audita con `cliente_validado: false`.
- **Topes** (los mismos del portal de soporte de Iris y **compartidos** con él, por cliente y empresa): tickets abiertos desde un portal (10) y creados en la última hora (20) → `429 demasiados_tickets`; mensajes del cliente en el último minuto (10) → `429 demasiados_mensajes`; solicitudes de servicio creadas en el último día (5) → `429 demasiadas_solicitudes`. Tope del conector: 600 peticiones por minuto → `429 demasiadas_peticiones`. Todos con `Retry-After`. Los valores son configurables en Iris; una petición repetida con la misma `Idempotency-Key` no cuenta.
- Con el conector Argos apagado en Iris: `409 argos_apagado`.

### 11.3 Rutas

Todas bajo `https://<iris>/api/v1/integraciones/argosisp/portal/clientes/{cliente_id}` (`cliente_id` = id del cliente en Argos, `^[1-9]\d{0,17}$`).

| Método y ruta | Permiso | Qué hace |
| --- | --- | --- |
| `GET /categorias` | `portal_argos.ver_tickets` | Categorías que el cliente puede elegir (activas y visibles en el portal, globales y de la empresa): `{ data: [{ id, codigo, nombre }] }` |
| `GET /tickets?estado=&cursor=&limit=` | `portal_argos.ver_tickets` | Sus tickets en la empresa, de cualquier origen, más recientes primero. `estado` admite varios separados por coma |
| `GET /tickets/{ticket_id}` | `portal_argos.ver_tickets` | Un ticket |
| `GET /tickets/{ticket_id}/mensajes?cursor=&limit=` | `portal_argos.ver_tickets` | Mensajes públicos, más antiguos primero |
| `GET /tickets/{ticket_id}/adjuntos/{adjunto_id}` | `portal_argos.ver_tickets` | Descarga un adjunto de un mensaje público del ticket (§11.6) |
| `POST /tickets` | `portal_argos.crear_ticket` | Abre un ticket (§11.4) |
| `POST /tickets/{ticket_id}/responder` | `portal_argos.responder_ticket` | Mensaje del cliente (§11.5) |
| `POST /tickets/{ticket_id}/confirmar-solucion` | `portal_argos.confirmar_solucion` | El cliente confirma que quedó resuelto (§11.5) |
| `GET /solicitudes?estado=&cursor=&limit=` | `portal_argos.ver_solicitudes` | Sus solicitudes de servicio (§11.7) |
| `GET /solicitudes/{solicitud_id}` | `portal_argos.ver_solicitudes` | Una solicitud |
| `POST /solicitudes` | `portal_argos.solicitar_servicio` | Pide un servicio (§11.7) |

**Ticket** (lo que devuelven las rutas de tickets):

```json
{
  "id": 8812,
  "numero": "TK-008812",
  "asunto": "Sin internet desde la mañana",
  "estado": "abierto",
  "categoria": { "id": 3, "codigo": "sin_servicio", "nombre": "Sin servicio" },
  "servicio_id": 4410,
  "origen": "portal_argos",
  "afectado_por_incidente": false,
  "puede_confirmar": false,
  "resuelto_en": null,
  "cerrado_en": null,
  "version": 4,
  "creado_en": "2026-11-21T14:03:11Z",
  "actualizado_en": "2026-11-21T15:20:40Z"
}
```

- `estado`: `nuevo`, `abierto`, `en_espera`, `resuelto`, `cerrado`. `categoria` puede ser `null`. `origen`: `panel`, `portal`, `portal_argos`, `correo`, `telegram`, `horus`, `argos`, `api` (informativo: el portal puede mostrar «abierto por soporte»).
- `afectado_por_incidente`: el ticket está ligado a una falla general de la red que sigue abierta (el portal puede decir «estamos trabajando en una falla en tu zona»).
- `puede_confirmar`: está `resuelto`; el cliente puede confirmar (lo cierra) o responder (lo reabre).

**Mensaje:**

```json
{
  "id": 51230,
  "autor_tipo": "agente",
  "autor_nombre": "Ana López",
  "cuerpo": "Ya revisamos tu equipo, ¿puedes reiniciarlo?",
  "adjuntos": [{ "id": "0193a7c2-…", "nombre": "foto.jpg", "mime": "image/jpeg", "tamano": 183220 }],
  "creado_en": "2026-11-21T15:20:40Z"
}
```

- `autor_tipo`: `cliente` (el propio cliente, por cualquier portal o por correo), `agente` (soporte) o `sistema` (aviso automático; `autor_nombre` = «Soporte»).

### 11.4 Abrir un ticket

`POST …/tickets` con `Idempotency-Key = argos:portal:ticket:<uuid>`. `application/json`, o `multipart/form-data` con la parte `datos` (el mismo JSON) y hasta 3 partes `archivos` (§11.6).

```json
{
  "asunto": "Sin internet desde la mañana",
  "descripcion": "La luz LOS del módem está en rojo.",
  "categoria_id": 3,
  "servicio_id": 4410
}
```

- `asunto` de 3 a 200 caracteres; `descripcion` de 1 a 20 000 (primer mensaje público del cliente); `categoria_id` opcional (una de `GET …/categorias`; otra: `422 validacion`, `rule: "categoria"`); `servicio_id` opcional (debe ser del cliente según la instantánea; si no, `422 validacion`, `rule: "servicio"`).
- Iris crea el ticket con origen `portal_argos`, prioridad media, contacto (nombre y teléfono) y zona de la ficha de Argos, y lo liga a una falla general abierta como cualquier otro ticket. Emite `ticket.creado`.
- `201` con el ticket (§11.3). Un reintento con la misma clave: el mismo ticket (`201`).

### 11.5 Responder y confirmar

- `POST …/tickets/{ticket_id}/responder` con `Idempotency-Key = argos:portal:respuesta:<uuid>`; JSON o `multipart/form-data` (§11.6): `{ "cuerpo": "…", "version": 4 }` (`cuerpo` de 1 a 20 000; `version` opcional: si viene y cambió, `409 conflicto_version`). Un ticket `en_espera` se reanuda y uno `resuelto` se reabre (como cuando el cliente responde por correo); `cerrado` responde `422 transicion_invalida`. Responde `200 { ticket, mensaje }`. Emite `ticket.mensaje_cliente` con canal `portal_argos` y, si cambió el estado, `ticket.estado_cambiado`.
- `POST …/tickets/{ticket_id}/confirmar-solucion` con `Idempotency-Key = argos:portal:confirmar:<ticket_id>:<version>`: `{ "version": 5 }` (obligatoria). Solo `resuelto` → `cerrado`; si no, `422 transicion_invalida`. Responde `200` con el ticket.

### 11.6 Adjuntos

- **Subir:** solo dentro de `POST …/tickets` y `POST …/responder` en `multipart/form-data`. Hasta **3** archivos por mensaje, **10 MB** cada uno; tipos `image/jpeg`, `image/png`, `image/webp`, `image/heic` y `application/pdf`, comprobados por el contenido (no por el nombre). Fuera de eso: `413 tamano_excedido`, `422 mime_no_permitido` o `422 demasiados_adjuntos`, y no se crea nada. Iris quita los metadatos EXIF de las fotos. Igual que los comprobantes de §7.2: Argos sube los bytes y nadie descarga URLs del otro (sin SSRF).
- La idempotencia incluye los archivos: un reintento manda exactamente los mismos bytes.
- **Descargar:** `GET …/tickets/{ticket_id}/adjuntos/{adjunto_id}` devuelve el archivo (`Content-Type` real, `Content-Disposition: attachment`) solo si es de un mensaje **público** de ese ticket del cliente; si no, `404 no_encontrado`. Argos lo sirve al cliente desde su propio servidor.

### 11.7 Solicitudes de servicio

En Iris una solicitud es un **prospecto** de ventas (seguimiento comercial; decisión del PM): un asesor la revisa, cotiza y la convierte en orden. Origen `portal_argos`; el cliente de Argos ya es conocido, así que «ganar» nunca crea otro cliente en Argos.

`POST …/solicitudes` con `Idempotency-Key = argos:portal:solicitud:<uuid>`:

```json
{
  "tipo": "traslado",
  "servicio_id": 4410,
  "direccion": "5a avenida 10-20, zona 1, Mixco",
  "coordenadas": "14.6349,-90.5069",
  "plan_interes_id": 12,
  "comentario": "Me mudo el 1 de diciembre",
  "telefono_contacto": "+502 5512-3344"
}
```

| Campo | Regla |
| --- | --- |
| `tipo` | `alta` (servicio nuevo), `cambio_plan`, `traslado` u `otro` |
| `servicio_id` | Obligatorio en `cambio_plan` y `traslado` (`422 servicio_requerido`); debe ser del cliente (`422 validacion`, `rule: "servicio"`). Opcional en `otro`; no va en `alta` |
| `direccion` | Obligatoria en `alta` y `traslado` (1 a 300). En `cambio_plan` se toma la del servicio |
| `coordenadas` | Opcional, `"lat,lng"` como en §3.2 |
| `plan_interes_id` | Opcional, un plan de Argos (`GET /planes`) |
| `comentario` | Opcional, hasta 2000 |
| `telefono_contacto` | Opcional, con la regla de teléfonos de §3.2; si falta, el de la ficha |

- Responde `201` con la solicitud. Una abierta del mismo tipo y servicio (`alta`: del mismo tipo) responde `409 solicitud_abierta` con `extensions.solicitud_id`.
- Iris la crea con nombre, correo y teléfono de la ficha de Argos y la deja sin vendedor para que la tome ventas (o la asigna por zona si la empresa lo configuró). Emite `prospecto.creado` (origen `portal_argos`).

**Solicitud** (lo que devuelven las rutas):

```json
{
  "id": 731,
  "numero": "PR-000731",
  "tipo": "traslado",
  "servicio_id": 4410,
  "estado_cliente": "en_revision",
  "comentario": "Me mudo el 1 de diciembre",
  "creado_en": "2026-11-21T16:02:00Z",
  "actualizado_en": "2026-11-21T17:40:12Z"
}
```

`estado_cliente`: `recibida`, `en_revision`, `cotizada`, `en_proceso`, `completada` o `no_procede`. Nunca el vendedor, las actividades, la factibilidad, la cotización ni el motivo de pérdida.

**Qué hace Iris al ganar** (informativo para Argos):

| `tipo` | En Argos | Orden en Iris |
| --- | --- | --- |
| `alta` | Solo `POST /servicios` (§3.2) con el `cliente_id` conocido, `pendiente_instalacion` | Instalación |
| `cambio_plan` | Nada por API en 0.9.0: una persona lo aplica en Argos (Iris deja la tarea con «Abrir en Argos»; A34 después) | `cambio_equipo` si hace falta |
| `traslado` | Igual que `cambio_plan` | Visita de traslado |
| `otro` | Nada | La decide el asesor |

### 11.8 Lo que Iris nunca hace por esta vía

- Mostrar o aceptar datos de otro cliente, de otra empresa o internos (§11.1).
- Crear clientes ni servicios en Argos fuera de «ganar» (§3.2).
- Cambiar el Wi-Fi del cliente: eso es del portal de Argos y queda fuera del contrato (§12 cubre solo los pedidos del personal de Iris).

### 11.9 Portal de soporte de Iris (`/soporte`, ya existe)

Queda como respaldo (y para Argos < 0.9.0): con el permiso `portal.emitir` del token de §1.2, `POST https://<iris>/api/v1/portal/tickets-acceso { clienteId, empresaId, nombre? }` devuelve `201 { url: "https://<iris>/soporte/entrar#<ticket>", expiraEn }`, válida 60 s y de un solo uso, a la que Argos redirige al cliente. El ticket va en el fragmento y nunca en la consulta ni en logs. Errores: `403 empresa_no_permitida`, `422 empresa_no_coincide`, `422 cliente_no_existe`. La sesión del cliente en Iris dura 2 h. Los topes de §11.2 son los mismos para los dos portales.
````

### 7.14 §12 Equipos del cliente y activación desde la orden (0.9.0, bloque E) — sección nueva

````markdown
## 12. Equipos del cliente y activación desde la orden (0.9.0, bloque E)

### 12.1 Principios

| Regla | Detalle |
| --- | --- |
| Dueño de los equipos | Argos es el único que habla con SmartOLT (ONU/ONT de fibra) y con el ACS (TR-069, routers). **Iris nunca guarda credenciales de SmartOLT ni del ACS** y nunca los llama: todo lo pide a Argos por las rutas de esta sección. El detalle de SmartOLT/TR-069 (perfiles, VLAN por plan y por router/OLT, IPAM) es de Argos. |
| Quién lo pide | El técnico desde la orden de instalación de Iris (app de campo, también sin señal) o el personal desde el panel de Iris. El asistente de IA de Iris no usa esta sección. El cliente cambia su Wi-Fi desde el portal de Argos, fuera de este contrato. |
| Asíncrono | Aprovisionar y cambiar el Wi-Fi responden `202` y el resultado llega por webhook; Iris consulta si el webhook no llega. |
| Secretos | La clave Wi-Fi viaja solo en el cuerpo de la petición. Nunca en webhooks, respuestas, auditoría, eventos ni logs (en los dos lados queda `***`). Iris la guarda cifrada solo hasta el `2xx` de Argos. |
| Idempotencia | §3.1. Claves `iris:aprovisionar:<orden_id>:<intento>`, `iris:wifi:orden:<orden_id>:<uuid>` e `iris:wifi:ticket:<ticket_id>:<uuid>`. |
| Capacidad | Argos informa por empresa si puede aprovisionar ONT y TR-069 (`capacidades.aprovisionamiento`, §4.3). Sin ella, Iris activa al cerrar como en §3. |

### 12.2 Aprovisionar y activar (A32)

`POST /api/v1/servicios/:id/aprovisionar` (`servicios.aprovisionar`), `Idempotency-Key` obligatoria. Solo servicios `pendiente_instalacion` (A1).

```json
{
  "equipo": { "tipo": "ont", "serie": "ZTEGC8A1B2C3", "mac": null, "unidad_id": null, "modelo": "F670L" },
  "planta": { "olt": "OLT-Centro", "slot": 1, "pon": 7, "caja": "CJ-118", "puerto": 4 },
  "wifi": { "ssid": "Familia Pérez", "clave": "********" },
  "tecnico": { "id_externo": "57" },
  "origen_tipo": "iris_orden",
  "origen_id": "9123"
}
```

- `equipo.tipo`: `ont` (con `serie`) o `router_tr069` (con `mac`, o `serie` si el ACS lo identifica así). `unidad_id` opcional si Iris ya lo conoce del inventario. Si la serie o la MAC son de una unidad del inventario de Argos, debe estar en la bodega del técnico (`409 unidad_en_otra_bodega`); Argos la asigna en comodato al servicio en la misma operación.
- `planta` opcional (A9): si viene, Argos la guarda y la puede usar para ubicar la ONT.
- `wifi` opcional: SSID de 1 a 32 caracteres, clave de 8 a 63 (`422 wifi_invalido`). Sin `wifi`, Argos aplica su configuración por defecto.
- `tecnico` como en §3.3.
- **Respuesta** `202 { aprovisionamiento_id, estado: "en_proceso" }`. Si ya terminó (reintento con la misma clave), `200` con el estado final.
- **Errores** inmediatos: `409 servicio_no_pendiente`, `409 aprovisionamiento_en_curso` (con `extensions.aprovisionamiento_id`), `409 unidad_en_otra_bodega`, `422 equipo_invalido`, `422 wifi_invalido`, `422 aprovisionamiento_no_disponible`.

`GET /api/v1/aprovisionamientos/:id` (`servicios.aprovisionar`) → `200 { aprovisionamiento_id, servicio_id, empresa_id, origen_tipo, origen_id, estado, pasos[], causa, reintentable, detalle, servicio_estado, equipo, senal, creado_en, actualizado_en }`.

- `estado`: `en_proceso`, `listo` o `fallido`.
- `pasos[]`: `{ paso: ip | equipo | wifi | activacion, estado: pendiente | hecho | fallido, en }`. Iris los muestra al técnico.
- `causa` (con `fallido`): `equipo_no_encontrado` (la ONT no aparece en la OLT o el router no contactó al ACS), `equipo_en_uso` (la serie ya está en otro servicio), `sin_puerto`, `perfil_sin_mapear` (el plan o el router/OLT no tienen perfil), `sin_ip_disponible`, `olt_sin_respuesta`, `acs_sin_respuesta`, `servicio_no_pendiente`, `otro`. `reintentable` dice si tiene sentido reintentar sin cambiar nada; `detalle` es texto para la persona, sin secretos.
- Con `listo`, el servicio queda `activo` y la facturación empieza según la regla de Argos (decisión del PM, P9). Argos emite además sus eventos de siempre (`servicio.estado_cambiado`, `servicio.instalacion_activada`).

Webhooks Argos → Iris (§2.3): `instalacion.aprovisionada` e `instalacion.fallida`, uno final por aprovisionamiento. Iris los deduplica por `X-Webhook-Id` y los relaciona por `aprovisionamiento_id` o por `idempotency_key`. Si no llega en 10 min, Iris consulta el `GET`.

**En Iris** (informativo para Argos):

- El técnico pide la activación con la operación sin conexión `orden.activar_servicio` (`contratos/sync.md`): se guarda en el teléfono y se envía al volver la señal. Iris la deja en su cola de pendientes y llama a Argos. Un `fallido` no es un rechazo de la operación: el técnico ve la causa y pulsa «Reintentar», que es otro intento (otra clave).
- La orden no se cierra como instalación exitosa sin la activación `lista` (salvo un supervisor, que deja el motivo). Los adjuntos obligatorios de la plantilla siguen aplicando.
- Al cerrar, Iris **no** manda `activar-instalacion` ni el comodato del equipo aprovisionado (§3). `orden.cerrada` lleva `aprovisionamiento_id` y `equipos[].aprovisionado`.

### 12.3 Estado del equipo (A33)

`GET /api/v1/servicios/:id/equipo` (`servicios.ver_equipo`) → `200`:

```json
{
  "servicio_id": 4410,
  "tipo": "ont",
  "serie": "ZTEGC8A1B2C3",
  "modelo": "F670L",
  "en_linea": true,
  "senal": { "rx_dbm": -19.8, "tx_dbm": 2.1 },
  "firmware": "V9.0.10P2N3",
  "ultimo_contacto": "2026-11-21T15:58:02Z",
  "fuente": "smartolt",
  "leido_en": "2026-11-21T15:58:05Z"
}
```

- `senal` solo en `ont` (o `null`). `fuente`: `smartolt` o `acs`. Argos puede responder de su caché si `leido_en` tiene menos de 60 s.
- Errores: `409 equipo_sin_gestion`, `503 equipo_sin_respuesta` (Iris no reintenta solo: muestra «no se pudo leer» con «Reintentar»).
- Nunca trae la clave Wi-Fi ni credenciales del equipo.

### 12.4 Cambiar el Wi-Fi pedido desde Iris (A35)

`POST /api/v1/servicios/:id/wifi` (`servicios.cambiar_wifi`), `Idempotency-Key` obligatoria: `{ "ssid": "…", "clave": "…", "origen_tipo": "iris_orden" | "iris_ticket", "origen_id": "…" }` (al menos uno de `ssid` y `clave`).

- `202 { solicitud_id, estado: "en_proceso" }`. Resultado por el webhook `equipo.wifi_resuelto` (`aplicado` o `fallido` con `causa`: `equipo_sin_gestion`, `equipo_sin_respuesta`, `rechazado_por_equipo`, `otro`).
- Errores inmediatos: `409 equipo_sin_gestion`, `422 wifi_invalido`.
- Argos audita el cambio con `X-Iris-Actor` (§7.1: `usuario:<id>`; `asistente_ia` responde `403`) y la clave como `***`.
````

---

## 8. Notas para la implementación en Iris (no van al contrato)

- **Reusar, no copiar:** las rutas de §11 son casos `ConPermiso` nuevos que llaman a las funciones internas que ya usa el portal (`crearTicket`, `reactivarPorCliente`, `tomar`, `cerrar`, `aTicketPortal`, `aMensajePortal`, `topeTickets`, `topeMensajes`), con el cliente tomado de la ruta y la empresa de `X-Iris-Empresa`. Los topes ya cuentan por `cliente:<empresa>:<cliente>`, así que se comparten solos.
- **Notas internas:** `listar_conversacion.go:19-22` las oculta por canal `portal`; las rutas nuevas corren con canal `integracion`, así que deben pedir `SoloPublicos = true` siempre (no basta con que el rol no tenga `tickets.ver_notas`: un admin sí lo tiene).
- **Webhooks:** hoy viven en `mensajeria/webhooks.go` con una lista fija; conviene un suscriptor de soporte (o un despachador común en `integracion`) que arme el payload leyendo el ticket (el evento interno no trae `cliente_id` ni `empresa_id`), filtre `cliente_id` no nulo y conector con contrato ≥ 0.9.0.
- **Adjuntos de ticket:** `adjuntos.RegistrarEntidadAdjuntable("ticket", …)` con verificador por alcance; el `multipart` de Argos se guarda con el almacén del núcleo y se liga al mensaje (`soporte.ticket_mensajes.adjuntos`). El panel de Iris también debe poder mostrarlos.
- **Prospectos:** migración con `goose create` (`tipo`, `servicio_afectado_id`, origen `portal_argos` en el `CHECK`); `ganar.go` salta `POST /clientes` cuando `cliente_argos_id` ya viene; `prospecto.creado` suma `portal_argos` y `tipo` en `eventos.md` y en el catálogo de `CLAUDE.md`.
- **Activación:** `campo.aprovisionamientos`, operación de pendientes `aprovisionar`, regla nueva en `contratos/sync.md` §2 y §3, `aprovisionamientos[]` en paquete y cambios, `cierre_orden.go` omite activación y comodato aprovisionados, eventos nuevos en el catálogo.
- **Versión:** `ContratoIris` pasa a `"0.9.0"` (`integracion/argos/contrato.go:34`) solo cuando el bloque correspondiente esté implementado; `capacidades` en `GET /integraciones/argosisp/estado`.
- **Rol:** `nucleo/permisos.go:763-766` suma `portal_argos.*`, y el comentario que habla de «pedido A9 propuesto» se corrige (es §11.9 de la 0.9.0).
