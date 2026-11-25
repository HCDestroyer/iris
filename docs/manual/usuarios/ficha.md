# Ficha del usuario

## Para qué sirve

Ver y cambiar el acceso de una persona: sus datos, roles, empresas, sesiones abiertas y su historial de accesos. También resolver problemas de entrada (contraseña olvidada o teléfono perdido).

## Cómo se usa

1. Abre un usuario desde [Usuarios](/manual/usuarios/lista).
2. Revisa las pestañas **Datos**, **Roles**, **Empresas**, **Sesiones** y **Accesos**.
3. **Roles:** agrega o quita roles. Cada rol puede valer en todas las zonas, en una zona o en una cuadrilla.
4. **Empresas:** usa **Asignar empresa** o quita una. Solo puedes dar las empresas en que operas tú.
5. **Editar:** cambia el nombre o el correo. Cambiar el correo cierra sus sesiones y su autologin.
6. **Restablecer contraseña:** para quien la olvidó. Iris crea una contraseña temporal, cierra sus sesiones y le pide cambiarla al entrar. Entrégala en persona o por teléfono, nunca por correo ni chat.
7. **Reiniciar MFA:** para quien perdió su teléfono. Borra su verificación en dos pasos y le da una contraseña temporal. Puedes revocar también sus tokens de API.
8. **Desactivar usuario:** deja de poder entrar, se cierran sus sesiones y sus tokens dejan de valer. No se borra.
9. Algunas acciones sensibles te piden **Confirma tu contraseña** (vale 15 minutos).

## Qué significa cada estado

- **MFA activo** o **Sin MFA**.
- **Cuentas vinculadas:** cuentas de Argos con que entra a Iris sin contraseña («Ver en Iris» desde Argos).
- **Es el último administrador activo:** Iris no deja desactivarlo; primero da el rol de administrador a otra persona.

## Preguntas frecuentes

**La contraseña temporal ya no se ve.**
Se muestra una sola vez. Si la perdiste, restablécela de nuevo.

**No puedo reiniciar mi propio MFA ni desactivarme.**
Pídeselo a otro administrador.

**Dice «Tiene permisos o empresas que tú no administras».**
Pídeselo a un administrador global.

## Permisos necesarios

- `usuarios.ver`, `usuarios.editar`, `usuarios.asignar_rol`, `usuarios.quitar_rol`, `usuarios.asignar_empresa`, `usuarios.quitar_empresa`, `usuarios.restablecer_contrasena`, `usuarios.reiniciar_mfa`, `usuarios.cerrar_sesiones` y `usuarios.desactivar`.
- En los roles base, solo el **Administrador** los tiene todos.
