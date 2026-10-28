# Configuración de buzones de soporte (correo → tickets)

Un buzón conecta Iris a una cuenta de correo: el worker lee la bandeja por **IMAP** y crea o
responde tickets; las respuestas públicas de los agentes salen por el **SMTP** del buzón. Se
administra en el panel (Administración → Buzones; permisos `buzones.*`) o por la API
(`/api/v1/buzones`).

## Requisitos del entorno

- `IRIS_CLAVE_CIFRADO` en API y worker (cifra las contraseñas; sin ella, 422
  `cifrado_no_configurado`).
- Solo TLS con certificado verificado: `tls` (implícito, 993/465) o `starttls` (143/587). Sin TLS o
  con un certificado no válido, Iris no manda la contraseña (`sin_tls`). Para una CA privada,
  `IRIS_CORREO_CA` con su PEM.
- Protección SSRF: un servidor en IP interna (privada, loopback, CGNAT…) solo se acepta si su red está
  en `IRIS_CORREO_REDES_PERMITIDAS` (vacía en producción).

## Datos del buzón

| Campo | Qué poner |
| --- | --- |
| `nombre`, `correo` | nombre interno y la dirección del buzón (`soporte@isp.gt`) |
| `imap` | `host`, `puerto`, `tls`, `usuario`, `contrasena`, `carpeta` (INBOX) y `carpetaProcesados` opcional |
| `smtp` | `host`, `puerto`, `tls`, `usuario`/`contrasena`. Sin SMTP, las respuestas no salen por correo |
| `categoriaId`, `prioridad` | con qué categoría y prioridad nacen los tickets |
| `authservId` | ver abajo: **sin él, ninguna respuesta por correo cambia un ticket** |

El buzón nace **inactivo**: «Probar» (`POST /buzones/{id}/probar`) prueba IMAP y SMTP por separado,
y «Activar» lo pone a leer.

## `authservId`: verificación del remitente

Iris solo cree que un correo es del cliente si el servidor que lo **recibió** lo autenticó (DMARC, o
DKIM alineado) y lo dejó escrito en la cabecera `Authentication-Results` (RFC 8601). Esa cabecera
empieza con el *authserv-id* del servidor, por ejemplo:

```
Authentication-Results: mx.isp.gt; dkim=pass header.d=cliente.com; dmarc=pass header.from=cliente.com
```

- `authservId` es ese primer valor (`mx.isp.gt`). Búscalo en las cabeceras de un correo real
  recibido en el buzón (ver original / mostrar cabeceras). En Gmail/Google Workspace es
  `mx.google.com`; en Microsoft 365, el de `Authentication-Results` que pone Exchange Online.
- Solo cuenta el primer bloque de cabeceras con ese id, desde arriba. El servidor de correo debe
  **quitar** las `Authentication-Results` con su id que vengan de fuera (RFC 8601 §5); si no lo
  hace, un remitente podría falsificarla. Confírmalo con el administrador del correo.
- Sin `authservId` (o con uno equivocado) nada queda verificado: los correos nuevos crean ticket con
  el contacto «sin verificar» y las respuestas quedan como **nota interna**, sin cambiar el estado.
- SPF solo no basta.

## Desarrollo y lab

- **Mailpit** (compose) exige STARTTLS en `:1025` con la CA de `infra/mailpit/certs/ca.pem`, que el
  compose monta como `IRIS_CORREO_CA`. Buzón de prueba: `smtp { host: "mailpit", puerto: 1025, tls:
  "starttls", usuario: "x", contrasena: "x" }`. Los correos salientes se ven en
  `http://localhost:8025`.
- Mailpit **no tiene IMAP**: «Probar» da SMTP correcto e IMAP `conexion`. Para probar la entrada,
  un servidor IMAP con TLS de lab (Dovecot, GreenMail) con su CA en `IRIS_CORREO_CA` y su red en
  `IRIS_CORREO_REDES_PERMITIDAS`; los correos de prueba no traen `Authentication-Results` de un
  servidor real, así que entran sin verificar salvo que el IMAP de lab los agregue.

## Problemas comunes

| Síntoma | Causa |
| --- | --- |
| 422 `destino_no_permitido` | IP interna fuera de `IRIS_CORREO_REDES_PERMITIDAS` |
| Probar: `sin_tls` | el servidor no anuncia STARTTLS o se eligió `starttls` en un puerto de TLS implícito (o al revés) |
| Probar: `tls` | certificado no válido: CA privada sin `IRIS_CORREO_CA`, o el host no coincide con el certificado |
| Probar: `dns` / `conexion` | el host no resuelve, o el puerto está cerrado o filtrado |
| Probar: `autenticacion` | usuario/contraseña; en Microsoft 365 o Gmail puede hacer falta contraseña de aplicación |
| Las respuestas quedan como notas internas | falta `authservId` o no coincide con la cabecera |
| Tras rotar `IRIS_CLAVE_CIFRADO` falla todo | reescribe las contraseñas ([rotación](rotar-clave-cifrado.md)) |
