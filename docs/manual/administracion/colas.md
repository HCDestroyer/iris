# Colas

## Para qué sirve

Una cola reúne a las personas que atienden las conversaciones de uno o varios números de WhatsApp. Aquí defines quién atiende, quién supervisa y cuánto espera el bot antes de contestar.

## Cómo se usa

1. En **Administración**, abre **Colas**.
2. Pulsa **Nueva cola** o abre una para editarla.
3. Escribe el nombre y, si quieres, una descripción.
4. En **Miembros**, usa **Agregar persona…**: reciben las conversaciones nuevas de la cola.
5. En **Supervisores**, usa **Agregar supervisor…**: ven, asignan y pasan todo lo de la cola, aunque no sea suyo.
6. Revisa los **Números de WhatsApp** que llegan a la cola. La cola de cada número se elige en la configuración del número.
7. Ajusta los tiempos (si los dejas vacíos, valen los del número):
    - **Tiempo de espera de respuesta (min):** pasado este tiempo sin responder, el punto de la conversación se pone naranja.
    - **Minutos sin confirmación antes de que conteste el bot:** en horario, si nadie dice «La atiendo», contesta el bot.
    - **Minutos sin agentes disponibles:** si no hay nadie disponible, el bot toma la conversación pasado este tiempo.
8. Pulsa **Crear cola** o **Guardar cambios**.

## Qué significa cada estado

- **Ningún número llega aquí:** la cola no recibe conversaciones.
- **Nadie disponible ahora:** ningún miembro está en **Disponible**.
- **Sin tiempo de respuesta:** el punto no cambia de color por espera.
- El punto verde junto a cada persona dice quién está disponible ahora.

## Preguntas frecuentes

**¿Un supervisor tiene que ser miembro?**
No. Puede supervisar sin recibir conversaciones.

## Permisos necesarios

- `canales.configurar` (**Supervisor**).
