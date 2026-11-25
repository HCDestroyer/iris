# Buzones de correo

## Para qué sirve

Conectar los correos de soporte de la empresa. Los correos que llegan a estos buzones abren tickets o se suman a la conversación de un ticket, y las respuestas de los agentes salen por correo (SMTP).

## Cómo se usa

1. En **Administración**, abre **Buzones de correo**.
2. Pulsa **Nuevo buzón**.
3. Escribe la **Dirección** (remitente de las respuestas).
4. Llena los datos de entrada: **Servidor IMAP**, **Usuario IMAP** y **Contraseña IMAP**. Solo se aceptan conexiones cifradas.
5. Si quieres, indica una **Carpeta de procesados**: cada correo ya guardado se mueve ahí.
6. Llena los datos de salida (SMTP) para que salgan las respuestas.
7. En **Verificación del remitente**, escribe el authserv-id de tu servidor de correo.
8. Elige la **Categoría por defecto** y la **Prioridad por defecto** de los tickets que abre un correo nuevo.
9. Pulsa **Crear buzón**. Nace inactivo: pulsa **Probar conexión** y, si todo sale bien, actívalo.

## Qué significa cada estado

- **Activo:** Iris lo lee cada minuto.
- **Inactivo:** no se lee; los correos nuevos no crean tickets.
- **Verifica remitentes** o **Sin verificación de remitente:** sin authserv-id ningún remitente queda verificado, y las respuestas por correo entran como notas internas.
- **Último error de lectura:** lo que falló la última vez que Iris leyó el buzón.

## Preguntas frecuentes

**¿Por qué una respuesta del cliente quedó como nota interna?**
Solo el contacto del ticket, con el remitente verificado, entra como mensaje del cliente. Cualquier otro (copia, reenvío, remitente sin verificar) queda como nota interna.

**Dice «Iris no puede guardar contraseñas».**
Falta una clave de cifrado en el servidor. Avisa a quien administra Iris.

**Al editar, ¿tengo que volver a escribir la contraseña?**
Si cambias el servidor, el puerto, el cifrado o el usuario, sí. Si no, déjala vacía para conservar la guardada.

## Permisos necesarios

- Ver: `buzones.ver`. Crear, editar, activar y desactivar: `buzones.crear`, `buzones.editar`, `buzones.activar` y `buzones.desactivar`, con alcance global (**Supervisor**).
