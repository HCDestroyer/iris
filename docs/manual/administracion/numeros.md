# Números de WhatsApp

## Para qué sirve

Conectar los números de WhatsApp de la empresa para recibir y contestar mensajes en la bandeja de [Conversaciones](/manual/conversaciones/bandeja). Aquí también se elige la cola, el horario y las llamadas de cada número.

## Cómo se usa

1. En **Administración**, abre **Números de WhatsApp**.
2. Pulsa **Conectar número** y elige cómo:
    - **Con QR**, si ya usas ese número en la app WhatsApp Business.
    - **Con la API de Meta**, con los datos de tu app de Meta (token y app secret).
3. Si eliges la API, sigue **Cómo conectar WhatsApp**: una guía paso a paso dentro de Iris.
4. Pega el **Token de acceso** y el **App secret**, y conecta el webhook en Meta. Pulsa **Guardar y probar**.
5. Abre el número y usa **Configurar uso y horario** para elegir su cola, su horario y si recibe llamadas.
6. Si otra empresa de tu grupo debe usar el mismo número, compártelo desde su ficha.

## Qué significa cada estado

- **Número conectado:** recibe y envía mensajes.
- **Número desconectado:** no recibe mensajes. Revisa sus credenciales.
- **Siempre abierto:** no tiene horario.
- **Sin cola:** sus conversaciones no llegan a nadie.
- **Meta lo desconecta a los 14 días sin uso:** en números con QR, abre la app en el teléfono de vez en cuando.

## Preguntas frecuentes

**Meta rechazó el token.**
Venció o no tiene permiso sobre ese número. Pulsa **Cambiar token** y pega uno nuevo.

**¿Puedo terminar después?**
Sí. El número queda en la lista con su estado.

## Permisos necesarios

- `canales.configurar` o `mensajeria.configurar` (**Supervisor**).
