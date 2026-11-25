# Ficha de la orden

## Para qué sirve

Ver todo de una orden: el cliente, la cita, el técnico, lo que hizo en el sitio (fotos, firma, materiales, equipos y GPS), los formularios, lo pendiente con Argos y el chat del equipo. Desde aquí se asigna, se programa, se pausa, se reabre o se cancela.

## Cómo se usa

1. Abre una orden desde [Órdenes de trabajo](/manual/ordenes/lista).
2. Revisa las pestañas:
    - **Resumen:** técnico, cita, cuadrilla, resultado, horas (salió, llegó, finalizada), cliente y ticket de origen.
    - **Evidencias:** checklist, fotos, firma del cliente, ubicación y GPS, materiales, equipos y notas del técnico.
    - **Formularios:** los checklists que llenará el técnico. Puedes **Agregar formulario** o quitar uno mientras no tenga respuestas.
    - **Argos:** lo que esta orden tiene pendiente en Argos.
    - **Chat del equipo:** el chat interno de la orden con el técnico.
3. Usa las acciones del panel:
    - **Asignar técnico:** elige quién va.
    - **Programar visita** o **Reprogramar visita:** día y hora de Guatemala. El fin se calcula con la duración del tipo de orden.
    - **Pausar la orden** y **Reanudar la orden**.
    - **Reabrir orden:** vuelve a pendiente para asignarla y programarla de nuevo. Pide el motivo.
    - **Cancelar orden:** pide el motivo. La orden deja de estar en la ruta del técnico.
4. Si el ticket y la orden tienen responsables distintos, Iris ofrece **Asignar la orden como el ticket** o **Asignar el ticket como la orden**.

## Qué significa cada estado

Estados de la orden: **Pendiente**, **Programada**, **En camino**, **En sitio**, **Pausada**, **Cerrada**, **Fallida** y **Cancelada** (ver [Órdenes de trabajo](/manual/ordenes/lista)).

Pendientes en Argos (pestaña **Argos**):

- **En cola** y **Enviando:** Iris lo está mandando.
- **Hecho:** Argos ya lo registró.
- **Argos lo rechazó:** Argos dijo que no. Lee el **Último error**.
- **Sin respuesta de Argos:** se agotaron los reintentos.
- **Bloqueado:** espera a que termine otra acción de la que depende.
- **Resuelto a mano:** alguien lo arregló directamente en Argos.
- **Conflicto en Argos:** hay un cierre repetido con otros datos. Corrígelo a mano en Argos.

Materiales y equipos:

- **Más de lo que había en bodega:** el técnico usó más de lo que tenía registrado.
- **Verificado en el comodato** o **Sin verificar con Argos:** si el equipo coincide con lo que Argos tiene prestado al cliente.

## Preguntas frecuentes

**Argos rechazó un pendiente. ¿Qué hago?**
Lee el error. Si se puede, pulsa **Reintentar**. Si lo arreglaste en Argos, usa **Resolver a mano** y explica qué hiciste: queda en el historial.

**Me aparece «Cambió un dato del cliente o del servicio en Argos».**
Argos avisó un cambio (dirección, coordenadas, referencia o contacto). Usa **Aplicar datos de Argos** para copiarlos a la orden, o **Descartar el aviso de Argos** para quedarte con los de la orden.

**La orden pide firma y no la tiene.**
La firma la captura el técnico en la app de campo, en el paso final. Hasta tenerla, la orden no se cierra.

**Me sale «Alguien más cambió esta orden».**
Otra persona la modificó. Iris la recarga; revisa y vuelve a intentarlo.

## Permisos necesarios

- Ver: `ordenes.ver` u `ordenes.ver_propias`.
- Acciones: `ordenes.asignar`, `ordenes.programar`, `ordenes.reprogramar`, `ordenes.pausar`, `ordenes.reanudar`, `ordenes.reabrir`, `ordenes.cancelar`, `ordenes.asignar_formulario`, `ordenes.igualar_asignacion`, `ordenes.aplicar_datos_argos` y `ordenes.descartar_datos_argos` (**Despacho** y **Supervisor**).
- Reintentar o resolver a mano un pendiente de Argos: `integraciones.reintentar` e `integraciones.resolver_pendiente` (**Supervisor**).
