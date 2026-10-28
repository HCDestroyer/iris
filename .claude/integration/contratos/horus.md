Versión: 0.1.0 (borrador) · Iris ↔ Horus Flow

# Contrato de integración Iris ↔ Horus Flow

## Gobierno

- **Copias espejo:** este archivo vive igual en `HCDestroyer/iris` (`.claude/integration/contratos/horus.md`) y en `HCDestroyer/horus-flow` (`docs/integrations/iris.md`).
- **Mismas reglas que Argos ↔ Horus:** PRs enlazadas, versionado con semver y cada lado toca solo su código.
- **Mecanismos reutilizados:** los del contrato Argos ↔ Horus §1, es decir, tokens, webhooks firmados, enlace de conexión y autologin por ticket. Los prefijos son `/api/v1/integrations/iris` en Horus y `/api/v1/integraciones/horus` en Iris.

## 0. Principios

- **Independencia:** sin el conector, Iris y Horus funcionan igual.
- **Identidad del cliente:** la resuelve Argos. Horus manda IPs y su propio contexto (router, hallazgo), e Iris pregunta a Argos de quién es esa IP.
- **Sin duplicados:** una caída que afecta a muchos clientes abre **un** ticket maestro con los clientes afectados como hijos, no uno por cliente.

## 1. Tokens

### 1.1 Iris → Horus

Token `hf_pat_…` con `traffic.customer.read`, `security.findings.read`, `devices.read` y `sso.issue`.

Iris lo usa para mostrar, en el ticket y en la orden, el consumo y la salud del servicio del cliente (§3.1 del contrato Argos ↔ Horus, llamado con `servicio_id` de Argos) y el estado del router.

### 1.2 Horus → Iris

Token `iris_pat_…` con `tickets.crear`, `tickets.ver` y `sso.emitir`.

## 2. Webhooks Horus → Iris

`POST https://<iris>/api/v1/integraciones/horus/webhook/{conector_id}`, firmado según §1.3 del contrato Argos ↔ Horus.

| Evento | Efecto en Iris |
| --- | --- |
| `horus.router_estado_cambiado` con `estado = caido` | Abre o reabre un ticket maestro `incidente_red` (prioridad alta) con el router. Los tickets nuevos de clientes de ese router se vinculan como hijos. Con `estado = arriba`, propone resolver el maestro y notificar a los hijos. |
| `horus.servicio_infra_cambiado` | Igual que el router, para servicios de infraestructura (OLT, enlaces). |
| `horus.hallazgo_abierto` | Si la categoría está configurada para abrir tickets (por ejemplo, equipo infectado o CPE con firmware vulnerable), abre un ticket del cliente (IP → Argos) con el hallazgo y el enlace «Ver en Horus». |
| `horus.hallazgo_resuelto` | Agrega una nota al ticket vinculado y propone cerrarlo. |

## 3. Iris → Horus

- **Autologin:** «Ver en Horus» desde el ticket o la orden, hacia `router` y `hallazgo` (§1.5 del contrato Argos ↔ Horus).
- **Al cerrar una orden de instalación o cambio de equipo:** webhook `orden.cerrada` con `servicio_id`, `router_id` y la `mac` del CPE, para que Horus asocie el equipo nuevo (pedido H1).

## 4. Solicitudes

### 4.1 Iris solicita a Horus

| # | Pedido | Estado |
| --- | --- | --- |
| H1 | Recibir `orden.cerrada` (instalación o cambio de equipo) para asociar la MAC del CPE al servicio. Es opcional. | solicitado |
| H2 | Conector Iris en Horus: token, enlace de conexión, webhooks de §2 hacia Iris y autologin desde Iris. | solicitado |

### 4.2 Horus solicita a Iris

(vacía)
