# integracion/inventario

Inventario del técnico en la app de campo (contrato Iris ↔ ArgosISP 0.5.0 §3.3, Argos ≥ 1.6.0),
sobre el vínculo de su ficha de agenda con su usuario de Argos (`agenda.tecnicos.argos_id_externo`).

## Qué hace

- `GET /api/v1/campo/inventario/mi-bodega` (`campo.ver_bodega`): `GET /inventario/mi-bodega` de Argos
  con el vínculo y la empresa activa (`X-Empresa-Id`), con caché de 1 min por técnico y empresa.
  Existencias con modalidad, unidades, ids de traslados pendientes y los últimos 50 movimientos.
- `GET /api/v1/campo/inventario/traslados?estado=` (`campo.ver_bodega`): las órdenes de traslado hacia
  su bodega (modelo de lectura `integracion.argos_traslados`, de los webhooks) con el estado de su
  recepción.
- `traslado.recibir` del lote (`POST /sync/lote`, `campo.recibir_traslado`): `Encolar` valida vínculo,
  versión, el traslado según Iris, la firma y los datos, y encola el pendiente `recepcion`
  (`iris:recepcion:<traslado_id>`, entidad `traslado`, `X-Empresa-Id` al enviar) **bloqueado con
  `esperando_firma`**. `ArmarFirma` (job `integracion.recepcion_armar_firma`, suscriptor de
  `adjunto.completado` / `adjunto.rechazado` y barrido cada 5 min) lee el PNG del almacenamiento,
  verifica ≤ 200 KB y 50×20–1000×400 px y fija el cuerpo `{ recibido_por, metodo_recepcion:
  firma_digital, firma: { png_base64, sha256 }, recibido_en, lineas? }`; si no sirve, el pendiente
  queda `fallido_negocio` con `firma_invalida` sin llamar a Argos. Un `409 traslado_ya_recibido` es
  hecho (cola de pendientes).
- `Recepciones`: el estado para `GET /campo/cambios` (`recepciones[]`).
- La firma es un adjunto de la entidad `traslado` (`RegistrarAdjuntos`): la sube el técnico al que va
  la orden de traslado (o cualquier técnico vinculado si el webhook aún no llegó).

## Eventos

No emite eventos propios. La cola de pendientes emite `integracion.pendiente_*` de la operación
`recepcion` (entidad `traslado`); `integracion.pendiente_fallido` también para `firma_invalida` local.
Consume `adjunto.completado` y `adjunto.rechazado`.

## Seguridad de la recepción (revisión de la PR #83)

- Solo se encola la recepción de un traslado que Iris conoce (webhook) y que va al técnico que la
  manda. Desconocido o sin técnico: `traslado_desconocido` (409, reintentable; no toma la clave
  `iris:recepcion:<id>`). La firma de esos traslados no se registra ni se ve (404).
- El destinatario real toma la recepción que otro registró si el otro ya no es el destinatario o
  Argos la rechazó (`pendientes.RehacerEsperando` con el dueño nuevo, auditado); no si está en curso,
  hecha o resuelta a mano.
- El armado de la firma guarda solo si el pendiente sigue en la versión que leyó; el PNG se decodifica
  completo; series de 1 a 100 caracteres; `recibidoEn` no anterior al envío (5 min de tolerancia).
- El worker no arranca si el almacenamiento de adjuntos no abre.
