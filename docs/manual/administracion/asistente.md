# Asistente de IA

## Para qué sirve

Configurar el asistente que sugiere respuestas en la bandeja y, si lo permites, atiende solo algunas conversaciones: proveedor de IA, cuándo sugiere, qué puede consultar, cuánto puede gastar al mes, sus límites y sus perfiles por horario.

## Cómo se usa

1. En **Administración**, abre **Asistente de IA**.
2. **Conexión con la IA:** elige el **Proveedor de IA**, pega su clave (se guarda cifrada) y elige el **Modelo que usa el asistente**. Pulsa **Probar conexión**.
3. **Cuándo sugiere:** **Al llegar un mensaje del cliente** (la sugerencia ya está lista al abrir la conversación) o **Solo al pulsar «Sugerir»** (gasta menos).
4. **Uso del mes:** pon el **Tope mensual (tokens)**. Al 80 % avisa a los administradores; al 100 % se pausa hasta el mes siguiente. Si el proveedor cobra, pon sus precios para ver el uso en dinero.
5. **Límites del asistente:** qué puede hacer solo, la **Prioridad máxima** de los tickets que crea, cuántos días adelante puede agendar visitas y cuántos mensajes manda antes de pasar a una persona. Con **Apagado por cola** marcas lo que no debe hacer en cada cola.
6. **Perfiles por horario:** para que haga una cosa de día y otra de noche o en fin de semana. El perfil se elige en las franjas de un [Horario](/manual/administracion/horarios).
7. **Categorías:** con categorías, el asistente clasifica cada mensaje nuevo y lo manda a la cola que toca.
8. **Reconexión temporal:** decide si el asistente puede pedir a Argos que reconecte a un cliente cortado mientras caja revisa su boleta.

## Qué significa cada estado

- **En pausa por el tope:** se gastó el tope del mes; vuelve el mes siguiente.
- **El de siempre:** el perfil que vale en todas las franjas donde no elijas otro.
- **Todavía no se usa:** el perfil no está elegido en ningún horario.

Lo que el asistente hace siempre: se presenta como asistente virtual, deja cada acción en el historial, si duda no actúa y deja la conversación en la cola, y cuando llega una persona, ella toma el control.

## Preguntas frecuentes

**¿Los mensajes salen de Iris?**
Sí: van al proveedor de IA que elijas. Con el [Servidor de IA](/manual/administracion/servidor-ia) propio se quedan en tu red.

## Permisos necesarios

- `mensajeria.asistente_configurar` (**Administrador**).
