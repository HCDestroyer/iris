# I4 · App de campo

**Carpetas:** `apps/web/app/campo`, `packages/sync` y `apps/movil` (fase 2).

## Tareas

1. **`packages/sync`:**
    - Cola local en IndexedDB (Dexie) con operaciones de `contratos/sync.md`, ids UUID v7 y reintentos con espera.
    - Aplicación optimista sobre el estado local.
    - Manejo de rechazos visible.
    - Paquete del día y cambios por cursor.
    - Sin dependencias del DOM fuera de un adaptador, para reutilizarlo en Expo.
2. **PWA `/campo`:**
    - Manifest con el logo de Iris y service worker (Serwist) que cachea el shell.
    - Mis órdenes del día en orden de ruta y detalle.
    - «En camino» y «Llegué» con GPS.
    - Checklist, fotos (compresión y subida reanudable en segundo plano) y firma del cliente.
    - Escaneo de serie/MAC por cámara (BarcodeDetector con respaldo en zxing).
    - Materiales desde las existencias de su bodega.
    - Cierre o falla con motivo.
    - Indicador claro de «sin conexión / pendientes por enviar».
3. **Ubicación en vivo:** cada 60 s mientras la app está abierta y en turno, con envío en lote.
4. **Fase 2:** `apps/movil` con Expo, reutilizando `packages/sync` y `packages/contratos`, con GPS en segundo plano y cámara nativa.

**Pruebas:** Vitest del motor de sync (con conflictos de §3) y Playwright en modo sin conexión (`context.setOffline`), en teléfono 390 px.
