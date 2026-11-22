# Conversaciones

## Para qué sirve

Es la bandeja de WhatsApp: aquí llegan los mensajes de los clientes a los números de la empresa y aquí se contestan. Desde cada conversación también puedes crear tickets, órdenes y prospectos, registrar boletas de pago y promesas, y pedir ayuda al equipo.

## Cómo se usa

1. Entra a **Conversaciones** en el menú.
2. Marca tu disponibilidad arriba: **Disponible** o **No disponible**. Solo a quien está disponible le llegan conversaciones nuevas.
3. Elige qué ver: **Mías**, **Sin asignar** o **Todas**. Filtra por **SLA por vencer**, **SLA vencido**, **Esperando al cliente** o **Resueltas**, o busca por nombre, número o ticket.
4. Abre una conversación. Si nadie la atiende, pulsa **La atiendo**.
5. Escribe abajo y envía. Para mandar algo que se repite, escribe `/` y el atajo de una **Respuesta rápida**. Con el clip adjuntas fotos, documentos (PDF) o usas la cámara.
6. Para dejar algo solo para el equipo, cambia a **Nota interna**. Puedes mencionar a un compañero o **Pedir ayuda al supervisor**.
7. En la ficha de la derecha:
    - busca y asocia al **Cliente de Argos** (o guárdalo como **Contacto de Iris** si no es cliente);
    - revisa sus servicios, tickets y lo **Creado desde esta conversación**;
    - pon **Etiquetas**.
8. Desde **⋯ Más acciones**: **Asignar…**, **Pasar a…** otra cola, **Pasar a otra empresa**, **Crear ticket**, crear orden o prospecto, registrar una **Boleta de pago** o una **Promesa de pago**.
9. Si el asistente de IA está encendido, verás su **Sugerencia del asistente**. Puedes usarla, editarla, marcarla como **Útil** o **No útil**, u ocultarla.
10. Busca guías para el cliente con **Buscar en los manuales**: puedes **Enviar al cliente** un manual o **Usar en la respuesta** su texto.
11. Cuando el caso termine, resuelve la conversación.

## Qué significa cada estado

Reloj de respuesta (el punto de cada conversación):

- **En tiempo:** aún hay margen para contestar.
- **SLA por vencer:** se está por pasar el tiempo de respuesta de la cola.
- **SLA vencido:** el cliente lleva demasiado esperando.
- **Sin pendiente:** no falta contestar nada.

Otras marcas:

- **Sin asignar:** nadie la ha tomado.
- **Esperando al cliente:** la última palabra fue del equipo.
- **Ventana de 24 h cerrada:** el cliente no escribe hace más de 24 horas. WhatsApp solo deja mandarle una plantilla aprobada.
- **Cliente pendiente de confirmar:** Iris cree que es ese cliente por su número, pero un supervisor debe confirmarlo antes de mandarle plantillas con sus datos.
- **Asistente en pausa en esta conversación:** el asistente de IA no contesta aquí hasta que alguien lo reanude.

Boletas de pago: **Enviándose a Argos**, **Pendiente de revisar**, **Aprobada**, **Rechazada** y **Argos no la aceptó**.

## Preguntas frecuentes

**No puedo mandar un mensaje normal.**
La ventana de 24 horas se cerró. Usa una plantilla aprobada: solo se puede si el cliente dio permiso de contacto por WhatsApp.

**Me sale «Ya la tomó otra persona».**
Otro agente la tomó primero. Iris recarga la conversación.

**Me pasé a No disponible sin querer.**
Sin teclado, ratón ni la pestaña a la vista durante un tiempo, Iris te pasa a **No disponible**. También al cerrar sesión o fuera de tu horario. Vuelve a marcar **Disponible**.

**¿Quién aprueba las boletas?**
Caja, desde la conversación, con **Aprobar y registrar pago** o **Rechazar boleta**. La primera vez debe vincular su usuario de Argos.

## Permisos necesarios

- Ver: `conversaciones.ver`. Contestar: `conversaciones.responder`. Tomar: `conversaciones.tomar`.
- Asignar, pasar, transferir, asociar cliente, etiquetar y resolver tienen su propio permiso (`conversaciones.asignar`, `conversaciones.pasar`, `conversaciones.transferir`, `conversaciones.asociar`, `conversaciones.etiquetar`, `conversaciones.resolver`).
- Boletas: `conversaciones.registrar_boleta`; aprobar o rechazar, el rol **Caja**.
- Los roles **Soporte**, **Ventas**, **Despacho** y **Supervisor** atienden conversaciones.
