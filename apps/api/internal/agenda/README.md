# agenda

Zonas, cuadrillas, técnicos (turnos, ausencias y capacidad diaria por tipo de orden) y las citas de
las órdenes programadas, con la ventana acordada con el cliente. Schema `agenda`. La propuesta y
publicación de rutas (OSRM) llega en la PR siguiente.

## Qué hace

- **Zonas** (`agenda.zonas`): espejo de `integracion.argos_zonas` con **el mismo id** que en Argos
  (así `zona_id` de tickets, órdenes, servicios y alcances sigue siendo el de Argos) más las
  **locales** (sin conector o fuera del catálogo; ids desde 10^12) y las **migradas** (ids que ya
  estaban en uso sin catálogo). Polígono GeoJSON propio opcional (Polygon o MultiPolygon, anillos
  cerrados, hasta 5000 vértices), también en las de Argos. El nombre de una zona de Argos no se
  edita en Iris. Job `agenda.espejo_zonas` (cada 10 min y al arrancar el worker): nuevas,
  renombradas o de otra empresa; las que salen del catálogo de Argos se desactivan (solo si Iris
  tiene el catálogo de esa empresa). Una zona que llega por un dato de Argos antes que el catálogo
  se copia al vuelo como «Zona N» (`ZonaDeArgos`).
- **Cuadrillas** con las zonas que atienden (sin zonas = todas) y capacidad propia.
- **Técnicos**: una ficha por usuario (la persona: sus turnos, ausencias y citas valen en todas
  sus empresas) con cuadrilla, bodega de Argos, habilidades, base y `activo`. Solo personas activas
  con `ordenes.ver_propias` u `ordenes.ver` que operen en la empresa activa.
- **Turnos** semanales en hora de Guatemala (día ISO, `HH:MM`, fin hasta `24:00`, sin cruces en un
  día; un turno nocturno se parte en dos). **Ausencias** (rango UTC, motivo); registrar una devuelve
  las citas vivas que caen dentro (no se mueven solas). **Capacidad** diaria por tipo de orden (o
  todos), en cupos y/o minutos, del técnico o de la cuadrilla (suma de sus técnicos); sin líneas =
  sin límite.
- **Citas** (`agenda.citas`, una por orden): campo delega en `ReservarCita` al programar,
  reprogramar o cambiar el técnico de una orden programada. Valida, con un candado por técnico (y
  por cuadrilla): ficha activa (`tecnico_sin_agenda`), cita dentro de un turno y sin cruzar la
  medianoche (`fuera_de_turno`), sin ausencia (`tecnico_ausente`), sin cruzarse con otra cita viva
  del técnico —programada, en camino o en sitio, de cualquier empresa— (`solape`), capacidad del
  día —cuentan las vivas y las cerradas— (`capacidad_excedida`) y ventana (`fuera_de_ventana`: el
  inicio cae dentro; hasta 24 h). Al reprogramar sin ventana nueva se conserva la anterior si el
  nuevo inicio cae en ella. Sin técnico, cancelada o reabierta: la cita se borra (`QuitarCita`).
- **Asignables** (`GET /tecnicos/asignables?zonaId=&fecha=&tipo=`): fichas activas que atienden la
  zona (cuadrilla) y pueden ver sus órdenes (la misma regla con que campo valida al técnico), con
  turnos, ausencias, citas, huecos libres y capacidad usada del día; `disponible` o `motivo`
  (`sin_turno`, `ausente`, `capacidad_completa`, `sin_espacio`). Los disponibles primero.
- **Calendario** (`GET /agenda?desde=&hasta=&tecnicoId=&cuadrillaId=&zonaId=`, hasta 31 días): citas
  con su orden y ausencias de los técnicos visibles.
- **Permisos** `agenda.*` (uno por acción). Zonas y cuadrillas: el permiso sin límite de alcance
  (403 si no). Técnicos: alcance por cuadrilla (o por las zonas que atiende su cuadrilla); fuera de
  lo visible, 404; visible sin alcance para la acción, 403 `fuera_de_alcance`.
- **Empresa activa:** zonas y cuadrillas con `empresa_id` null son de todas; si no, de la activa.
  Técnicos: los usuarios que operan en la empresa activa. `agenda.zonas`, `agenda.cuadrillas` y
  `agenda.citas` se registran para la adopción.

## Para soporte y campo

`ValidarZona` (zona indicada por una persona: existe, activa, de la empresa; 422 en el campo),
`ZonaDeArgos` (zona de un dato de Argos: la copia si falta), `ValidarCuadrilla`, `ReservarCita`,
`QuitarCita`, `CitasDeOrdenes`. `soporte.tickets` y `campo.ordenes` tienen llave foránea de
`zona_id` y `cuadrilla_id` al catálogo (migración `20261028175434`: los valores huérfanos se
conservan como zona o cuadrilla «migrada» con el mismo id; ver el comentario de la migración).

## Eventos

- **Emite:** `orden.programada` (en la transacción de la acción de campo), `zona.creada`,
  `zona.actualizada`, `cuadrilla.creada`, `cuadrilla.actualizada`, `tecnico.creado`,
  `tecnico.actualizado`, `tecnico.ausencia_registrada`, `tecnico.ausencia_anulada`
  (`.claude/integration/contratos/eventos.md`).
- **Consume:** nada (el espejo de zonas lee la instantánea que refresca el conector de Argos).
