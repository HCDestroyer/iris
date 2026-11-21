# Plantillas de WhatsApp: migración y vuelta atrás

Migración: `apps/api/migraciones/20261121065851_mensajeria_plantillas_whatsapp.sql`.
Contrato: `.claude/integration/contratos/plantillas-whatsapp.md`.

## Antes de probar en dev y en el lab

Esta migración cambió mientras su PR estaba en revisión (`codigos[]`, dueña de la cuenta,
`meta_evento_en`, `borrada_por = 'sistema'`, `_avisos_meta`). En una base de dev o del lab que ya
corrió una versión anterior, goose no la vuelve a correr sola: hay que bajarla y subirla de nuevo
antes de probar (borra lo hecho en la pantalla de plantillas; ver «Qué borra el `Down`»):

```bash
goose -dir apps/api/migraciones postgres "$IRIS_DB_URL" down-to 20261121033239
goose -dir apps/api/migraciones postgres "$IRIS_DB_URL" up-to 20261121065851
```

Si después hay migraciones más nuevas, sigue con `goose ... up`.

## Qué hace el `Up`

1. Crea `mensajeria.plantillas_whatsapp` (plantillas por cuenta), su `_historial`, `_cuentas` (dueña y
   estado de sincronización de cada WABA), `_ajustes` (marketing por empresa) y `_avisos_meta`
   (avisos de Meta ya aplicados, para no aplicarlos dos veces).
2. Fija la empresa dueña de cada WABA de los números activos y conectados (su número de avisos o, si
   no hay, el más antiguo).
3. Copia `mensajeria.canales.plantillas` de los números activos y conectados de la dueña, como
   aprobadas (`origen = migrada`). Lo inválido se descarta y sale como `NOTICE` en la salida de goose:
   guárdala.

`mensajeria.canales.plantillas` **no se toca**.

## Cuentas compartidas de antes (una WABA con números de varias empresas)

Antes de esta migración, dos empresas podían tener números en la misma WABA. La migración le da la
cuenta a una sola (la del número de avisos o, si no, la del número más antiguo). Desde entonces:

- Los números de la dueña usan las plantillas de la cuenta (pantalla «Plantillas de WhatsApp»).
- Los números de **otra** empresa en esa cuenta siguen con su mapa propio
  (`mensajeria.canales.plantillas`), como antes: sus avisos no se cortan. No ven ni tocan las
  plantillas de la cuenta, y editarlos sin cambiar de cuenta (nombre, cola, plantillas) funciona.
  Moverlos a otra cuenta que ya es de otra empresa responde 409 `waba_de_otra_empresa`.
- Para ordenarlo: o la otra empresa usa una WABA propia, o la dueña le **presta** su número.
- Los números inactivos o desconectados no se copiaron. Al **reactivar** uno: si la cuenta aún no
  tiene dueña, pasa a su empresa y su mapa se copia a la cuenta en revisión (`PENDING`), solo en los
  códigos que ninguna otra plantilla atiende; luego Iris relee la cuenta en Meta. Si la cuenta ya
  tiene dueña, reactivar **nunca** la cambia (no se comprueba nada en Meta): el número sigue con su
  mapa propio si es de otra empresa.
- Un número desconectado por Meta sigue siendo de su empresa. La cuenta solo puede pasar a otra
  empresa cuando todos los números de la dueña están dados de baja (inactivos), y solo al crear o
  actualizar un número con la cuenta comprobada en Meta.
- Si una cuenta pasa a otra empresa (la anterior ya no tiene números en ella), las plantillas de la
  anterior quedan archivadas: «Borrada» con `borradaPor = sistema`. Sus códigos y usos quedan en el
  historial de cada plantilla y en el evento `plantilla_whatsapp.borrada`, para poder deshacerlo. No se eliminan y la nueva dueña
  no las ve; la nueva dueña trae las suyas con «Sincronizar con Meta».

## Qué borra el `Down`

`goose down` elimina las cinco tablas, y con ellas **todo lo hecho desde el panel de plantillas**:

- las plantillas creadas en Iris (borradores incluidos), las sugeridas y las traídas de Meta;
- los usos y códigos elegidos en la pantalla de plantillas;
- el historial de cada plantilla;
- la dueña de cada cuenta, su última sincronización y su error;
- el ajuste de marketing de cada empresa;
- los avisos de Meta ya aplicados.

No borra nada en Meta: las plantillas siguen allá. Los avisos y conversaciones vuelven a salir con el
mapa de cada número (`mensajeria.canales.plantillas`), que solo tiene lo que había antes de la
migración más lo que se cambió desde el formulario de «Números de WhatsApp».

## Antes de bajar

1. Respalda las tablas (`docs/runbooks/respaldos.md`):
   `pg_dump -t 'mensajeria.plantillas_whatsapp*' iris > plantillas_whatsapp.sql`.
2. Si se eligieron códigos en la pantalla de plantillas que no están en el mapa del número, pásalos al
   formulario de «Números de WhatsApp» antes, o esos avisos quedarán `bloqueado / canal_no_disponible`.

## Volver a subir

`goose up` es repetible: vuelve a copiar el mapa de los números. Lo creado en el panel se recupera
del respaldo o con «Sincronizar con Meta» (vuelve como `origen = meta`, sin usos ni códigos).
