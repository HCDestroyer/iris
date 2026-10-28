# agenda

Zonas, cuadrillas, técnicos (turnos, ausencias y capacidad diaria por tipo de orden) y las citas de
las órdenes programadas, con la ventana acordada con el cliente, y las rutas del día (propuesta con
OSRM, publicación y ruta propuesta frente a la real). Schema `agenda`.

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
- **Cuadrillas** con las zonas que atienden y capacidad propia. Una cuadrilla sin zonas atiende
  todas para el filtro `zonaId` de asignables, pero **no** la cubre un alcance por zona (falla
  cerrado: sus técnicos, fichas y posiciones solo los ve el alcance global o el de esa cuadrilla).
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
- **Empresa activa:** zonas y cuadrillas con `empresa_id` null se ven desde todas las empresas,
  pero solo las modifica quien tiene `empresas.todas` sin límite (o en modo sin conector) y, con
  empresas registradas, no se pueden usar en tickets ni órdenes (422): un admin global les asigna
  empresa o las reemplaza. Una cuadrilla cubre por alcance solo si es de la empresa activa.
  Técnicos: los usuarios que operan en la empresa activa; la **ficha** (datos, turnos, capacidad,
  ausencias) se modifica solo desde la empresa de su cuadrilla —sin cuadrilla, o de otra empresa,
  hace falta `empresas.todas`— y el `detalle` de sus ausencias solo lo ve esa empresa. En
  asignables, una cita de otra empresa sale como `ordenId: 0`, `estado: ocupado` (solo el
  horario). `agenda.zonas`, `agenda.cuadrillas` y
  `agenda.citas` se registran para la adopción.

## Rutas del día (`agenda.rutas`, `agenda.ruta_paradas`)

- **Proponer** (`POST /rutas/proponer`, `rutas.proponer`; `ProponerRuta`): las citas del técnico ese
  día en la empresa activa (programadas, en curso y terminadas; hasta 100). Las ya en camino, en
  sitio o terminadas van primero en el orden real (`fija`, con su hora real); el resto se ordena
  para **llegar dentro de la ventana** acordada con el cliente (llegar antes = esperar; después =
  retraso, que pesa 100 veces el tiempo) con la menor hora de terminar y luego el menor viaje.
  Duración de cada parada = la del tipo de orden. Exacto (todas las permutaciones) hasta 8 paradas;
  más, semillas (por ventana, por cita, vecino más cercano) con reubicación y 2-opt, acotada a
  250 ms (por encima de 30 paradas solo 2-opt) y cortada si se cancela la petición. Las paradas
  sin coordenadas van al final (viaje 0). Sale de la base de la ficha (hoy sin paradas hechas, de
  la última posición de menos de 2 h; con paradas hechas, de la última); hora de salida = inicio
  del turno (o la primera cita); hoy, lo pendiente no empieza antes de ahora. `ordenIds` = orden
  manual. Crea o rehace el **borrador** (uno por técnico, día y empresa); la publicada no se toca.
- **OSRM** (`IRIS_OSRM_URL`, `IRIS_OSRM_ESPERA`): solo si el actor puede proponer esa ruta (alcance
  sobre el técnico y todas sus paradas), la matriz (`/table`), el plan y el trazo (`/route`) se
  calculan **una vez, fuera de la transacción**; dentro se vuelve a leer y, si la huella de la
  entrada no cambió, se reutiliza el plan; si cambió, línea recta (`datos_cambiaron`). Sin OSRM o si
  no responde: línea recta (haversine × 1,3 a 30 km/h), mismo orden por ventana y distancia, y la
  ruta lo dice (`motor: haversine`, `degradada: true`, `motivoMotor`), nunca un 500.
- **Publicar** (`POST /rutas/{id}/publicar`, `rutas.publicar`, `version`): solo un borrador vigente
  (422 `ruta_desactualizada` si una cita cambió). La publicada anterior pasa a `reemplazada`
  (historial, `reemplazadaPorId`). Ninguna ruta se borra.
- **Reasignación en caliente:** `ReservarCita` y `QuitarCita` marcan `desactualizada` (con motivo)
  el borrador y la publicada del técnico y día de antes y de después. La publicada sigue siendo la
  del técnico hasta publicar otra; el paquete lo avisa.
- **Leer** (`GET /rutas?fecha=`, `GET /rutas/{id}`, `rutas.ver`): paradas con llegada estimada y
  real (`en_sitio`), `desvioSeg`, `vigente`, y el recorrido real del día (`campo.posiciones`,
  muestreado a 1000 puntos; exige `campo.ver_posiciones`, si no `disponible: false`).
- **Alcance (falla cerrado):** la ruta se ve si el alcance de `rutas.ver` cubre la cuadrilla del
  técnico (como las fichas) y es de la empresa activa; si no, 404. Una parada cuya orden el alcance
  no ve (ni zona, ni cuadrilla de la orden, ni cuadrilla del técnico por alcance de cuadrilla) sale
  `oculta` (solo su posición en la secuencia: sin orden, estado, horas, lugar ni viaje; la
  siguiente sin viaje ni distancia; la ruta sin `geometria`), y con ella no se puede proponer ni
  publicar (403). Si la ruta sale de la última posición del técnico, la salida y el trazo solo
  los ve quien tiene `campo.ver_posiciones` sobre él. Una ruta solo lleva órdenes de su empresa.
- **App de campo:** `RutaPublicadaDelTecnico` (campo arma `ruta` del paquete y de los cambios) y
  `TurnoDelDia` (turno del día sin ausencias: `turno` del paquete).

## Para soporte y campo

`ValidarZona` (zona indicada por una persona: existe, activa, de la empresa; 422 en el campo),
`ZonaDeArgos` (zona de un dato de Argos: la copia si falta), `ValidarCuadrilla`, `ReservarCita`,
`QuitarCita` (método: marca la ruta), `CitasDeOrdenes`, `RutaPublicadaDelTecnico`. `soporte.tickets` y `campo.ordenes` tienen llave foránea de
`zona_id` y `cuadrilla_id` al catálogo (migración `20261028175434`: los valores huérfanos se
conservan como zona o cuadrilla «migrada» con el mismo id. Si la usan varias empresas nace sin
empresa e **inactiva**: las filas que ya la referencian siguen válidas (la FK no mira `activa`),
pero no se usa en datos nuevos hasta que un admin global (`empresas.todas`) le asigne empresa —hoy
por SQL, `UPDATE agenda.zonas SET empresa_id = …, activa = true`— o la reemplace).

## Eventos

- **Emite:** `orden.programada` (en la transacción de la acción de campo), `zona.creada`,
  `zona.actualizada`, `cuadrilla.creada`, `cuadrilla.actualizada`, `tecnico.creado`,
  `tecnico.actualizado`, `tecnico.ausencia_registrada`, `tecnico.ausencia_anulada`, `ruta.propuesta`,
  `ruta.publicada`, `ruta.desactualizada`
  (`.claude/integration/contratos/eventos.md`).
- **Consume:** nada (el espejo de zonas lee la instantánea que refresca el conector de Argos).
