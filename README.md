<p><img src="docs/marca/logo-mark.svg" width="56" alt="Iris"></p>

# Iris

Iris es el sistema de operación de campo y atención del ISP, de la familia Horus. Cubre:

- tickets y SLA;
- órdenes de trabajo (instalación, visita, retiro y cambio de equipo);
- app de campo para técnicos, que funciona sin conexión;
- agenda, rutas y despacho;
- seguimiento comercial.

Se integra con [ArgosISP](https://github.com/HCDestroyer/argos-isp), dueño de clientes, servicios, facturación e inventario, y con [Horus Flow](https://github.com/HCDestroyer/horus-flow), que aporta tráfico y alertas de red, mediante contratos escritos (`.claude/integration/contratos/`).

**Stack:**

| Parte | Tecnología |
| --- | --- |
| API | Go: chi, pgx, sqlc, River |
| Base de datos | PostgreSQL 16 |
| Panel y app de campo | Next.js 15 (PWA) |
| App nativa (próximamente) | Expo |

Las reglas están en `.claude/CLAUDE.md` y el plan en `.claude/PLAN.md`.
