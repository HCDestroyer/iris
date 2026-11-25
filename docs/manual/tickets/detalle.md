# Ficha del ticket

## Para qué sirve

Es la hoja completa de un caso: los datos del cliente, la conversación con él, las notas del equipo, las órdenes de trabajo que salieron del ticket y todo lo que pasó con él. Las acciones (responder, asignar, escalar, resolver…) están en el panel de acciones.

## Cómo se usa

1. Abre un ticket desde la [Bandeja de tickets](/manual/tickets/lista).
2. Revisa las pestañas **Conversación**, **Órdenes** e **Historial**. Los datos del ticket (cliente, responsable, categoría, prioridad y SLA) están al lado.
3. Para escribirle al cliente, usa **Responder**. Elige el tipo de mensaje:
    - **Respuesta:** la ve el cliente.
    - **Nota interna:** solo la ve el equipo.
4. Para darle un responsable, usa **Asignar** y elige a la persona o la cuadrilla. Debe poder ver el ticket y operar en esta empresa.
5. Si el caso necesita más prioridad, usa **Escalar**, elige la nueva prioridad y escribe el motivo.
6. Si esperas algo del cliente o de un proveedor, usa **Poner en espera** y di qué se espera. Cuando llegue, usa **Reanudar**.
7. Cuando esté arreglado, usa **Resolver** y describe la solución: el cliente la verá.
8. Si hace falta ir a la casa del cliente, usa **Crear orden** (o **Crear orden y asignar**). La orden hereda el cliente, el servicio y la zona del ticket.
9. Si dos tickets son el mismo caso, usa **Fusionar** y escribe el número del ticket que se queda (por ejemplo TK-000123).
10. **Reabrir** devuelve un ticket resuelto a abierto. **Cerrar** lo termina; si no estaba resuelto, pide el motivo.

## Qué significa cada estado

- **Nuevo**, **Abierto**, **En espera**, **Resuelto** y **Cerrado:** igual que en la [Bandeja de tickets](/manual/tickets/lista).
- **Contacto sin verificar:** el correo que abrió el ticket no se pudo autenticar. Ten cuidado antes de dar datos del cliente.
- **Parte de un incidente:** el ticket quedó ligado a una caída de red (un ticket de incidente de Horus). Cuando se resuelve el incidente, Iris avisa a todos sus tickets.
- **Ticket fusionado:** se unió a otro ticket, que es el que sigue.
- **Propuesta de resolución:** Horus avisó que el problema terminó, o se cerró la orden de trabajo del ticket. Iris propone resolverlo, pero nunca lo resuelve solo: lo decides tú.
- **Correo sin verificar** o **Correo de otro remitente:** ese correo se guardó como nota interna. No cambió el estado del ticket y ese remitente no recibe las respuestas.

## Preguntas frecuentes

**Me sale «Alguien más cambió este ticket».**
Otra persona lo modificó mientras lo tenías abierto. Pulsa **Recargar**, revisa el cambio y vuelve a intentarlo.

**¿El cliente ve las notas internas?**
No. Solo las ve el equipo, y solo quien tiene permiso para ver notas.

**Respondí y el cliente recibe la respuesta por correo. ¿Por qué?**
Si el ticket entró por correo, la respuesta pública sale por correo al contacto del ticket.

**¿Qué pasa con el reloj de SLA al poner en espera?**
Se detiene el plazo de resolución, no el de primera respuesta.

## Permisos necesarios

- Ver: `tickets.ver`. Ver notas internas: `tickets.ver_notas`.
- Cada acción tiene su permiso: `tickets.responder`, `tickets.nota_interna`, `tickets.asignar`, `tickets.escalar`, `tickets.poner_en_espera`, `tickets.reanudar`, `tickets.resolver`, `tickets.reabrir`, `tickets.cerrar`, `tickets.fusionar` y `tickets.crear_orden`.
- Los roles **Soporte** y **Supervisor** tienen todos. **Despacho** puede ver, crear, asignar y dejar notas internas.
