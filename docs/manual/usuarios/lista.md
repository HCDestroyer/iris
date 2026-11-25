# Usuarios

## Para qué sirve

Ver y administrar a las personas que entran a Iris (al panel o a la app de campo) y a las integraciones que usan un token.

## Cómo se usa

1. Entra a **Usuarios** en el menú.
2. Revisa la lista: nombre, tipo, roles y estado.
3. Toca un usuario para abrir su ficha: [Ficha del usuario](/manual/usuarios/ficha).
4. Para dar de alta a alguien, pulsa **Nuevo usuario**: [Nuevo usuario](/manual/usuarios/formulario).
5. Al final, **Cargar más** trae los siguientes.

## Qué significa cada estado

- **Activo** o **Desactivado:** un usuario desactivado no puede entrar. Nunca se borra.
- **MFA activo** o **Sin MFA:** si usa verificación en dos pasos.
- **Integración Argos** o **Integración Horus:** no es una persona, es el acceso de otro sistema.
- **Sin roles:** no puede hacer nada hasta que le asignen uno.

## Preguntas frecuentes

**No veo a alguien.**
Solo ves los usuarios de las empresas en que operas.

## Permisos necesarios

- `usuarios.ver` (**Supervisor**, **Despacho**, **Soporte**, **Caja** y **Lectura**). Crear: `usuarios.crear`.
