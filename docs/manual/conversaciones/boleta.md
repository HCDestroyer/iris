# Boleta de pago

## Para qué sirve

Mandar a Argos la boleta de depósito que el cliente envió por WhatsApp, para que caja la revise y registre el pago.

## Cómo se usa

1. En la conversación, abre **⋯ Más acciones** y elige la boleta de pago. La conversación debe tener asociado al cliente de Argos.
2. **1. La boleta:** elige la foto o el PDF que mandó el cliente en esta conversación.
3. **2. La cuenta:** elige la cuenta de la empresa a la que depositó, tal como está en Argos.
4. **3. El depósito:** método, banco de origen, **Monto (Q)**, **Fecha del depósito** y, si el cliente tiene varios servicios, de cuál es el pago.
5. Agrega una nota si hace falta y pulsa **Enviar a Argos**.

## Qué significa cada estado

- **Enviándose a Argos:** Iris la está mandando.
- **Pendiente de revisar:** ya está en Argos esperando a caja.
- **Aprobada:** caja registró el pago.
- **Rechazada:** caja la rechazó (por ejemplo, el depósito no aparece o el monto no coincide).
- **Argos no la aceptó:** hubo un error al registrarla.

## Preguntas frecuentes

**Dice «Asocia primero el cliente de Argos».**
Busca y asocia al cliente en la ficha de la conversación.

**Dice «La empresa no tiene cuentas activas en quetzales en Argos».**
Pide que las den de alta en Argos.

**¿Se le avisa al cliente?**
Al aprobar o rechazar, Iris le avisa por WhatsApp. Si no se pudo, verás «No se le pudo avisar al cliente por WhatsApp: avísale tú».

## Permisos necesarios

- Registrar: `conversaciones.registrar_boleta` (**Soporte** y **Supervisor**).
- Aprobar o rechazar: rol **Caja**, con su usuario de Argos vinculado.
