# comercial

Seguimiento comercial (Fase 4): prospecto → factibilidad → cotización → contrato → ganar (alta del
cliente y el servicio en Argos y orden de instalación). Schema `comercial`.

## Qué hace

- **Prospectos** (`comercial.prospectos`, `PR-000001`): origen (`panel`, `web`, `referido`,
  `argos` —solo la integración o el sistema— u `otro`), contacto, dirección y coordenadas, zona
  (catálogo de agenda), plan de interés (id de Argos) y vendedor. Estados `nuevo`, `contactado`,
  `factibilidad`, `cotizado`, `contrato`, `ganado`, `perdido`; cada paso es una acción con su
  permiso (`POST /prospectos/{id}/<acción>`), nunca un PATCH de estado. Historial en
  `comercial.prospecto_eventos`.
- **Actividades** (`llamada`, `visita`, `nota`, `mensaje`) con la próxima acción y su fecha.
- **Factibilidad** (`comercial.factibilidades`, historial; vale la última) por la interfaz
  `PlantaExterna.Evaluar(ctx, empresa, lat, lon)`, que se llama **fuera de la transacción** (la
  fase 6 enchufa OZmap sin tocar los casos de uso). `PlantaPorDistancia`: línea recta al router
  más cercano de `integracion.argos_routers` de la empresa con los umbrales de
  `comercial.umbrales_factibilidad` (por empresa; por defecto 300 m viable y 1000 m requiere
  obra). Sin conector, conector apagado, sin empresa, sin instantánea o sin routers con
  coordenadas: `indeterminado` con `motivo` (nunca un resultado inventado). `manual`: el resultado
  de una visita técnica (exige notas); es lo que permite vender en modo sin conector.
- **Cotización** (`COT-000001`, `revision` 1, 2…): exactamente una línea de plan (con conector, un
  plan de Argos de `GET /planes` con su nombre y precio; sin conector, descripción y precio a
  mano), instalación, equipos, descuentos y otros, mensuales o únicos. Montos en `numeric(14,2)`,
  calculados en centavos (sin flotantes) y expuestos como texto decimal. Vigencia (15 días por
  defecto). Una revisión nueva reemplaza la vigente o aceptada sin borrarla y anula el contrato en
  borrador. El PDF se genera en el servidor (go-pdf/fpdf, determinista) y se guarda como adjunto
  del núcleo (`entidad = cotizacion`, tipo `documento`).
- **Contrato** (`CTR-000001`): plantilla `estandar_v1` llena con el prospecto y la cotización
  aceptada (`contenido`), PDF borrador para leer y **firma del cliente capturada como imagen**
  (SVG de trazos, igual que la firma de la orden de campo) en el dispositivo del vendedor. El PDF
  firmado (la firma dibujada) queda como adjunto (`entidad = contrato`) y su SHA-256 en
  `auditoria.acciones` (`entrada.auditoria.pdf_firmado_sha256`) y en el contrato. El SVG queda
  enmascarado (`***`) en la auditoría. Se eligió frente a la aceptación por enlace con token: no
  abre un endpoint público sin sesión ni necesita un canal hacia el cliente.
- **Ganar** (`POST /prospectos/{id}/ganar`): exige el contrato firmado, su cotización aceptada y
  la última factibilidad viable (o `requiere_obra` con `confirmarObra: true`) sobre las
  coordenadas actuales del prospecto.
  - **Con conector** (activo o apagado, como la cola de I1): encola en `integracion.pendientes`
    `POST /clientes` (`iris:cliente:<prospecto_id>`, entidad `prospecto`). Al quedar hecho
    (`integracion.pendiente_hecho` → `SuscribirAltas`, o el barrido `comercial.completar_altas`
    cada 5 min), recupera el id del cliente repitiendo la llamada con la misma Idempotency-Key
    (Argos devuelve el mismo resultado) y encola `POST /servicios` con
    `estado = pendiente_instalacion` (`iris:servicio:<prospecto_id>`, `depende_de` el primero).
    Al quedar hecho, crea la orden de instalación con el cliente y el servicio de Argos
    (`origen = sistema`, clave `comercial.instalacion:<id>`: una sola). `alta.estado`:
    `esperando_cliente` → `esperando_servicio` → `completa`.
  - **Sin conector** (nunca se conectó Argos): crea la orden con los datos capturados; el prospecto
    queda ganado sin ids de Argos (`alta.modo = local`).
- **Permisos y alcance:** `prospectos.*`, `factibilidad.evaluar|configurar`,
  `cotizaciones.crear|aceptar`, `contratos.crear|firmar`. Un prospecto se ve con `prospectos.ver`
  si el alcance es global, cubre su zona o el actor es su vendedor; si no, 404 (falla cerrado: un
  prospecto sin zona no lo ve un alcance por zona). Visible pero fuera del alcance de la acción:
  403 `fuera_de_alcance`. Crear con alcance limitado exige una zona propia. Empresa activa como en
  soporte y campo (sin conector, `empresa_id` null); las tablas se adoptan con la empresa.
- **Idempotencia y concurrencia:** todas las acciones aceptan `Idempotency-Key`; crear prospecto,
  actividad y cotización además la guardan (clave retomada = lo ya creado). Las acciones llevan
  `version` (409 `conflicto_version`).

## Eventos

Emite `prospecto.creado`, `prospecto.actualizado`, `prospecto.asignado`, `prospecto.estado_cambiado`,
`prospecto.actividad_registrada`, `prospecto.perdido`, `prospecto.ganado`,
`prospecto.alta_actualizada`, `factibilidad.evaluada`, `factibilidad.umbrales_cambiados`,
`cotizacion.creada`, `cotizacion.aceptada`, `contrato.creado`, `contrato.firmado` y
`contrato.anulado` (detalle en `.claude/integration/contratos/eventos.md`, «Comercial»). Ganar y el
alta emiten además, por sus casos de uso, `orden.creada` (campo), `adjunto.*` (los PDF) y los de la
cola de pendientes.

Consume `integracion.pendiente_hecho` (entidad `prospecto`) para avanzar el alta en Argos.

## Pruebas

`comercial_test.go` (Postgres real y un Argos falso con `httptest`: embudo sin conector, embudo
con Argos hasta la orden con cliente y servicio, Argos caído, sin duplicar, requiere obra,
umbrales por empresa, conector apagado; revisiones y vencimiento; permisos, alcance,
idempotencia y versión; multiempresa), `http_test.go` (rutas, Idempotency-Key del middleware,
409, descarga del PDF) y `pdf_test.go` (PDF determinista, montos).
