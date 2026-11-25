# Conexión con Meta

## Para qué sirve

Definir cómo se conecta este Iris con Meta para registrar números de WhatsApp con QR. Es una sola conexión para toda la instalación.

## Cómo se usa

1. En **Administración**, abre **Conexión con Meta**.
2. Elige el **Modo de la conexión**:
    - **App propia:** tu ISP pone su app de Meta y todo se queda en tu servidor. Para el QR, tu app debe ser Tech Provider aprobado.
    - **App compartida:** a través del servicio central.
3. Con app propia, escribe el **App ID**, el **App secret** y el **ID de configuración**.
4. Copia el **Dominio permitido** y agrégalo en Meta.
5. Copia la **URL de devolución de llamada** y el **Token de verificación** del webhook y pégalos en Meta. Suscríbete a los campos que indica la pantalla.
6. Pulsa **Guardar y probar**.

## Qué significa cada estado

- **Meta aún no verificó el webhook:** falta pegar el token en Meta y verificar.
- **Token de verificación oculto:** solo se muestra una vez. Si lo perdiste, usa **Generar otro**.

## Preguntas frecuentes

**¿Cambiar el modo desconecta mis números?**
No. Los números ya conectados siguen funcionando.

## Permisos necesarios

- `mensajeria.conexion_meta` (**Administrador**).
