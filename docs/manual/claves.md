# Claves del manual

Contrato entre el botón «?» de cada pantalla (`BotonManual({ clave })` de `@iris/ui`, que enlaza a `/manual/<clave>`) y las páginas de este manual. La clave tiene la forma `modulo/pantalla` y su página vive en `docs/manual/<clave>.md`.

- Una clave nueva lleva su archivo y su fila aquí. Una prueba (`apps/web/lib/manual/contenido.test.ts`) revisa que esta tabla y los archivos coincidan.
- Después de cambiar un `.md`, corre `pnpm --filter @iris/web manual` para regenerar el contenido que lee el panel.
- En la app de campo (`/campo`), el botón debe enlazar a `/campo/manual/<pantalla>` (por ejemplo `/campo/manual/hoy` para la clave `campo/hoy`): esas páginas las guarda el service worker y abren sin señal. `/manual/campo/<pantalla>` lleva ahí con señal.
- Una clave sin página muestra «Esta pantalla todavía no tiene manual» y un enlace al índice.

| Pantalla | Clave | Archivo |
| --- | --- | --- |
| Índice del manual | — | `docs/manual/indice.md` |
| `/administracion/accesos` | `administracion/accesos` | `docs/manual/administracion/accesos.md` |
| `/administracion/actividad-app` | `administracion/actividad-app` | `docs/manual/administracion/actividad-app.md` |
| `/administracion/integraciones/argos (y /vuelta)` | `administracion/argos` | `docs/manual/administracion/argos.md` |
| `/administracion/asistente (conexión, límites, perfiles, categorías, reconexión y visitas)` | `administracion/asistente` | `docs/manual/administracion/asistente.md` |
| `/administracion/bandeja` | `administracion/bandeja` | `docs/manual/administracion/bandeja.md` |
| `/administracion/buzones (nuevo y editar)` | `administracion/buzones` | `docs/manual/administracion/buzones.md` |
| `/administracion/integraciones/centralita (conexión, teléfono y extensiones)` | `administracion/centralita` | `docs/manual/administracion/centralita.md` |
| `/administracion/colas (nueva y editar)` | `administracion/colas` | `docs/manual/administracion/colas.md` |
| `/administracion/conexion-meta` | `administracion/conexion-meta` | `docs/manual/administracion/conexion-meta.md` |
| `/administracion/correo` | `administracion/correo` | `docs/manual/administracion/correo.md` |
| `/administracion/cuadrillas` | `administracion/cuadrillas` | `docs/manual/administracion/cuadrillas.md` |
| `/administracion/datos-legales` | `administracion/datos-legales` | `docs/manual/administracion/datos-legales.md` |
| `/administracion/datos-sin-empresa` | `administracion/datos-sin-empresa` | `docs/manual/administracion/datos-sin-empresa.md` |
| `/administracion/horarios (nuevo y editar)` | `administracion/horarios` | `docs/manual/administracion/horarios.md` |
| `/administracion` | `administracion/indice` | `docs/manual/administracion/indice.md` |
| `/administracion/integraciones` | `administracion/integraciones` | `docs/manual/administracion/integraciones.md` |
| `/administracion/llamadas` | `administracion/llamadas` | `docs/manual/administracion/llamadas.md` |
| `/administracion/manuales (nuevo y editar)` | `administracion/manuales` | `docs/manual/administracion/manuales.md` |
| `/administracion/numeros (nuevo, guía, conectar, configurar, credenciales, llamadas, compartir y préstamo)` | `administracion/numeros` | `docs/manual/administracion/numeros.md` |
| `/administracion/plantillas (nueva y ficha)` | `administracion/plantillas` | `docs/manual/administracion/plantillas.md` |
| `/administracion/plantillas-whatsapp (nueva, sugeridas, ficha y editar)` | `administracion/plantillas-whatsapp` | `docs/manual/administracion/plantillas-whatsapp.md` |
| `/administracion/respuestas-rapidas (nueva y editar)` | `administracion/respuestas-rapidas` | `docs/manual/administracion/respuestas-rapidas.md` |
| `/administracion/roles (nuevo y ficha)` | `administracion/roles` | `docs/manual/administracion/roles.md` |
| `/administracion/servidor-ia (avisos, enlazar y mantenimiento)` | `administracion/servidor-ia` | `docs/manual/administracion/servidor-ia.md` |
| `/administracion/tokens` | `administracion/tokens` | `docs/manual/administracion/tokens.md` |
| `/administracion/zonas` | `administracion/zonas` | `docs/manual/administracion/zonas.md` |
| `/agenda` | `agenda/calendario` | `docs/manual/agenda/calendario.md` |
| `/campo («+»: tarea propia y órdenes libres)` | `campo/acciones-rapidas` | `docs/manual/campo/acciones-rapidas.md` |
| `/campo (Agenda, Mañana y Semana)` | `campo/agenda` | `docs/manual/campo/agenda.md` |
| `/campo?vista=bodega` | `campo/bodega` | `docs/manual/campo/bodega.md` |
| `/campo (Chat)` | `campo/chat` | `docs/manual/campo/chat.md` |
| `/campo?orden=N&paso=checklist` | `campo/checklist` | `docs/manual/campo/checklist.md` |
| `/campo?orden=N (firma y cierre)` | `campo/cierre` | `docs/manual/campo/cierre.md` |
| `/campo?orden=N&paso=fotos` | `campo/fotos` | `docs/manual/campo/fotos.md` |
| `/campo (Hoy)` | `campo/hoy` | `docs/manual/campo/hoy.md` |
| `/campo?orden=N&paso=llegada` | `campo/llegada` | `docs/manual/campo/llegada.md` |
| `/campo?orden=N&paso=materiales` | `campo/materiales` | `docs/manual/campo/materiales.md` |
| `/campo?orden=N` | `campo/orden` | `docs/manual/campo/orden.md` |
| `/campo?orden=N (Pausar y No se pudo)` | `campo/pausa` | `docs/manual/campo/pausa.md` |
| `/campo?vista=pendientes` | `campo/por-enviar` | `docs/manual/campo/por-enviar.md` |
| `/campo?vista=recibir` | `campo/por-recibir` | `docs/manual/campo/por-recibir.md` |
| `/campo (sin señal)` | `campo/sin-senal` | `docs/manual/campo/sin-senal.md` |
| `/campo (ubicación y turno)` | `campo/ubicacion` | `docs/manual/campo/ubicacion.md` |
| `/campo?vista=yo` | `campo/yo` | `docs/manual/campo/yo.md` |
| `/comercial/[id]/cotizacion` | `comercial/cotizacion` | `docs/manual/comercial/cotizacion.md` |
| `/comercial` | `comercial/embudo` | `docs/manual/comercial/embudo.md` |
| `/comercial/nuevo` | `comercial/nuevo` | `docs/manual/comercial/nuevo.md` |
| `/comercial/[id]` | `comercial/prospecto` | `docs/manual/comercial/prospecto.md` |
| `/conversaciones` | `conversaciones/bandeja` | `docs/manual/conversaciones/bandeja.md` |
| `/conversaciones/[id]/boleta` | `conversaciones/boleta` | `docs/manual/conversaciones/boleta.md` |
| `/conversaciones/nueva` | `conversaciones/nueva` | `docs/manual/conversaciones/nueva.md` |
| `/conversaciones/[id]/orden` | `conversaciones/orden` | `docs/manual/conversaciones/orden.md` |
| `/conversaciones/[id]/prospecto` | `conversaciones/prospecto` | `docs/manual/conversaciones/prospecto.md` |
| `/cuenta/mfa` | `cuenta/mfa` | `docs/manual/cuenta/mfa.md` |
| `/cuenta` | `cuenta/perfil` | `docs/manual/cuenta/perfil.md` |
| `/despacho` | `despacho/tablero` | `docs/manual/despacho/tablero.md` |
| `/encuestas` | `encuestas/lista` | `docs/manual/encuestas/lista.md` |
| `/equipo` | `equipo/chat` | `docs/manual/equipo/chat.md` |
| `/` | `inicio/tablero` | `docs/manual/inicio/tablero.md` |
| `/mapa` | `mapa/tecnicos` | `docs/manual/mapa/tecnicos.md` |
| `/ordenes/[id]` | `ordenes/detalle` | `docs/manual/ordenes/detalle.md` |
| `/ordenes` | `ordenes/lista` | `docs/manual/ordenes/lista.md` |
| `/ordenes/nueva` | `ordenes/nueva` | `docs/manual/ordenes/nueva.md` |
| `/reportes` | `reportes/indicadores` | `docs/manual/reportes/indicadores.md` |
| `/soporte/[id]` | `soporte/caso` | `docs/manual/soporte/caso.md` |
| `/soporte` | `soporte/mis-casos` | `docs/manual/soporte/mis-casos.md` |
| `/soporte/nuevo` | `soporte/nuevo` | `docs/manual/soporte/nuevo.md` |
| `/tecnicos/argos` | `tecnicos/argos` | `docs/manual/tecnicos/argos.md` |
| `/tecnicos/[id]/ausencias/nueva` | `tecnicos/ausencia` | `docs/manual/tecnicos/ausencia.md` |
| `/tecnicos/[id]` | `tecnicos/ficha` | `docs/manual/tecnicos/ficha.md` |
| `/tecnicos/nuevo y /tecnicos/[id]/editar` | `tecnicos/formulario` | `docs/manual/tecnicos/formulario.md` |
| `/tecnicos` | `tecnicos/lista` | `docs/manual/tecnicos/lista.md` |
| `/tickets/[id]` | `tickets/detalle` | `docs/manual/tickets/detalle.md` |
| `/tickets` | `tickets/lista` | `docs/manual/tickets/lista.md` |
| `/tickets/nuevo` | `tickets/nuevo` | `docs/manual/tickets/nuevo.md` |
| `/usuarios/[id]` | `usuarios/ficha` | `docs/manual/usuarios/ficha.md` |
| `/usuarios/nuevo y /usuarios/[id]/editar` | `usuarios/formulario` | `docs/manual/usuarios/formulario.md` |
| `/usuarios` | `usuarios/lista` | `docs/manual/usuarios/lista.md` |
