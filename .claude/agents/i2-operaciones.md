# I2 · Operaciones (soporte, campo, agenda, comercial)

**Carpetas:** `apps/api/internal/{soporte,campo,agenda,comercial}` y sus migraciones.

Usa la base de casos de uso, auditoría, outbox y conectores de I1. No escribe en Argos directamente: usa `integracion.Pendiente(...)` con la tabla de §3 de `contratos/argosisp.md`.

## Tareas

1. **Soporte:**
    - Tickets: acciones `crear`, `asignar`, `responder`, `nota_interna`, `escalar`, `poner_en_espera`, `resolver`, `reabrir`, `cerrar` y `fusionar`.
    - Categorías y SLA con calendario y feriados de Guatemala; cron de SLA (`sla_en_riesgo` al 80 %, `sla_vencido`).
    - Ticket maestro de incidente de red (webhook de Horus).
    - Entrada por correo (IMAP) y por portal.
2. **Campo:**
    - Tipos de orden y plantillas de checklist.
    - Máquina de estados de la orden.
    - `GET /campo/paquete`, `GET /campo/cambios` y `POST /sync/lote` según `contratos/sync.md`.
    - Cierre con validación de adjuntos obligatorios.
    - Pendientes hacia Argos al cerrar: activar, comodato, consumo, devolución y cargo.
    - Órdenes automáticas desde webhooks de Argos (equipo por recuperar, retiro).
3. **Agenda:**
    - Técnicos, cuadrillas, turnos, ausencias y capacidad.
    - Citas con ventana.
    - Propuesta de ruta con OSRM (`/trip` y matriz `/table`, con heurística de ventanas) y publicación.
    - `posiciones` (ingesta en lote, sin auditoría por fila).
4. **Comercial:**
    - Prospectos y actividades.
    - Factibilidad por la distancia al router más cercano de la instantánea, detrás de una interfaz que la fase 6 (OZmap) reemplaza. El pedido A5 a Argos quedó reemplazado.
    - Cotización con PDF y contrato con firma.
    - `ganar` → pendientes de alta de cliente y servicio en Argos, y luego la orden de instalación.

**Pruebas:** Go con Postgres real (`IRIS_DB=iris_test_i2`), con un servidor falso de Argos (`httptest`) que cumple el contrato.
