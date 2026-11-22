# Roles y permisos

## Para qué sirve

Ver qué puede hacer cada rol y crear roles propios. Un permiso es el nombre de una acción (por ejemplo `tickets.crear`); un rol agrupa permisos.

## Cómo se usa

1. En **Administración**, abre **Roles y permisos**.
2. Abre un rol para ver sus permisos, agrupados por módulo.
3. Para un rol a medida, pulsa **Nuevo rol**, ponle nombre, marca sus permisos y pulsa **Crear rol**.
4. Para cambiar un rol propio, ábrelo, cambia sus permisos y pulsa **Guardar cambios**.
5. Para dejar de usarlo, pulsa **Desactivar rol**. Para volver a usarlo, **Activar rol**.
6. Los roles se asignan a cada persona desde su [Ficha del usuario](/manual/usuarios/ficha).

## Qué significa cada estado

- **Rol base:** lo mantiene Iris (Administrador, Supervisor, Despacho, Técnico, Soporte, Ventas, Lectura y Caja). No se edita.
- **Rol desactivado:** deja de dar permisos a quienes lo tienen; las asignaciones se conservan.

## Preguntas frecuentes

**¿Se puede borrar un rol?**
No. Los roles propios se desactivan, nunca se borran.

**Dar permisos de administración me pide la contraseña.**
Es una acción sensible: confirma tu contraseña (vale 15 minutos).

## Permisos necesarios

- Ver: `roles.ver` (**Supervisor** y **Lectura**). Crear, editar, activar y desactivar: `roles.crear`, `roles.editar`, `roles.activar` y `roles.desactivar` (**Administrador**).
