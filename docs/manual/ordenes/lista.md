# Órdenes de trabajo

## Para qué sirve

Es el tablero de todas las órdenes de trabajo: instalaciones, visitas técnicas, retiros, cambios de equipo y mantenimientos. Si eres técnico, aquí ves solo las órdenes asignadas a ti.

## Cómo se usa

1. Entra a **Órdenes y tareas** en el menú.
2. Elige cómo verlas con **Vista**: **Lista** o **Columnas** (una columna por estado).
3. Busca por número, dirección o contacto en **Buscar**.
4. Filtra por **Estado** (todas o solo las activas), **Tipo**, **Técnico** (también «Sin técnico»), **Fecha programada** y **Zona**.
5. Toca una orden para abrir su ficha: [Ficha de la orden](/manual/ordenes/detalle).
6. Para una orden nueva, pulsa **Nueva orden**: [Nueva orden](/manual/ordenes/nueva).
7. Al final de la lista, **Cargar más** trae las siguientes.

## Qué significa cada estado

- **Pendiente:** creada, todavía sin fecha.
- **Programada:** tiene técnico y hora.
- **En camino:** el técnico va hacia el cliente.
- **En sitio:** el técnico llegó y está trabajando.
- **Pausada:** el técnico la detuvo (por ejemplo, esperando material).
- **Cerrada:** el trabajo terminó.
- **Fallida:** el técnico no pudo hacer el trabajo. En la app de campo se llama «No realizada».
- **Cancelada:** despacho la anuló.

Otras marcas:

- **Pendiente en Argos:** algo de esta orden todavía no se registró en Argos (activar el servicio, descontar material…). Iris lo reintenta solo.
- **Agendada por el asistente:** la creó el asistente de IA desde una conversación.

## Preguntas frecuentes

**¿Por qué solo veo mis órdenes?**
Tu rol tiene permiso para ver solo las órdenes propias, como el rol Técnico.

**La marca «Pendiente en Argos» no sale en una orden vieja.**
Si hay muchos pendientes, la marca se muestra para los 200 más recientes de cada estado. Iris te avisa cuando pasa.

## Permisos necesarios

- Ver todas: `ordenes.ver` (**Despacho**, **Supervisor**, **Lectura**).
- Ver solo las propias: `ordenes.ver_propias` (**Técnico**).
- Ver la marca de pendientes con Argos: `integraciones.ver_pendientes`.
