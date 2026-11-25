# Técnicos

## Para qué sirve

Administrar las fichas de agenda de los técnicos: a qué cuadrilla pertenecen (y con ella qué zonas atienden), su contacto, habilidades, turnos y ausencias. Sin ficha de agenda, a un técnico no se le pueden programar órdenes.

## Cómo se usa

1. Entra a **Técnicos** en el menú.
2. Busca por nombre o correo, o filtra por cuadrilla.
3. Pulsa **Ver ficha** para abrir un técnico: [Ficha del técnico](/manual/tecnicos/ficha).
4. Para dar de alta a alguien, pulsa **Nuevo técnico**: [Alta y edición del técnico](/manual/tecnicos/formulario).
5. Si Iris está conectado con Argos, revisa **Técnicos de Argos sin ficha**: [Técnicos de Argos sin ficha](/manual/tecnicos/argos).

## Qué significa cada estado

- **Activa:** el técnico se puede programar en la agenda y el despacho.
- **Inactiva:** deja de ser asignable. Sus citas ya programadas no se mueven.

## Preguntas frecuentes

**Dice «Aún no hay técnicos con ficha».**
Primero crea el usuario con el rol **Técnico** en [Usuarios](/manual/usuarios/lista) y después su ficha aquí.

**Un técnico atiende varias zonas.**
Las zonas salen de su cuadrilla. Cámbialas en [Cuadrillas](/manual/administracion/cuadrillas).

## Permisos necesarios

Entra quien administra fichas: `agenda.crear_tecnico`, `agenda.editar_tecnico`, `agenda.activar_tecnico`, `agenda.desactivar_tecnico`, `agenda.fijar_turnos`, `agenda.registrar_ausencia` o `agenda.anular_ausencia`. El **Supervisor** los tiene todos; **Despacho**, los de turnos y ausencias.
