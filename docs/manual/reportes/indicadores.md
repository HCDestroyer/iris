# Reportes

## Para qué sirve

Medir cómo trabaja el equipo de soporte y de campo en un período: plazos cumplidos, tiempos de resolución, órdenes por técnico, primera visita resuelta, reincidencias y satisfacción.

## Cómo se usa

1. Entra a **Reportes** en el menú.
2. Elige el reporte:
    - **SLA cumplido:** tickets resueltos que respondieron y se resolvieron dentro de su plazo.
    - **Tiempo de resolución:** desde que se abrió el ticket hasta que se resolvió (tiempo de reloj, no horario hábil).
    - **Órdenes por técnico:** órdenes finalizadas, su resultado y el tiempo en sitio.
    - **Primera visita resuelta:** instalaciones y visitas cerradas con éxito sin otra orden ni ticket del mismo servicio después.
    - **Reincidencias:** tickets resueltos tras los que el mismo servicio o cliente abrió otro.
    - **Satisfacción:** encuestas enviadas y cómo calificaron.
3. Elige el **Período** (**Este mes**, **Mes anterior**, **Últimos 90 días** o fechas propias). Todo va en hora de Guatemala.
4. Acota con zona, cuadrilla, técnico, categoría o prioridad según el reporte.
5. Pulsa **Descargar CSV** para abrirlo en una hoja de cálculo.

## Qué significa cada estado

- **Mediana:** la mitad se resolvió en menos tiempo. Es mejor guía que el promedio cuando hay casos muy largos.
- **Con retorno:** hubo otra orden o ticket del mismo servicio dentro de los días que elegiste.
- **En observación:** todavía no pasan los días para saber si vuelve.
- **Sin servicio:** no se puede evaluar; no cuenta en el porcentaje.
- **Sin SLA de resolución:** no cuenta en el porcentaje.
- **Alertas:** encuestas con calificación de 2 o menos.

## Preguntas frecuentes

**Me pide acotar el período.**
Hay demasiados datos. Acorta las fechas o filtra por zona, cuadrilla o técnico.

**Un ticket de la reincidencia dice «No disponible».**
Es de una zona o cuadrilla que no ves; la reincidencia cuenta igual.

## Permisos necesarios

- `reportes.ver` (**Supervisor** y **Lectura**). Los filtros de técnicos piden `agenda.ver`; los de tickets, `tickets.ver`.
