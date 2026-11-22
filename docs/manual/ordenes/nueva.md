# Nueva orden

## Para qué sirve

Crear una orden de trabajo para mandar a un técnico: instalación, visita técnica, retiro, cambio de equipo o mantenimiento.

## Cómo se usa

1. En [Órdenes de trabajo](/manual/ordenes/lista), pulsa **Nueva orden**. También puedes crearla desde la ficha de un ticket o desde una conversación; en ese caso hereda los datos del ticket.
2. Elige el **Tipo**.
3. En **Checklist**, deja «La del tipo» o elige otro formulario para el técnico.
4. Escribe el **Trabajo a realizar**.
5. Busca el **Cliente en Argos** y su servicio. La dirección y la zona salen del cliente si las dejas vacías.
6. Si no eliges cliente, escribe la **Dirección** (es obligatoria) y, si las tienes, latitud y longitud. Elige la **Zona**.
7. En **Contacto en sitio**, pon el nombre y el teléfono de quien recibe al técnico.
8. Pulsa **Crear orden**. Nace **Pendiente**: después se asigna y se programa desde su ficha, la agenda o el despacho.

## Qué significa cada estado

- La orden nueva queda **Pendiente** hasta que tenga técnico y hora.
- Desde un ticket, **Crear orden y asignar** la crea y la asigna en un solo paso.

## Preguntas frecuentes

**No se cargan las plantillas.**
Se usa la del tipo de orden. Puedes agregar formularios después desde la ficha.

**El cliente no está en Argos.**
Escribe la dirección de la visita a mano.

## Permisos necesarios

- `ordenes.crear` (**Despacho** y **Supervisor**). Desde un ticket, además `tickets.crear_orden`.
