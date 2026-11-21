# Conexión con Argos

## Para qué sirve

Ver lo que Iris sabe de su conexión con Argos: versión, empresas que ve, primera sincronización, avisos sin aplicar, acciones esperando y el certificado de seguridad.

## Cómo se usa

1. En [Integraciones](/manual/administracion/integraciones), abre la conexión con Argos.
2. Revisa **Conexión** (desde cuándo y qué versión), **Datos de Argos en Iris** y **Empresas que ve Iris**.
3. En **Primera sincronización** ves el avance. Si se detuvo, puedes lanzarla de nuevo: solo lee de Argos.
4. Pulsa **Revisar ahora** para releer la versión de Argos.
5. Revisa **Avisos de Argos sin aplicar** y **Acciones hacia Argos esperando**.

## Qué significa cada estado

- **Iris no está conectado con Argos:** conéctalo desde Integraciones. Mientras tanto, Iris trabaja con sus propios datos.
- **Sin empezar**, **Empezó al activar el conector** o **La lanzó un administrador:** el estado de la primera sincronización.
- **Argos no respondió:** inténtalo más tarde.
- **Certificado de Argos: Cambió:** el certificado del servidor de Argos es otro. Solo informa; no bloquea nada.
- **Con este Argos queda apagado:** una función de Iris necesita una versión más nueva de Argos.

## Preguntas frecuentes

**Una empresa nueva de Argos no aparece en Iris.**
Si la conexión es solo para algunas empresas, una nueva no se ve hasta reconectar.

## Permisos necesarios

- `integraciones.ver`. Revisar la versión: `integraciones.revisar_version`.
