# auditoria

Base de los casos de uso de Iris (CLAUDE.md §1, §3 y §9). Un caso de uso es un
`*auditoria.Caso[In, Out]` armado con `auditoria.Nuevo` y ejecutado con `Ejecutar(ctx, in)`:

1. abre la transacción (`nucleo.EnTx`; si ya hay una en `ctx`, un savepoint);
2. verifica el acceso (`ConPermiso`, `Autenticado`, `Publico` o `Cliente`): anónimo → 401, sin
   permiso → 403. `Cliente` es solo para el actor cliente del portal (fija su empresa como activa
   y el canal `portal`); el actor cliente recibe 403 en cualquier otro acceso salvo `Publico`;
3. corre `Logica` con `*auditoria.Tx` (sirve a sqlc y trae los ayudantes);
4. escribe `auditoria.acciones` y `auditoria.cambios` (lo sensible como `***`) y los eventos de
   `t.Emitir` en `integracion.outbox`, todo en el mismo COMMIT.

Si falla, ROLLBACK y la fila de error se escribe en otra transacción. Ayudantes de `Tx`:
`FijarEntidad`, `FijarEmpresa`, `RegistrarCambios`, `Anotar`, `Emitir`, `AlConfirmar` (tras el
COMMIT más externo) y `AlFallar` (trabajo que debe quedar escrito aunque la acción falle, p. ej.
contar un intento de login fallido: corre en la transacción de la fila de error). Con el actor
cliente, `auditoria.acciones.actor_tipo = 'cliente'` y `actor_id` = id del cliente en Argos.

**Eventos:** no emite ni consume eventos propios; `Tx.Emitir` escribe los de cada módulo.

Uso detallado para otros frentes: `.claude/integration/i1.md`.
