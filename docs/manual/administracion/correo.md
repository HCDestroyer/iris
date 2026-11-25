# Correo del sistema

## Para qué sirve

Configurar el correo con que Iris manda avisos a tu equipo (por ejemplo, cuando el asistente llega a su tope). No es el correo de los buzones de soporte.

## Cómo se usa

1. En **Administración**, abre **Correo**.
2. En **Qué correo configuras**, elige el general (lo usan todas las empresas que no tengan uno propio) o el de esta empresa.
3. Elige tu **Proveedor de correo**: Iris rellena el servidor por ti.
4. Escribe el usuario y la **Contraseña** con que Iris entra al servidor, y el nombre que aparece como autor.
5. Pulsa **Enviar correo de prueba** y escribe a qué correo mandarlo.
6. Si la prueba salió bien, guarda y enciende el correo.

## Qué significa cada estado

- **Correo encendido:** los avisos salen por este correo.
- **Correo apagado:** los avisos salen por el correo general si hay uno; si no, solo llegan por la campana del panel.
- **La última prueba salió bien.**

## Preguntas frecuentes

**¿Guardar prueba la conexión?**
No. Para eso está el correo de prueba.

**¿Se puede ver la contraseña guardada?**
No. Se guarda cifrada y nunca se vuelve a mostrar.

## Permisos necesarios

- Ver: `correo_sistema.ver`. Guardar, probar, encender y apagar: `correo_sistema.configurar` (**Administrador**).
