# Plantillas de WhatsApp: migración y vuelta atrás

Migración: `apps/api/migraciones/20261121065851_mensajeria_plantillas_whatsapp.sql`.
Contrato: `.claude/integration/contratos/plantillas-whatsapp.md`.

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
