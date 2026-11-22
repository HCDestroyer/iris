# Ficha del técnico

## Para qué sirve

Ver y cambiar los datos de agenda de un técnico: cuadrilla, teléfono, habilidades, bodega en Argos, turnos semanales y ausencias.

## Cómo se usa

1. Abre un técnico desde [Técnicos](/manual/tecnicos/lista).
2. Revisa las pestañas:
    - **Datos:** cuadrilla, teléfono, habilidades y bodega en Argos.
    - **Agenda:** sus citas.
    - **Ausencias:** las vigentes y las próximas.
3. Para cambiar los datos, usa **Editar técnico**: [Alta y edición del técnico](/manual/tecnicos/formulario).
4. Para fijar sus horas de trabajo, abre **Turnos semanales**, agrega un turno por día con hora de inicio y fin, y pulsa **Guardar turnos**. Un turno nocturno se parte en dos (hasta 24:00 y desde 00:00).
5. Para vacaciones, enfermedad, capacitación o permiso, usa **Nueva ausencia**: [Registrar una ausencia](/manual/tecnicos/ausencia).
6. Para quitar una ausencia, pulsa **Anular ausencia** y confirma.
7. Para que deje de recibir órdenes, desactiva la ficha.
8. Si Iris está conectado con Argos, puedes **Vincular con Argos** o **Desvincular** su usuario de Argos (su bodega).

## Qué significa cada estado

- **Activa** o **Inactiva:** si se le pueden programar órdenes.
- **Sin turnos:** no tiene horario fijo; la agenda no puede comprobar su disponibilidad.
- **Sin bodega:** no tiene bodega de Argos; no puede descontar materiales de su inventario.
- **Argos < 1.6.0: inventario por técnico no disponible:** el Argos conectado es viejo.

## Preguntas frecuentes

**Desactivé un técnico, ¿qué pasa con sus citas?**
No se mueven. Reprográmalas en la [Agenda](/manual/agenda/calendario) si hace falta.

**Al registrar una ausencia, Iris me avisa de citas en conflicto.**
La ausencia se guarda igual. Abre la agenda de ese día y mueve esas citas.

## Permisos necesarios

- Editar: `agenda.editar_tecnico`. Activar o desactivar: `agenda.activar_tecnico` y `agenda.desactivar_tecnico`.
- Turnos: `agenda.fijar_turnos`. Ausencias: `agenda.registrar_ausencia` y `agenda.anular_ausencia`.
- Vincular con Argos: `agenda.vincular_argos` y `agenda.desvincular_argos`.
