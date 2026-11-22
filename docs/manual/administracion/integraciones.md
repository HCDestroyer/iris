# Integraciones

## Para qué sirve

Conectar Iris con ArgosISP (clientes, servicios, facturación e inventario) y con Horus Flow (alertas de red). Iris funciona completo sin ellos: si un conector está apagado, trabaja con sus propios datos.

## Cómo se usa

1. En **Administración**, abre **Integraciones**.
2. Para Argos, escribe la **Dirección del panel de Argos** y pulsa **Conectar con Argos**. Iris te lleva a Argos para que apruebes la conexión y vuelve aquí.
3. El conector queda apagado hasta que pulses **Activar**. Al activarlo empieza la primera sincronización.
4. Para Horus Flow (o Argos como opción avanzada), usa **Conectar con otro sistema**: pega el enlace de conexión que generó ese sistema (vale 15 minutos y sirve una vez) y pulsa **Revisar enlace**.
5. Para apagar un conector, pulsa **Desactivar**. Los conectores apagados se pueden eliminar.
6. Para ver el detalle de Argos, abre [Conexión con Argos](/manual/administracion/argos).

## Qué significa cada estado

- **Activo:** Iris usa este conector. Puede haber como mucho uno activo por sistema.
- **Apagado:** no se usa.
- **Sin conectores:** Iris trabaja solo con sus datos.

## Preguntas frecuentes

**Creo que se filtró el acceso de un conector.**
Al activar uno nuevo, marca **Cortar ya el acceso anterior**: su token se revoca en el mismo paso.

**¿Eliminar un conector borra lo que hizo?**
No. Lo que hizo sigue en la auditoría.

## Permisos necesarios

- Ver: `integraciones.ver` (**Supervisor**). Activar y desactivar: `integraciones.activar` e `integraciones.desactivar` (**Administrador**).
