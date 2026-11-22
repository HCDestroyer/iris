# Despacho

## Para qué sirve

Reúne en una pantalla lo que necesita quien reparte el trabajo del día: el calendario por técnico, el mapa con las órdenes y los técnicos en vivo, y la ruta de cada técnico (la propuesta frente a la real).

## Cómo se usa

1. Abre **Despacho** desde el [Mapa](/manual/mapa/tecnicos) o con el buscador global.
2. En **Calendario del día**, arrastra una cita a otra hora o a otro técnico. En el teléfono, usa **Mover a…** y escribe el motivo.
3. Arrastra una orden de **Órdenes sin programar** a la línea de un técnico para darle hora, o a su nombre para solo asignarla.
4. En **Técnicos en vivo** ves la última posición enviada por la app de campo.
5. En **Ruta propuesta frente a la real**, elige el **Técnico de la ruta** y pulsa **Proponer ruta**. Iris calcula el orden de las paradas.
6. Revisa la propuesta y publícala para que el técnico la vea en su app. Puedes cambiar el orden de las paradas.
7. Compara la llegada estimada de cada parada con la real. En el mapa, la ruta propuesta es la línea continua y el recorrido real, la punteada.

## Qué significa cada estado

- **En vivo:** el técnico mandó su ubicación hace poco.
- **Sin señal reciente:** su teléfono no ha mandado ubicación en un rato.
- **A tiempo** o **Fuera de la ventana:** si la llegada cae dentro de la ventana acordada con el cliente.
- **La ruta quedó desactualizada:** una orden cambió de técnico o de hora después de proponerla. Vuelve a proponerla para poder publicarla. El técnico sigue con la ruta anterior hasta que publiques otra.
- **La ruta termina después del turno:** las paradas no caben en el horario del técnico.
- **Tiempos aproximados:** no se pudo calcular el recorrido exacto.

## Preguntas frecuentes

**No veo el recorrido real.**
Necesitas el permiso de ver ubicaciones, y la app de campo debe haber mandado ubicación ese día.

**El mapa no se dibuja.**
El navegador necesita WebGL. Las listas siguen al día aunque el mapa no cargue.

**Algunas paradas salen sin nombre.**
Están fuera de tu alcance: solo ves su lugar en la secuencia.

## Permisos necesarios

- Ver: `agenda.ver`.
- Ubicaciones en vivo y recorrido real: `campo.ver_posiciones`.
- Rutas: `rutas.ver`, `rutas.proponer` y `rutas.publicar` (**Despacho** y **Supervisor**).
- Mover citas: `ordenes.programar`, `ordenes.reprogramar` y `ordenes.asignar`.
