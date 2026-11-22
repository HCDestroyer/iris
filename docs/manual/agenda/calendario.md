# Agenda

## Para qué sirve

Ver y organizar las citas de los técnicos: qué órdenes tiene cada uno, cuándo, sus ausencias y los conflictos. Desde aquí se programan y se mueven visitas.

## Cómo se usa

1. Entra a **Agenda** en el menú.
2. Elige la **Vista**:
    - **Mes:** por cada día, cuántas órdenes hay, conflictos, ausencias y órdenes sin técnico. Al elegir un día ves su resumen con **Abrir el día** y **Programar**.
    - **Semana:** los 7 días por técnico.
    - **Día por técnico:** una línea por técnico con sus citas.
3. Cambia de día, semana o mes con las flechas.
4. Filtra por **Técnico**, **Cuadrilla** o **Zona**. Los filtros quedan en la dirección de la página: puedes compartir el enlace.
5. En día o semana, arrastra una cita a otra hora o a otro técnico para reprogramarla o reasignarla. En el teléfono, usa **Mover a…**.
6. Para programar una orden pendiente, arrástrala desde **Órdenes sin programar** a la línea de un técnico.

## Qué significa cada estado

- **Sin órdenes:** ese día no hay nada programado.
- **Conflicto:** una cita choca con otra, con una ausencia o con el turno del técnico.
- **Ausencia:** el técnico no trabaja ese día (vacaciones, enfermedad, capacitación u otro motivo).
- **Sin técnico:** hay órdenes de ese día sin asignar.

Al mover una cita, Iris puede avisar:

- **No tiene turno ese día.**
- **Está ausente ese día.**
- **Ya completó su capacidad del día.**
- **No le queda un hueco del largo de esta orden.**

## Preguntas frecuentes

**Dice «Demasiadas citas para mostrarlas todas».**
Filtra por cuadrilla, zona o técnico.

**Un técnico no aparece.**
Revisa los filtros. Para salir en la agenda, el técnico necesita una ficha de agenda: [Técnicos](/manual/tecnicos/lista).

**¿En qué hora están las citas?**
En la hora de Guatemala.

## Permisos necesarios

- Ver: `agenda.ver` (**Despacho**, **Supervisor**, **Ventas** y **Lectura**).
- Programar y mover: `ordenes.programar`, `ordenes.reprogramar` y `ordenes.asignar`.
