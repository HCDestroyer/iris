# Boletas: un comprobante por empresa (antes de migrar)

La migración `20261116095604_mensajeria_boletas_confirmacion.sql` crea el índice único
`boletas_comprobante_uidx` sobre `mensajeria.boletas (coalesce(empresa_id, 0), comprobante_id)`: un
comprobante de Argos es de una sola boleta por empresa.

Solo hace falta revisar esto si la instalación corrió la rama de B3 (aprobar y rechazar boletas)
**antes** de esa migración. En una instalación que pasa directo de una versión publicada a otra no
hay nada que hacer.

## Antes de `iris migrar`

```sql
SELECT empresa_id, comprobante_id FROM mensajeria.boletas
WHERE comprobante_id IS NOT NULL GROUP BY 1,2 HAVING count(*)>1;
```

- **Sin filas:** migra normalmente.
- **Con filas:** la migración fallaría al crear el índice. Para cada par, mira las boletas:

    ```sql
    SELECT id, conversacion_id, estado, referencia, monto, created_at, decidida_en
    FROM mensajeria.boletas WHERE coalesce(empresa_id, 0) = coalesce(:empresa_id, 0) AND comprobante_id = :comprobante_id
    ORDER BY created_at;
    ```

    Confirma en Argos (`GET /comprobantes/:id`, o la caja del panel) a cuál corresponde el
    comprobante y quítaselo a las demás (`UPDATE mensajeria.boletas SET comprobante_id = NULL,
    estado = 'fallida', detalle = 'comprobante repetido' WHERE id = :id`). No borres boletas: tienen
    su vínculo en la conversación. Después corre la consulta otra vez y migra.
