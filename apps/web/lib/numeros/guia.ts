/*
 * «Cómo conectar WhatsApp»: la guía dentro del panel (pedido del PM). Paso a paso, en español
 * llano, para quien administra el ISP y no es técnico. Se muestra con `MarkdownSeguro` (sin HTML).
 *
 * Solo dice lo que respaldan la documentación pública de Meta (WhatsApp Cloud API, Business
 * Manager) o nuestro contrato (contratos/conversaciones.md §1 y §1.1). Meta cambia a menudo los
 * nombres de sus menús y los traduce distinto: donde puede variar, se dice.
 *
 * Los `id` son los anclajes que usan «¿Dónde lo encuentro?» en «Agregar número»
 * (`/administracion/numeros/guia#token`): si cambias uno, cambia también `ANCLAS`.
 */

export const RUTA_GUIA = "/administracion/numeros/guia";

export type SeccionGuia = { id: string; titulo: string; cuerpo: string };

/** Anclajes que enlazan otras pantallas. */
export const ANCLAS = {
    portafolio: "portafolio",
    app: "app",
    numero: "numero",
    usuarioSistema: "usuario-sistema",
    token: "token",
    ids: "ids",
    appSecret: "app-secret",
    webhook: "webhook",
    varios: "varios-numeros",
    estados: "estados",
    errores: "errores",
    qr: "qr",
} as const;

export const enlaceGuia = (ancla?: (typeof ANCLAS)[keyof typeof ANCLAS]) => (ancla ? `${RUTA_GUIA}#${ancla}` : RUTA_GUIA);

export const SECCIONES_GUIA: readonly SeccionGuia[] = [
    {
        id: "antes",
        titulo: "Antes de empezar",
        cuerpo: `Iris se conecta a WhatsApp con la **API oficial de Meta (WhatsApp Cloud API)**. No usa conexiones por QR que no son de Meta: Meta bloquea esos números.

Necesitas:

- Ser administrador del **portafolio comercial de Meta** de tu empresa (antes «Business Manager»).
- Un **número de teléfono** que pueda recibir un SMS o una llamada para verificarlo. No puede estar en uso en la app de WhatsApp ni en WhatsApp Business: si lo está, primero se borra la cuenta de esa app, o se conecta con QR (al final de esta guía).
- Una **tarjeta** para el método de pago de WhatsApp.
- Que Iris se pueda abrir desde internet con **https** (Meta no entrega mensajes a direcciones sin https).
- Unos 30 a 60 minutos, más lo que tarde Meta en revisar el nombre del número y las plantillas.`,
    },
    {
        id: ANCLAS.portafolio,
        titulo: "1. Crear o verificar el portafolio comercial",
        cuerpo: `1. Entra a [business.facebook.com](https://business.facebook.com) con tu cuenta de Facebook.
2. Si tu empresa aún no tiene portafolio, créalo con el nombre legal de la empresa.
3. Abre **Configuración del negocio** (Business settings) → **Centro de seguridad** (Security Center) → **Verificación del negocio** y sigue los pasos (Meta pide documentos de la empresa).

Puedes seguir sin la verificación terminada, pero mientras no esté lista Meta limita a cuántos clientes puede escribirles el número cada día. La revisión puede tardar varios días.`,
    },
    {
        id: ANCLAS.app,
        titulo: "2. Crear la app con el producto WhatsApp",
        cuerpo: `1. Entra a [developers.facebook.com/apps](https://developers.facebook.com/apps) con la misma cuenta y pulsa **Crear app** (Create app).
2. Cuando Meta pregunte para qué es, elige la opción de **WhatsApp** o, si no aparece, **Otro** y el tipo **Negocio** (Business). Las opciones de este asistente cambian seguido.
3. Ponle un nombre (por ejemplo, «Iris WhatsApp») y conéctala a tu **portafolio comercial**.
4. En el panel de la app, agrega el producto **WhatsApp** (botón **Configurar** / Set up).

Meta crea un número de prueba y una cuenta de WhatsApp de prueba. Sirven para ensayar, pero no los uses para hablar con clientes.`,
    },
    {
        id: ANCLAS.numero,
        titulo: "3. Agregar el número de teléfono",
        cuerpo: `1. En la app, abre **WhatsApp** → **Configuración de la API** (API Setup) y pulsa **Agregar número de teléfono** (Add phone number).
2. Escribe el **nombre visible** (lo que verán tus clientes: el nombre de tu ISP). Meta lo revisa y debe parecerse al nombre de tu empresa.
3. Elige la categoría y escribe el número con su código de país.
4. Pide el código por **SMS** o **llamada** y escríbelo.

Si Meta te pide crear un **PIN de 6 dígitos** (verificación en dos pasos), créalo y guárdalo en un lugar seguro: Meta lo pide para volver a registrar el número.`,
    },
    {
        id: ANCLAS.usuarioSistema,
        titulo: "4. Crear el usuario del sistema",
        cuerpo: `El token que usa Iris no debe ser de una persona: si esa persona se va, el token deja de servir. Por eso se crea un **usuario del sistema**.

1. En [business.facebook.com](https://business.facebook.com), abre **Configuración del negocio** → **Usuarios** → **Usuarios del sistema** (System users).
2. Pulsa **Agregar**, ponle un nombre (por ejemplo, «Iris») y el rol **Administrador** (Admin).`,
    },
    {
        id: "asignar",
        titulo: "5. Darle la app y la cuenta de WhatsApp",
        cuerpo: `Con el usuario del sistema elegido, pulsa **Asignar activos** (Assign assets o Add assets):

1. En **Apps**, elige tu app y dale **control total** (Manage app / Full control).
2. En **Cuentas de WhatsApp** (WhatsApp accounts), elige la cuenta de WhatsApp Business de tu número (la **WABA**) y dale **control total**.
3. Guarda.

Si te saltas este paso, el token se genera igual pero Meta lo rechaza al usarlo con tu número (en Iris sale **«Al token le faltan permisos»**).`,
    },
    {
        id: ANCLAS.token,
        titulo: "6. Generar el token permanente",
        cuerpo: `No uses el token temporal de la página **Configuración de la API**: vence en 24 horas.

1. En **Usuarios del sistema**, elige el usuario y pulsa **Generar nuevo token** (Generate new token).
2. Elige tu **app**.
3. En **Caducidad del token** (Token expiration), elige **Nunca** (Never).
4. Marca los permisos \`whatsapp_business_messaging\` y \`whatsapp_business_management\`.
5. Pulsa **Generar** y **copia el token**. Meta lo muestra una sola vez: si lo pierdes, genera otro.

Pégalo directo en Iris, en el campo **Token de acceso**. No lo mandes por correo ni por chat: con él se puede escribir a tus clientes en nombre de tu empresa.`,
    },
    {
        id: ANCLAS.ids,
        titulo: "7. Encontrar el Phone number ID y el WABA ID",
        cuerpo: `En la app, abre **WhatsApp** → **Configuración de la API** (API Setup) y elige tu número en «De» (From). Debajo salen dos números largos:

- **Identificador del número de teléfono** (Phone number ID): va en **Phone number ID**.
- **Identificador de la cuenta de WhatsApp Business** (WhatsApp Business Account ID): va en **WABA ID**.

No son tu número de teléfono: son los códigos internos de Meta, solo con dígitos. Copia cada uno con el botón de copiar de Meta para no equivocarte en un dígito.`,
    },
    {
        id: ANCLAS.appSecret,
        titulo: "8. Copiar el app secret",
        cuerpo: `Iris usa el app secret para comprobar que cada mensaje que llega viene de verdad de Meta.

1. En la app, abre **Configuración de la app** → **Básica** (App settings → Basic).
2. En **Clave secreta de la app** (App secret), pulsa **Mostrar** (Meta te pide tu contraseña) y cópiala.

Si algún día la cambias en Meta («Restablecer»), cámbiala también en Iris, en **Credenciales** del número: si no, Iris descarta los mensajes que llegan.`,
    },
    {
        id: "iris",
        titulo: "9. Agregar el número en Iris",
        cuerpo: `En Iris, abre **Administración** → **Números de WhatsApp** → **Agregar número**, elige **Ingresar datos de la API** y llena:

- **Nombre**: como lo verá tu equipo en la bandeja (por ejemplo, «Soporte técnico»).
- **Número**: con \`+\`, código de país y sin espacios, por ejemplo \`+50222000040\`.
- **Phone number ID**, **WABA ID**, **Token de acceso** y **App secret** de los pasos anteriores.
- **Usos** y **Cola**: a qué se dedica el número y a qué cola llegan sus conversaciones nuevas.

Pulsa **Guardar y probar**. Iris guarda las credenciales cifradas, no las vuelve a mostrar y prueba la conexión con Meta. Después te da la **URL de devolución de llamada** y el **token de verificación** para el paso siguiente.`,
    },
    {
        id: ANCLAS.webhook,
        titulo: "10. Conectar el webhook en Meta",
        cuerpo: `El webhook es la dirección a la que Meta le entrega a Iris los mensajes de tus clientes.

1. En la app de Meta, abre **WhatsApp** → **Configuración** (Configuration).
2. En **Webhook**, pulsa **Editar** (Edit).
3. Pega la **URL de devolución de llamada** (Callback URL) y el **token de verificación** (Verify token) que te dio Iris, y pulsa **Verificar y guardar** (Verify and save).
4. En **Campos del webhook** (Webhook fields), pulsa **Administrar** (Manage) y suscríbete a \`messages\`.
5. Vuelve a Iris y pulsa **Volver a comprobar**: debe decir **Conectado**.

El token de verificación lo genera Iris y se muestra **una sola vez**. Si saliste sin copiarlo, abre **Credenciales** del número y pulsa **Generar otro** (luego pégalo en Meta y verifica otra vez).

Cuando todo funcione, revisa que la app esté **publicada** (modo «Activo» o Live, arriba en el panel de la app). Meta pide una URL de política de privacidad para publicarla, y una app en modo de desarrollo puede no recibir los mensajes de tus clientes.`,
    },
    {
        id: "pago",
        titulo: "11. Agregar el método de pago",
        cuerpo: `Meta cobra por los mensajes que **inicia tu empresa** con una plantilla (avisos, recordatorios de pago). Sin método de pago, esos mensajes no salen.

1. Abre **WhatsApp Manager** (desde [business.facebook.com](https://business.facebook.com) → **Cuentas de WhatsApp**, o el enlace de la app).
2. Busca **Configuración de la cuenta** → **Métodos de pago** (Payment settings) y agrega la tarjeta.

Los precios cambian por país y por tipo de mensaje; revísalos en la página de precios de WhatsApp de Meta. Hoy Meta no cobra las respuestas dentro de las 24 horas desde el último mensaje del cliente.`,
    },
    {
        id: "plantillas",
        titulo: "12. Crear las plantillas de mensaje",
        cuerpo: `Fuera de las 24 horas desde el último mensaje del cliente, WhatsApp solo deja enviar **plantillas aprobadas por Meta**. Iris las usa para los avisos (factura por vencer, pago recibido, corte, bienvenida…) y para iniciar una conversación.

1. En **WhatsApp Manager**, abre **Plantillas de mensajes** (Message templates) → **Crear plantilla**.
2. Elige la categoría (**Utilidad** / Utility para avisos de servicio y cobros), el idioma y un nombre en minúsculas con guiones bajos (por ejemplo \`factura_por_vencer\`).
3. Escribe el texto. Las partes que cambian van como variables (\`{{1}}\`, \`{{2}}\`…) con un ejemplo de cada una.
4. Envíala a revisión. Meta suele responder en minutos, pero puede tardar hasta un día.

Iris conoce cada plantilla por su código (por ejemplo \`factura_por_vencer\`). Usa en Meta el mismo nombre que el código para que sea fácil enlazarlas: el enlace de cada código con su plantilla de Meta aún no tiene pantalla en el panel; pídelo a quien administra Iris.`,
    },
    {
        id: ANCLAS.varios,
        titulo: "13. Varios números en la misma app",
        cuerpo: `Meta manda los mensajes de **todos** los números de una app a una sola URL. Iris le da a cada número su propia URL (\`…/api/v1/mensajeria/canales/{id}/webhook\`), así que, si la app ya tiene otro número en Iris, el segundo necesita que Meta use **su** URL (en Meta se llama \`override_callback_uri\`).

Esto se configura con la API de Meta (Graph API), no desde los menús; puede pedírselo a quien administra Iris o a un técnico:

- Por cuenta de WhatsApp: \`POST /{waba_id}/subscribed_apps\` con \`override_callback_uri\` (la URL del número en Iris) y \`verify_token\` (su token de verificación).
- O por número: \`POST /{phone_number_id}\` con \`webhook_configuration.override_callback_uri\`.

Si aun así llega a un número un mensaje de otro, Iris lo pasa al número correcto solo si es de la misma empresa y usa el mismo app secret; si no, lo descarta.

La forma más sencilla: **una app de Meta por número**.`,
    },
    {
        id: ANCLAS.estados,
        titulo: "Qué significa cada resultado de «Probar conexión»",
        cuerpo: `- **Conectado**: Meta aceptó las credenciales y verificó el webhook. El número ya recibe y envía mensajes.
- **Webhook sin verificar**: las credenciales están bien, pero Meta todavía no llamó a la URL del webhook. Haz el paso 10 y pulsa **Volver a comprobar**.
- **Token inválido o vencido** (en la lista, «Token vencido»): Meta rechazó el token. Suele ser el token temporal de 24 horas o uno que se revocó. Genera uno permanente (paso 6) y cámbialo en **Credenciales**.
- **El número no coincide**: el Phone number ID no es de ese número, o Meta no lo encuentra con ese token. Revisa el número y el Phone number ID (paso 7).
- **Al token le faltan permisos** (en la lista, «Sin permiso en Meta»): faltan \`whatsapp_business_messaging\` o \`whatsapp_business_management\`, o el usuario del sistema no tiene la cuenta de WhatsApp (pasos 5 y 6).
- **Meta no respondió**: un problema pasajero de Meta o de la red. Las credenciales quedan guardadas; vuelve a comprobar en un momento.
- **Sin probar**: nunca se probó, o cambiaste el token, el número o el Phone number ID después de la última prueba.
- **Desactivado**: Iris no envía ni recibe por ese número. Sus conversaciones se quedan.`,
    },
    {
        id: ANCLAS.errores,
        titulo: "Errores comunes",
        cuerpo: `- **«Verificar y guardar» falla en Meta.** Revisa que pegaste la URL completa y el token de verificación sin espacios, que Iris se abre desde internet con https y que el token es el último que generó Iris (si pulsaste **Generar otro**, el anterior ya no sirve).
- **Meta verificó, pero no llegan mensajes** («Último mensaje» en **Credenciales** no cambia). Falta suscribirse a \`messages\` en **Campos del webhook**, la app sigue en modo de desarrollo, la app tiene otro número y Meta manda todo a la URL de ese (paso 13), o el app secret de Iris no es el de la app (por ejemplo, se restableció en Meta): Iris descarta lo que no viene firmado con él. Cámbialo en **Credenciales**.
- **El número ya está en WhatsApp.** Meta no deja agregar a la API un número activo en la app de WhatsApp o WhatsApp Business. Borra esa cuenta en la app o conéctalo con QR (al final de esta guía).
- **Los avisos no salen y las respuestas sí.** Falta el método de pago (paso 11) o la plantilla no está aprobada (paso 12).
- **«Ya existe un número con ese Phone number ID».** Ese Phone number ID ya está en otro número activo de Iris. Desactiva el anterior o corrige el ID.`,
    },
    {
        id: ANCLAS.qr,
        titulo: "Conectar un número que ya usas en WhatsApp Business (QR)",
        cuerpo: `Si tu número ya está en la app **WhatsApp Business** del teléfono, Meta tiene una forma oficial de usarlo también en Iris sin borrarlo de la app: la **coexistencia**. Se conecta escaneando un código QR con la app, dentro del registro de Meta.

1. Un administrador configura una vez la **Conexión con Meta** (Administración → Conexión con Meta): una app propia del ISP aprobada por Meta como Tech Provider, o el servicio central de Horus, **Iris Cloud** (todavía no disponible).
2. En **Agregar número**, elige **Conectar con QR**, lee lo que cambia en la app del teléfono y pulsa **Conectar con Meta**.
3. En la ventana de Meta, entra con tu Facebook, elige tu portafolio, escribe el número y elige «Conectar tu app WhatsApp Business». Confirma en el teléfono con el QR o el código.
4. De vuelta en Iris, puedes **traer el historial** (contactos y mensajes de hasta 180 días). Meta lo permite una vez y solo en las 24 horas después de conectar.

Lo que cambia (lo pone Meta): la app sigue funcionando y lo que mandes desde el teléfono también aparece en Iris («Desde el teléfono»); los grupos no pasan; las listas de difusión quedan de solo lectura; se apagan «ver una vez», ubicación en tiempo real y mensajes temporales en los chats 1:1; y si no abres la app en el teléfono por unos **14 días**, Meta desconecta el número (en la lista sale «Desconectado por Meta» con «Reconectar»).

Si la conexión con Meta no está lista, conecta el número con los datos de la API, como explica esta guía.`,
    },
];
