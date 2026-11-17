Versión: 1.0.0 (17 nov 2026) · Páginas legales públicas y datos legales del titular

# Legal: páginas públicas que pide Meta

Para pasar la app de Meta a modo público (revisión de la app y verificación del negocio), Meta pide
URL públicas de **política de privacidad**, **términos del servicio** y **eliminación de datos**.
Iris las sirve desde el dominio de la instalación, en español y en inglés, con los datos de quien la
opera («el titular»), que se configuran en el panel. Nada del titular va en el código ni en el `.env`.

Estado: **implementado** (API en `internal/nucleo/legal`, páginas y pantalla en `apps/web`).

## 1. Qué pide Meta (verificado el 17 nov 2026)

| Campo en Meta (Configuración de la app › Básica) | Para pasar a modo activo | Qué se pega |
| --- | --- | --- |
| URL de la Política de privacidad | La exigen las Condiciones de la plataforma y la revisión de la app | `/en/privacy` |
| URL de las Condiciones del servicio | Obligatoria | `/en/terms` |
| Eliminación de datos de usuarios | URL de instrucciones **o** URL de devolución de llamada | `/en/data-deletion` (instrucciones) |

- La política debe estar en una URL activa, pública, sin bloqueo geográfico, accesible para los
  rastreadores de Meta, marcada con claridad como política de privacidad, propia (no la de otra
  empresa) y debe decir qué datos se recogen, para qué y cómo pedir su eliminación. Un enlace roto
  es una infracción. Meta recomienda comprobarla con su depurador de enlaces (respuesta 200).
- **No se usa la devolución de llamada con `signed_request`.** Esa llamada identifica a la persona
  con un identificador de usuario de la app, que solo existe cuando alguien entra con Facebook Login.
  Iris no usa Facebook Login: a sus clientes los conoce por su número de WhatsApp. Por eso se da la
  URL de instrucciones, que Meta acepta como alternativa.
- Además Meta pide, fuera de Iris: icono de la app, categoría, correo de contacto, verificación del
  negocio (nombre legal, dirección, teléfono, correo y sitio web) y, para ser proveedor de tecnología,
  la revisión de la app con los permisos `whatsapp_business_messaging` y `whatsapp_business_management`.

Fuentes: `developers.facebook.com/docs/development/create-an-app/app-dashboard/basic-settings`,
`…/app-dashboard/data-deletion-callback`, `developers.facebook.com/docs/development/release`,
`developers.facebook.com/documentation/development/terms-and-policies/privacy-policy`,
`developers.facebook.com/terms/dfc_platform_terms` y
`developers.facebook.com/docs/whatsapp/solution-providers/get-started-for-tech-providers`.

## 2. Rutas públicas (estables: no cambiarlas)

| Documento | Español | Inglés |
| --- | --- | --- |
| Política de privacidad | `/privacidad` | `/en/privacy` |
| Términos del servicio | `/terminos` | `/en/terms` |
| Eliminación de datos | `/eliminacion-de-datos` | `/en/data-deletion` |

- Sin sesión: el middleware del panel no las toca. `robots.txt` las permite y cierra todo lo demás.
- Se arman en el servidor: el texto va en el HTML (se leen con JavaScript apagado).
- Cada una enlaza a su otro idioma y a las otras dos. El pie del inicio de sesión y del portal de
  soporte enlaza a las tres.
- El `<title>` y el encabezado nombran al titular. «Última actualización» es lo más reciente entre
  la fecha del texto (`TEXTO_REVISADO_EN`, en `apps/web/lib/legal/titular.ts`) y el último cambio de
  los datos.
- Sin datos, o si la API no responde, la página responde 200 igual, con un nombre genérico («el
  proveedor de este servicio») y sin líneas de contacto vacías: nunca un hueco ni un error.
- El HTML lleva `data-pagina-legal="<privacidad|terminos|eliminacion>"`: con esa marca el panel
  comprueba que la URL sirve la página y no otra cosa.

## 3. Datos del titular

Tabla `nucleo.datos_legales`. Hoy una sola fila, la de la instalación (`empresa_id` null): las
páginas se sirven por dominio y Iris no resuelve la empresa por dominio. La columna `empresa_id`
queda lista para cuando lo haga.

| Campo | Tipo | Notas |
| --- | --- | --- |
| `razonSocial` | texto, 0–200 | Obligatorio para estar listo. El responsable de los datos. |
| `nombreComercial` | texto, 0–120 | Obligatorio. Debe coincidir con el nombre usado en Meta. |
| `correoPrivacidad` | correo, 0–254 | Obligatorio. Una sola dirección. |
| `telefono` | texto, 0–40 | Opcional. Dígitos, espacios, `+`, guiones y paréntesis. |
| `direccion` | texto, 0–300 | Obligatorio. Una línea. |
| `pais` | texto, 0–80 | Obligatorio. Sus leyes son la ley aplicable de los términos. |
| `version` | entero | Para el 409. |

Todo puede guardarse vacío: se guarda lo que haya y `faltan` dice qué falta. Todo lo que hay en
esta tabla es público por definición.

## 4. Endpoints (`/api/v1`)

| Método y ruta | Permiso | Qué hace |
| --- | --- | --- |
| `GET /datos-legales` | `datos_legales.ver` | Los datos, `completo` y `faltan[]`. Sin datos: vacíos, `configurado: false`, `version: 0`. |
| `POST /datos-legales/guardar` | `datos_legales.configurar` | Crea o reemplaza. `version` (0 la primera vez) e `Idempotency-Key`. |
| `GET /publico/datos-legales` | ninguno (sin sesión) | Lo que publican las páginas. `Cache-Control: public, max-age=60`. |

```json
{
  "configurado": true,
  "razonSocial": "Redes del Sur, Sociedad Anónima",
  "nombreComercial": "RedSur",
  "correoPrivacidad": "privacidad@redsur.example",
  "telefono": "+502 2222 3333",
  "direccion": "5a avenida 1-23, zona 1, Ciudad de Guatemala",
  "pais": "Guatemala",
  "version": 2,
  "actualizadoEn": "2026-11-17T18:00:00Z",
  "completo": true,
  "faltan": []
}
```

- `faltan`: los obligatorios vacíos, en el orden del formulario (`razonSocial`, `nombreComercial`,
  `correoPrivacidad`, `direccion`, `pais`). `completo` = `faltan` vacío.
- `guardar`: 409 `conflicto_version`; 422 `validacion` con `errors[].field`. Si nada cambió no sube
  la versión ni la fecha y no emite evento.
- Público: `{ nombreComercial, razonSocial, correo, telefono, direccion, pais, actualizadoEn, completo }`.
  Sin datos responde 200 con cadenas vacías, `actualizadoEn: null` y `completo: false`.

**Seguridad.** Los dos permisos de fábrica solo los tiene `admin`. `configurar` es sensible (MFA y
re-autenticación en una sesión abierta por el autologin): quien lo cambia decide a qué correo
escriben los clientes para ejercer sus derechos. Son datos de la instalación: exigen operar sin
límite de empresa (`empresas.todas`, o una instalación sin empresas); si no, 403. Solo personas.
La lectura pública no se audita (la repiten los rastreadores) y no tiene límite de peticiones propio
(el repositorio no tiene uno general): la protegen la caché de un minuto de la respuesta y la del
servidor web, que es quien la consulta.

## 5. Evento (outbox)

| Evento | Payload | Cuándo |
| --- | --- | --- |
| `datos_legales.cambiados` | `campos[]` (snake_case), `completo`, `faltan[]` | Guardar con algún cambio. Nunca los valores. |

## 6. De dónde sale el texto

El texto (`apps/web/lib/legal/contenido.ts`) es un **texto base, no asesoría legal**: cada titular
debe hacerlo revisar. Se escribió a partir de lo que Iris hace:

| Afirmación | Dónde se verifica |
| --- | --- |
| Baja con `BAJA` o `ALTO` | `mensajeria/entrantes.go` (`palabrasBaja`), `argosisp.md` |
| El asistente se presenta como virtual, dice la verdad si le preguntan y pasa a una persona si se lo piden | `mensajeria/asistente_auto.go` (instrucciones; «pasar a una persona» no se puede apagar) |
| El proveedor de IA lo elige la empresa (propio o de terceros) | `conversaciones.md` §16 |
| Grabación y transcripción apagadas de fábrica, con anuncio, 30 y 90 días, transcripción redactada | `voz.md` §7 |
| Permiso previo para llamar por WhatsApp | `voz.md` (solicitud de permiso) |
| El asistente no aprueba pagos; la boleta la revisa caja | `mensajeria/asistente_auto.go`, `conversaciones.md` (boletas) |
| Contraseñas con argon2id, credenciales cifradas, auditoría, usuarios propios con permisos | `nucleo/identidad`, `auditoria` |
| Los datos del servicio vienen de ArgosISP; alertas de Horus | `argosisp.md`, `horus.md` |

**Sin respaldo en el código** (compromisos del titular, a confirmar con su asesor): los plazos de
respuesta (5 días hábiles de acuse y 30 días), la conservación «por los plazos que exige la ley», el
aviso de incidentes, la cláusula de menores, la de proveedores de alojamiento y el uso de HTTPS (lo
da el despliegue). **Iris no tiene hoy una acción para borrar las conversaciones de un contacto**:
la eliminación que describe la página la hace una persona del equipo con quien administra el
servidor, hasta que exista esa acción.

## 7. Para el panel

Administración › **Datos legales** (`/administracion/datos-legales`): formulario en pantalla propia.
Arriba dice si las páginas están listas o «Faltan datos legales: las páginas públicas no están
listas para Meta», con la lista. Abajo, «Enlaces para Meta»: las seis URL con el dominio del panel,
«Copiar», «Abrir», dónde se pega cada una y si responde (se pide sin cookies, como la abre Meta).
Sin `https` avisa que Meta no aceptará el enlace.
