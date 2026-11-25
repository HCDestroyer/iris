# Alta y edición del técnico

## Para qué sirve

Crear la ficha de agenda de un usuario que trabaja órdenes en campo, o cambiar sus datos.

## Cómo se usa

1. En [Técnicos](/manual/tecnicos/lista), pulsa **Nuevo técnico**. Para cambiar uno, abre su ficha y pulsa **Editar técnico**.
2. En **Buscar usuario**, elige a la persona. Solo salen personas activas con el rol de técnico y aún sin ficha.
3. Si todavía no tiene usuario, usa **Crear usuario del técnico**: se crea con el rol Técnico y vuelves aquí con el usuario elegido.
4. Elige la **Cuadrilla**. Define las zonas que atiende.
5. Escribe su **Teléfono** y sus **Habilidades** (separadas por comas, opcional).
6. Si Iris está conectado con Argos, elige su **Bodega en Argos**. Si tu Argos es anterior a la versión 1.6.0, escribe el número de bodega tal como está en Argos.
7. Pulsa **Crear técnico** o **Guardar cambios**.

## Qué significa cada estado

- **Atiende todas las zonas:** su cuadrilla no tiene zonas marcadas.
- **Sigue sin vínculo con Argos:** la ficha se guardó, pero no quedó ligada a su usuario de Argos. Usa **Reintentar vínculo**.

## Preguntas frecuentes

**Dice «No hay cuadrillas activas».**
Créalas en [Cuadrillas](/manual/administracion/cuadrillas).

**Dice «Ese usuario de Argos ya está vinculado a otra ficha».**
Desvincúlalo de la otra ficha primero.

**Dice «Ese usuario no tiene bodega técnico activa en esta empresa».**
Dásela en Argos y vuelve a intentarlo.

## Permisos necesarios

- Crear: `agenda.crear_tecnico`. Editar: `agenda.editar_tecnico`.
- Crear el usuario desde aquí: `usuarios.crear`.
