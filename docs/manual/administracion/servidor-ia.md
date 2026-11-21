# Servidor de IA

## Para qué sirve

Ver y cuidar la computadora con tarjeta gráfica donde trabaja el asistente de IA: su estado, cuánto se usa, sus modelos y sus avisos. Si tu empresa usa el servidor de tu proveedor, aquí ves el uso de tu empresa.

## Cómo se usa

1. En **Administración**, abre **Servidor de IA**.
2. Revisa **Cifras de ahora**: uso de la tarjeta gráfica, memoria de video, peticiones en curso, memoria del equipo y disco de los modelos.
3. Pulsa **Probar ahora** para comprobar que responde.
4. Revisa las gráficas de uso, peticiones por minuto y tiempo de respuesta.
5. En **Modelos instalados** ves qué modelos tiene.
6. En **Avisos**, **Enlazar** y **Mantenimiento** se ajustan los umbrales, se enlaza un servidor nuevo y se reparte el uso entre empresas.

## Qué significa cada estado

- **El servidor de IA respondió bien** o **La prueba encontró un problema.**
- **El servidor está enlazado pero falta activarlo:** hasta activarlo, Iris no le manda nada. Un enlace sin activar caduca a las 24 horas.
- **Sin servidor de IA:** el asistente usa el proveedor elegido en sus ajustes.
- **La última lectura del servidor ya es vieja:** puede estar apagado o reiniciándose (tarda cerca de un minuto en volver).
- **No alcanzan los turnos para todas las empresas:** revisa Mantenimiento.

## Preguntas frecuentes

**Dice «No tienes permiso para ver esta sección».**
El servidor de IA lo administra quien ve todas las empresas de la instalación.

## Permisos necesarios

- Ver: `servidor_ia.ver`. Ver solo el uso de tu empresa: `servidor_ia.ver_uso`. Administrar: `servidor_ia.administrar`.
