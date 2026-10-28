# Payloads de eventos de Iris

Todo evento sale por `integracion.outbox` con esta forma: `{ id (uuid v7), evento, ocurrido_en, empresa_id, payload }`. Cada agente agrega aquí el detalle de sus eventos, en la misma PR que el código que los emite. El catálogo resumido está en `.claude/CLAUDE.md`.
