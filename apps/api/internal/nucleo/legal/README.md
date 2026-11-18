# legal

Datos legales del titular de la instalación (quién opera Iris) para las páginas públicas de
privacidad, términos y eliminación de datos que Meta pide para revisar y publicar la app de WhatsApp.
Contrato: `.claude/integration/contratos/legal.md`.

- **Tabla:** `nucleo.datos_legales`, una fila por ámbito; hoy solo la de la instalación
  (`empresa_id` null). Todo lo que guarda es público: nunca secretos.
- **Casos de uso** (`datos_legales.go`):
    - `Ver` (`datos_legales.ver`): los datos, `completo` y `faltan[]`.
    - `Guardar` (`datos_legales.configurar`, sensible): crea o reemplaza con `version` (409). Guarda
      aunque falten datos. Si nada cambió, no sube la versión ni la fecha.
    - `Publico` (sin sesión): lo que leen las páginas públicas. No se audita. La ruta lo sirve con
      `LeerPublico`: una caché en memoria de 60 s que `Guardar` invalida al confirmar, para que un
      bucle de peticiones anónimas no llegue a la base.
- **Rutas** (`rutas.go`): `GET /api/v1/datos-legales`, `POST /api/v1/datos-legales/guardar` y
  `GET /api/v1/publico/datos-legales` (`Cache-Control: public, max-age=60`).
- **Alcance:** son de la instalación; exigen operar sin límite de empresa (`empresas.todas`, o una
  instalación sin empresas). Solo personas.
- **Emite:** `datos_legales.cambiados` (`campos[]`, `completo`, `faltan[]`; nunca los valores).
- **Consume:** nada.

Pruebas: `IRIS_DB=iris_test_legal go test ./internal/nucleo/legal/`.
