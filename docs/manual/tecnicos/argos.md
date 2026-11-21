# Técnicos de Argos sin ficha

## Para qué sirve

Muestra los usuarios de Argos que tienen bodega de técnico pero todavía no tienen ficha de agenda en Iris. Crear su ficha permite programarles órdenes y descontar los materiales de su bodega.

## Cómo se usa

1. En [Técnicos](/manual/tecnicos/lista), abre **Técnicos de Argos sin ficha**.
2. Busca por bodega, nombre o correo.
3. Elige el técnico y crea su ficha. Al guardar queda vinculado y Iris toma su bodega de Argos.
4. Si hay muchos, usa **Cargar más de Argos**.

## Qué significa cada estado

- **Todos los técnicos de Argos tienen ficha:** no queda nadie por dar de alta.
- **No se pudo leer la lista de Argos:** Argos no respondió. Pulsa **Reintentar**.

## Preguntas frecuentes

**Un técnico nuevo no aparece.**
Primero debe tener una bodega técnico en Argos, en la empresa activa.

## Permisos necesarios

- `agenda.crear_tecnico` y `agenda.vincular_argos`.
