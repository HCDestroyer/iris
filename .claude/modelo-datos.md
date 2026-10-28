# Modelo de datos base de Iris

Postgres 16 con un schema por módulo. Los ids son `bigint` (identity), salvo los generados en el teléfono, que son `uuid` v7. Todas las tablas llevan `created_at` y `updated_at` en UTC, y las editables llevan `version`. Todo lo operativo lleva `empresa_id`, que es el id de la empresa en Argos, o `null` sin conector.

## nucleo

- `usuarios` (nombre, correo, hash argon2id, totp, activo, `usuario_argos_id` opcional)
- `roles`, `permisos`, `roles_permisos`
- `usuarios_roles` (alcance: `zona_id` o `cuadrilla_id` opcionales)
- `sesiones`, `tokens_api` (prefijo `iris_pat_`, guardado como hash)
- `idempotencia` (clave, usuario, hash de la petición, respuesta, expira)
- `empresas` (instantánea de Argos: id, nombre, NIT)

## auditoria

- `acciones` (actor, canal, accion, entidad, entidad_id, entrada, resultado, request_id), solo inserción
- `cambios` (accion_id, entidad, entidad_id, campo, antes, despues), solo inserción

## integracion

- `conectores` (sistema `argosisp` | `horus`, base_url, token cifrado, secreto del webhook cifrado, estado)
- `outbox` (evento, payload, publicado_en)
- `entrantes` (conector_id, webhook_id único, evento, payload, procesado_en, error)
- `pendientes` (operación hacia Argos u Horus, idempotency_key única, intentos, próximo intento, último error, estado)
- `argos_clientes`, `argos_servicios` (instantánea, §2.2 del contrato), `argos_zonas`, `argos_routers`

## soporte

- `categorias` (nombre, SLA por prioridad, requiere orden)
- `slas` (respuesta en minutos y resolución en minutos por prioridad; con calendario o 24/7)
- `calendarios`, `feriados` (Guatemala por defecto)
- `tickets`:
    - datos: numero, empresa_id, cliente_id, servicio_id, categoria_id, prioridad (`baja`, `media`, `alta`, `critica`), estado (`nuevo`, `abierto`, `en_espera`, `resuelto`, `cerrado`), origen (`panel`, `portal`, `correo`, `telegram`, `horus`, `argos`, `api`), asunto, asignado_a, cuadrilla_id;
    - SLA: vence_respuesta, vence_resolucion, primera_respuesta_en, resuelto_en;
    - relaciones: ticket_padre_id (incidente maestro) y fusionado_en.
- `ticket_mensajes` (publico/interno, autor, cuerpo, adjuntos)
- `ticket_eventos` (historial de estado, asignación y SLA)
- `encuestas` (calificación y comentario al cerrar)

## campo

- `tipos_orden` (instalacion, visita, retiro, cambio_equipo, mantenimiento; duración estimada; plantilla de checklist)
- `plantillas_checklist`, `plantilla_items` (tipo `si_no`, `texto`, `numero`, `foto`, `medicion`; obligatorio)
- `ordenes`:
    - datos: numero, empresa_id, tipo_id, ticket_id, prospecto_id, cliente_id, servicio_id, dirección y coordenadas, zona_id;
    - estado: `pendiente`, `programada`, `en_camino`, `en_sitio`, `cerrada`, `fallida`, `cancelada`;
    - cierre: resultado, motivo_falla.
- `orden_checklist` (item, valor, marcado_por, ocurrido_en)
- `orden_materiales` (producto_id de Argos, cantidad, bodega_id)
- `orden_equipos` (accion `instalar` | `retirar`, unidad_id de Argos, serie, mac, condicion)
- `adjuntos` (uuid, orden_id o ticket_id, tipo `foto` | `firma` | `documento`, sha256, tamano, gps, tomado_en, estado de subida)
- `firmas` (orden_id, nombre, documento, svg, adjunto_id)
- `operaciones_sync` (uuid, dispositivo, tipo, resultado), para idempotencia del lote
- `dispositivos` (usuario, plataforma, último contacto, desfase de reloj)
- `posiciones` (tecnico_id, lat, lon, precision, registrada_en), particionada por mes, sin auditoría por fila

## agenda

- `tecnicos` (usuario_id, bodega_id de Argos, cuadrilla_id, habilidades[], base lat/lon)
- `cuadrillas`, `zonas` (espejo de Argos más polígono propio opcional)
- `turnos` (técnico, día de la semana, inicio, fin), `ausencias`
- `citas` (orden_id, técnico, inicio, fin, ventana acordada con el cliente)
- `rutas` (técnico, fecha, estado `borrador` | `publicada`, distancia y duración estimadas), `ruta_paradas` (orden_id, posición, llegada estimada)

## comercial

- `prospectos`:
    - datos: nombre, telefono, correo, direccion, lat, lon, origen, plan_interes (plan_id de Argos), vendedor_id;
    - estado: `nuevo`, `contactado`, `factibilidad`, `cotizado`, `contrato`, `ganado`, `perdido`;
    - cierre: motivo_perdida, cliente_argos_id, servicio_argos_id.
- `factibilidades` (prospecto_id, resultado `viable` | `no_viable` | `requiere_obra`, router y caja sugeridos, distancia, notas)
- `cotizaciones` (prospecto_id, lineas, total en `numeric(14,2)`, vigencia, PDF)
- `contratos` (prospecto_id, plantilla, firma, PDF)
- `actividades` (llamada, visita, mensaje; próxima acción y fecha)
