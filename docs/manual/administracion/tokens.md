# Tokens de API

## Para qué sirve

Ver los accesos de integraciones y scripts, y quitar los que ya no se usan.

## Cómo se usa

1. En **Administración**, abre **Tokens de API**.
2. Marca **Mostrar revocados y vencidos** si quieres ver todos.
3. Para que un token deje de valer, pulsa **Revocar**: queda en la lista como revocado.
4. Para quitarlo de la lista, pulsa **Eliminar** (o **Revocar y eliminar**).

## Qué significa cada estado

- **Vigente:** funciona.
- **Revocado:** ya no vale; quien lo use recibe un error.
- **Vencido:** pasó su fecha.
- **Sin usar:** nunca se ha usado.

## Preguntas frecuentes

**¿Eliminar borra lo que hizo el token?**
No. Lo que hizo sigue en la auditoría. Eliminar no se puede deshacer.

**No puedo eliminar un token.**
Los que usa un conector activo se quedan.

## Permisos necesarios

- Ver: `tokens.ver` (**Supervisor**). Revocar: `tokens.revocar`. Eliminar: `tokens.eliminar` (**Administrador**).
