# Mapa

## Para qué sirve

Ver en un mapa dónde están los técnicos ahora y las órdenes del día, y asignar rápido las que no tienen técnico.

## Cómo se usa

1. Entra a **Mapa** en el menú.
2. En **Qué mostrar**, marca **Técnicos**, **Órdenes** o los dos.
3. Toca un técnico para ver su tarjeta: qué hace **Ahora**, cuál es la **Siguiente** orden, su **Avance** del día y su **Posición**.
4. Para darle una orden sin técnico, elígela en **Elegir orden…** dentro de su tarjeta. Si ya tenía hora, se guarda esa misma hora en su agenda.
5. Abajo (o al lado) está la **Lista del mapa**, útil en el teléfono.
6. Para trabajar con calendario y rutas, abre [Despacho](/manual/despacho/tablero).

## Qué significa cada estado

- **En sitio:** el técnico está en una orden.
- **En camino:** va hacia una orden.
- **Sin señal:** no ha enviado ubicación hace rato.
- **Orden sin asignar:** orden del día sin técnico.

## Preguntas frecuentes

**¿Por qué no salen routers ni nodos?**
Iris todavía no recibe de Horus la ubicación de la red. Las caídas siguen llegando como tickets de incidente.

**No veo las órdenes sin asignar.**
Hace falta el permiso `ordenes.ver`.

## Permisos necesarios

- `agenda.ver` o `campo.ver_posiciones` para entrar.
- Técnicos en vivo: `campo.ver_posiciones`. Órdenes sin asignar: `ordenes.ver`. Asignar: `ordenes.asignar`.
