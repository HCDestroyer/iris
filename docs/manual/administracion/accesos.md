# Accesos

## Para qué sirve

Ver quién entró a Iris, cuándo, por qué canal (panel, app de campo, desde Argos o con un token) y con qué resultado. Sirve para revisar intentos sospechosos.

## Cómo se usa

1. En **Administración**, abre **Accesos**.
2. Filtra por usuario, canal, resultado o fechas.
3. Para ver y cerrar las sesiones abiertas de una persona, abre su [Ficha del usuario](/manual/usuarios/ficha), pestaña **Sesiones**.

## Qué significa cada estado

Canal: **Panel**, **App de campo (PWA)**, **App de campo (Android)**, **Desde Argos (autologin)** o **Token de API**.

Resultado:

- **Entró:** el acceso fue correcto.
- **Contraseña errónea.**
- **Código MFA fallido:** la contraseña estaba bien, pero el código de verificación no.
- **Bloqueado por intentos:** demasiados intentos seguidos.
- **Usuario desactivado:** el usuario ya no puede entrar.
- **Sin usuario:** alguien intentó entrar con un correo que no existe.

## Preguntas frecuentes

**Veo muchos intentos fallidos de alguien.**
Revisa con la persona. Si no fue ella, restablece su contraseña desde su ficha.

## Permisos necesarios

- `accesos.ver` (**Administrador**).
