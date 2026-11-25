# integracion

Outbox y entrega de eventos (CLAUDE.md §3).

- `Insertar` escribe un evento en `integracion.outbox` dentro de la transacción del caso de uso
  (lo usa `auditoria.Tx.Emitir`) y hace `pg_notify(iris_outbox)`, que sale al confirmar.
- `Relay` entrega los pendientes a los suscriptores del `Bus` (`bus.Suscribir(nombre, evento, fn)`).
  Un solo relay a la vez (advisory lock) y **un evento por transacción corta** (`FOR UPDATE SKIP
  LOCKED`): lo entregado queda confirmado aunque la corrida se corte. Si un suscriptor falla, el
  evento se reintenta con espera exponencial (5 s, 10 s, … hasta 1 h) para todos sus
  suscriptores; tras `MaxIntentos` (20) queda `fallido_en` y no se reintenta (error en el log;
  para reintentarlo a mano: `fallido_en = NULL, proximo_intento = now()`).
- `NuevoClienteRiver` + `EscucharOutbox`: el worker (`cmd/worker`) corre el relay con River por
  cada `NOTIFY` y periódicamente (`IRIS_RELAY_INTERVALO`). Excepción a la regla 6: el job del
  relay no usa `unique` de River (la unicidad descartaba el job de un `NOTIFY` llegado justo
  después de otro relay, y River obliga a incluir `running`). Lo acotan su cola propia (`relay`,
  un worker), el advisory lock y que ni el listener ni la corrida periódica encolan si ya hay un
  relay `available` en `river_job` (consulta propia por estado y cola; si falla, se encola igual). Tras soltar el candado el relay mira si quedó algo pendiente (saltando filas
  bloqueadas), y tras cada `LISTEN` (arranque y reconexión) el listener encola un barrido de lo
  que se avisó sin nadie escuchando; la espera de reconexión vuelve a 1 s tras un `LISTEN` correcto.

Los suscriptores deben ser idempotentes: un evento puede llegar más de una vez.

- `NuevoClienteInsercion` + `Encolar`: la API encola jobs de River dentro de la transacción del
  caso de uso (`InsertTx`); `OpcionesWorker.Registrar` y `Periodicos` suman los de otros módulos.

**Eventos:** el outbox no emite eventos de negocio; transporta todos los del catálogo
(`.claude/integration/contratos/eventos.md`). Hoy no hay suscriptores internos registrados; los
webhooks salientes a Argos y Horus llegan con la Fase 2.

**Conectores:** `conector/` (base común: enlace de conexión, webhooks firmados, cliente HTTP con
SSRF, URL pública), `argos/` (lectura de Argos, instantáneas) y `horus/` (webhooks de Horus →
eventos internos `horus.*`, lecturas de salud y router). Ver el README de cada uno.

**La outbox no se purga.** Las filas publicadas se quedan en `integracion.outbox`. La deduplicación
de algunos avisos depende de eso: comercial no repite `orden.contrato_sin_firma` con el mismo motivo
para la misma clave de pendiente o cotización (y empresa) si ya está en la outbox
(`AvisoSinFirmaPrevio`). Si algún día se purga, esos avisos necesitan su propia marca.
