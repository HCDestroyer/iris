# Payloads de eventos de Iris

Todo evento sale por `integracion.outbox` con esta forma: `{ id (uuid v7), evento, ocurrido_en, empresa_id, payload }`. Cada agente agrega aquí el detalle de sus eventos, en la misma PR que el código que los emite. El catálogo resumido está en `.claude/CLAUDE.md`.

## Identidad (núcleo, I1)

Emite el módulo `identidad`, con `empresa_id = null` (los usuarios de Iris no pertenecen a una empresa).

| Evento | Payload | Cuándo |
| --- | --- | --- |
| `usuario.creado` | `usuario_id`, `tipo` | Alta de un usuario (`usuarios.crear`, también desde `iris admin crear`). |
| `usuario.actualizado` | `usuario_id`, `campos[]` (nombres de los campos que cambiaron) | `usuarios.editar`. |
| `usuario.desactivado` | `usuario_id`, `motivo` | `usuarios.desactivar`. Revoca sus sesiones; sus tokens dejan de autenticar mientras esté inactivo, sin revocarse ni emitir `token.revocado`. |
| `token.creado` | `token_id`, `usuario_id` | Alta de un token `iris_pat_`. |
| `token.revocado` | `token_id`, `usuario_id`, `motivo` | `tokens.revocar`. |
