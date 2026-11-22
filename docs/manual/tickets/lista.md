# Bandeja de tickets

## Para qué sirve

Aquí ves todos los casos de soporte de la empresa activa: los que entran por el panel, el portal del cliente, el correo, Horus o Argos. Desde la bandeja eliges qué atender primero y abres la ficha de cada ticket.

## Cómo se usa

1. Entra a **Tickets** en el menú.
2. Elige una vista arriba: **Activos**, **Asignados a mí**, **En espera** o **Todos**.
3. Si buscas algo concreto, escribe en **Buscar** el número (TK-000123), el asunto o el contacto.
4. Abre **Filtros** para acotar por estado, prioridad, categoría o asignación.
5. Toca un ticket para abrir su ficha. Asignar, escalar y resolver se hacen ahí.
6. Para repartir varios tickets a la vez, márcalos con la casilla y usa **Asignar** en la barra de la selección. Cada ticket se asigna por separado: si alguno falla, Iris te dice cuál.
7. Si usas siempre los mismos filtros, pulsa **Guardar vista** y ponle un nombre. Aparece en **Mis vistas** de la barra lateral.
8. Al final de la lista, **Cargar más** trae los siguientes.

## Qué significa cada estado

- **Nuevo:** acaba de entrar y nadie lo ha tomado.
- **Abierto:** alguien lo asignó o ya respondió.
- **En espera:** se espera algo del cliente o de un proveedor. Mientras tanto, el plazo de resolución no corre.
- **Resuelto:** se dio una solución. Si el cliente responde, puede volver a abrirse.
- **Cerrado:** terminado. Ya no cambia.

El reloj de SLA de cada ticket dice si va a tiempo:

- **En tiempo:** le queda margen.
- **En riesgo:** ya pasó el 80 % del plazo.
- **Vencido:** se pasó del plazo.
- **Pausado:** el plazo está detenido (ticket en espera).
- **Cumplido** o **Incumplido:** cómo terminó el plazo.
- **Sin SLA:** la categoría no tiene plazo.

La prioridad va de **Baja** a **Media**, **Alta** y **Crítica**.

## Preguntas frecuentes

**¿Dónde quedan mis vistas guardadas?**
En este navegador. Si entras desde otra computadora, tendrás que guardarlas de nuevo.

**No veo un ticket que sé que existe.**
Revisa la empresa activa y los filtros. Si tu rol tiene alcance por zona o cuadrilla, solo ves los tickets de tu zona o cuadrilla, o los que tienes asignados.

**¿Puedo cambiar el estado desde la lista?**
No. Desde la lista solo se asigna. El resto de acciones están en la ficha del ticket: [Ficha del ticket](/manual/tickets/detalle).

## Permisos necesarios

- Ver la bandeja: `tickets.ver`.
- Asignar desde la selección: `tickets.asignar`.
- Los roles base **Soporte**, **Supervisor**, **Despacho** y **Lectura** pueden ver tickets.
