import type { ClavePagina, Idioma } from "./paginas";
import type { Titular } from "./titular";

/*
 * Texto de las páginas legales públicas. Es un texto base escrito a partir de lo que Iris hace de
 * verdad (contratos/legal.md §5 dice de dónde sale cada afirmación); no es asesoría legal y cada
 * titular debe hacerlo revisar. Los datos del titular llegan por parámetro: aquí no hay nombres,
 * correos ni países escritos.
 *
 * Regla del texto: no afirma como automático lo que hoy hace una persona, ni promete funciones que
 * Iris no tiene. La eliminación de datos la atiende una persona de la empresa (no hay una acción
 * del sistema que borre), y las llamadas hoy no se graban ni se transcriben.
 *
 * Si cambias el texto, sube `TEXTO_REVISADO_EN` (titular.ts): es parte de la «última actualización».
 */

export type Bloque = string | { lista: readonly string[] };
export type Seccion = { id: string; titulo: string; bloques: readonly Bloque[] };
export type Documento = { resumen: readonly string[]; secciones: readonly Seccion[] };

/** Palabras con que un cliente deja de recibir avisos por WhatsApp (mensajeria: `palabrasBaja`). */
export const PALABRAS_BAJA = ["BAJA", "ALTO"] as const;

/** Plazos que el texto promete (días). Son compromisos del titular: revisarlos con su asesor. */
export const DIAS_ACUSE = 5;
export const DIAS_RESPUESTA = 30;

const mayuscula = (s: string) => s.charAt(0).toUpperCase() + s.slice(1);

type Ayudas = {
    /** Nombre del titular a mitad de frase. */
    n: string;
    /** Nombre del titular al inicio de frase. */
    N: string;
    /** Cómo escribirle al titular («escribiendo a x@y» o el canal de atención). */
    medio: string;
    ley: string;
};

function ayudas(t: Titular, idioma: Idioma): Ayudas {
    const es = idioma === "es";
    return {
        n: t.nombre,
        N: t.conNombre ? t.nombre : mayuscula(t.nombre),
        medio: t.correo
            ? es
                ? `escribiendo a ${t.correo}`
                : `by writing to ${t.correo}`
            : es
              ? "por el mismo canal en que te atendemos (por ejemplo, el chat de WhatsApp)"
              : "through the same channel we use to assist you (for example, the WhatsApp chat)",
        ley: t.pais
            ? es
                ? `las leyes de ${t.pais}`
                : `the laws of ${t.pais}`
            : es
              ? "las leyes del país donde prestamos el servicio"
              : "the laws of the country where we provide the service",
    };
}

// --- Español ------------------------------------------------------------------------------------

function privacidadEs(t: Titular): Documento {
    const a = ayudas(t, "es");
    const responsable = t.razonSocial ? `${t.razonSocial} («${a.n}»)` : a.N;
    return {
        resumen: [
            `${responsable} es responsable de los datos personales que se tratan cuando nos escribes o nos llamas por WhatsApp, nos envías un correo, usas el portal de soporte o recibes una visita técnica.`,
            "Para atenderte usamos Iris, nuestro sistema de atención al cliente y operación de campo, conectado a la Plataforma de WhatsApp Business de Meta. Esta política explica qué datos tratamos, para qué, con quién los compartimos y cómo puedes pedir que los corrijamos o eliminemos.",
        ],
        secciones: [
            {
                id: "datos",
                titulo: "Qué datos tratamos",
                bloques: [
                    "Tratamos solo los datos que hacen falta para atenderte:",
                    {
                        lista: [
                            "Identificación y contacto: tu nombre, tu número de teléfono (el de WhatsApp) y tu correo electrónico.",
                            "Datos de tu servicio, si eres cliente: dirección y ubicación de la instalación, plan contratado y estado del servicio. Vienen de nuestro sistema de gestión y facturación.",
                            "Mensajes: lo que nos escribes por WhatsApp o por correo y lo que te respondemos, incluidos los archivos que envías (fotos, audios, documentos y ubicaciones).",
                            "Llamadas por WhatsApp: fecha, duración y resultado de la llamada. No grabamos ni transcribimos las llamadas.",
                            "Casos de soporte y visitas técnicas: el historial de tus tickets y órdenes de trabajo, las fotos de la instalación, los materiales y equipos usados y la firma con que recibes el trabajo.",
                            "Pagos: los comprobantes (boletas) que nos envías y las promesas de pago que registras. El sistema de atención no procesa pagos con tarjeta ni guarda datos de tarjetas.",
                            "Encuestas de satisfacción: la calificación y el comentario que decidas dejar.",
                            "Tu preferencia sobre recibir avisos: si aceptaste o pediste dejar de recibirlos, y cuándo.",
                        ],
                    },
                    "No pedimos por estos canales datos sensibles que no hagan falta para tu servicio. Si para confirmar tu identidad te pedimos un número de documento, se usa solo para compararlo con el que ya tenemos.",
                ],
            },
            {
                id: "finalidades",
                titulo: "Para qué los usamos",
                bloques: [
                    {
                        lista: [
                            "Responder tus consultas y resolver problemas de tu servicio.",
                            "Abrir y dar seguimiento a tickets, y agendar y realizar visitas técnicas.",
                            "Enviarte avisos sobre tu servicio por WhatsApp (por ejemplo, citas, cortes programados o recordatorios de pago), si aceptaste recibirlos.",
                            "Registrar los comprobantes y las promesas de pago que nos envías.",
                            "Medir y mejorar la calidad de la atención, y prevenir abusos y fraudes.",
                            "Cumplir las obligaciones legales, contables y fiscales que nos aplican.",
                        ],
                    },
                    "No vendemos tus datos ni los usamos para mostrarte publicidad de terceros.",
                ],
            },
            {
                id: "base",
                titulo: "Con qué base los tratamos",
                bloques: [
                    {
                        lista: [
                            "Tu consentimiento: cuando nos escribes primero o aceptas recibir avisos. Puedes retirarlo cuando quieras.",
                            "La relación contractual: para prestarte el servicio que contrataste y darte soporte.",
                            "Obligaciones legales: por ejemplo, conservar registros de facturación y pagos.",
                            "Nuestro interés legítimo en mantener la seguridad del servicio y mejorar la atención.",
                        ],
                    },
                ],
            },
            {
                id: "asistente",
                titulo: "Asistente virtual e inteligencia artificial",
                bloques: [
                    "Parte de la atención puede darla un asistente virtual. Está configurado para presentarse como asistente virtual y, si le preguntas, para decirte que no es una persona.",
                    "Puedes pedir hablar con una persona en cualquier momento: basta con escribirlo y la conversación pasa a nuestro equipo.",
                    "Para redactar sus respuestas, el contenido de la conversación y los datos de tu servicio que hagan falta se envían al proveedor de inteligencia artificial que tengamos configurado, que puede ser un servicio de un tercero o un servidor propio. El asistente no aprueba ni rechaza pagos: eso lo decide una persona o el sistema de facturación.",
                ],
            },
            {
                id: "terceros",
                titulo: "Con quién los compartimos",
                bloques: [
                    {
                        lista: [
                            "Meta Platforms, como proveedor de la Plataforma de WhatsApp Business: los mensajes y las llamadas viajan por su servicio, que tiene sus propias condiciones y política de privacidad.",
                            "El proveedor de inteligencia artificial, solo cuando el asistente virtual está activo y solo lo necesario para responder.",
                            "Nuestros propios sistemas de gestión y facturación y de monitoreo de red (ArgosISP y Horus), con los que Iris intercambia los datos de tu servicio.",
                            "Los proveedores que alojan o dan mantenimiento a nuestros sistemas, bajo obligación de confidencialidad.",
                            "Las autoridades, cuando una ley o una orden válida nos obliga.",
                        ],
                    },
                    "Algunos de estos proveedores pueden tratar los datos en otros países.",
                ],
            },
            {
                id: "conservacion",
                titulo: "Cuánto tiempo los conservamos",
                bloques: [
                    {
                        lista: [
                            "Conversaciones, tickets y órdenes de trabajo: mientras exista la relación contigo y, después, el tiempo que haga falta para atender reclamos y cumplir obligaciones legales. No tienen un borrado automático: cuando lo pides, una persona de nuestro equipo los elimina o anonimiza, como explica la página «Eliminación de datos».",
                            "Registros de facturación y pagos: los plazos que exige la ley.",
                        ],
                    },
                ],
            },
            {
                id: "seguridad",
                titulo: "Cómo los protegemos",
                bloques: [
                    {
                        lista: [
                            "Conexiones cifradas (HTTPS).",
                            "Cada persona de nuestro equipo entra con su propio usuario y solo ve lo que su función necesita.",
                            "Contraseñas guardadas con un algoritmo de un solo sentido y credenciales de servicios guardadas cifradas.",
                            "Registro de quién hizo cada acción en el sistema.",
                        ],
                    },
                    "Ningún sistema es infalible. Si ocurre un incidente que afecte tus datos, te avisaremos como lo exija la ley.",
                ],
            },
            {
                id: "derechos",
                titulo: "Tus derechos",
                bloques: [
                    "Puedes pedirnos acceso a tus datos, que los corrijamos, que los eliminemos, oponerte a un uso concreto o retirar tu consentimiento.",
                    `Para hacerlo, contáctanos ${a.medio}. Podemos pedirte que confirmes tu identidad. Respondemos en un máximo de ${DIAS_RESPUESTA} días.`,
                    "El detalle de cómo pedir la eliminación está en la página «Eliminación de datos».",
                ],
            },
            {
                id: "baja",
                titulo: "Dejar de recibir mensajes",
                bloques: [
                    `Si no quieres recibir más avisos por WhatsApp, responde ${PALABRAS_BAJA.join(" o ")} en el chat. Queda registrado de inmediato y dejamos de enviártelos.`,
                    "Podrás seguir escribiéndonos cuando necesites ayuda: la baja solo detiene los avisos que iniciamos nosotros.",
                ],
            },
            {
                id: "cookies",
                titulo: "Cookies",
                bloques: [
                    "El portal de soporte usa solo las cookies necesarias para mantener tu sesión y protegerla. No usamos cookies de publicidad ni de seguimiento de terceros.",
                ],
            },
            {
                id: "menores",
                titulo: "Menores de edad",
                bloques: [
                    "Nuestros servicios se contratan por personas mayores de edad. No recopilamos a sabiendas datos de menores; si crees que tenemos datos de un menor, escríbenos y una persona de nuestro equipo los eliminará.",
                ],
            },
            {
                id: "cambios",
                titulo: "Cambios a esta política",
                bloques: [
                    "Si cambiamos esta política, publicaremos aquí la versión nueva con su fecha. Si el cambio es importante, te avisaremos por los canales de atención.",
                ],
            },
        ],
    };
}

function terminosEs(t: Titular): Documento {
    const a = ayudas(t, "es");
    return {
        resumen: [
            `Estos términos regulan el uso de los canales de atención de ${a.n}: mensajes y llamadas por WhatsApp, correo, portal de soporte y visitas técnicas. Al usarlos, los aceptas.`,
            "El servicio de internet que contrataste se rige por tu contrato; estos términos solo tratan de cómo te atendemos.",
        ],
        secciones: [
            {
                id: "servicio",
                titulo: "Qué ofrecemos",
                bloques: [
                    {
                        lista: [
                            "Atención por WhatsApp (mensajes y, donde esté disponible, llamadas), por correo y por el portal de soporte.",
                            "Registro y seguimiento de tus casos de soporte.",
                            "Programación de visitas técnicas: instalación, revisión, cambio o retiro de equipo.",
                            "Avisos sobre tu servicio, si aceptaste recibirlos.",
                        ],
                    },
                ],
            },
            {
                id: "uso",
                titulo: "Uso adecuado",
                bloques: [
                    "Te pedimos dar información verdadera, tratar con respeto a nuestro equipo y no usar estos canales para fines ilícitos, para enviar contenido ofensivo o para saturarlos.",
                    "Ante un uso abusivo podemos limitar o bloquear la atención a un número.",
                ],
            },
            {
                id: "horarios",
                titulo: "Horarios y tiempos de respuesta",
                bloques: [
                    "Atendemos en el horario que informamos por nuestros canales. Fuera de ese horario puedes escribirnos igual: puedes recibir una respuesta automática, y una persona te atenderá cuando vuelva a haber atención.",
                    "Hacemos lo posible por responder pronto, pero los tiempos de respuesta no son una garantía.",
                ],
            },
            {
                id: "asistente",
                titulo: "Asistente virtual",
                bloques: [
                    "Es posible que te responda un asistente virtual basado en inteligencia artificial. Está configurado para presentarse como tal y para decirte, si se lo preguntas, que no es una persona.",
                    "Puedes pedir hablar con una persona en cualquier momento escribiéndolo en el chat.",
                    "Las respuestas automáticas pueden contener errores. Lo que decide tu servicio o tus pagos (por ejemplo, aprobar un comprobante) lo confirma una persona o nuestro sistema de facturación, no el asistente.",
                ],
            },
            {
                id: "mensajes",
                titulo: "Mensajes y avisos",
                bloques: [
                    "Si nos escribes, te respondemos por el mismo canal. Los avisos que iniciamos nosotros solo se envían si aceptaste recibirlos.",
                    `Para dejar de recibir avisos, responde ${PALABRAS_BAJA.join(" o ")} en el chat.`,
                ],
            },
            {
                id: "llamadas",
                titulo: "Llamadas",
                bloques: [
                    "Para llamarte por WhatsApp te pedimos permiso antes; puedes negarlo o retirarlo desde WhatsApp.",
                    "No grabamos las llamadas.",
                ],
            },
            {
                id: "pagos",
                titulo: "Comprobantes de pago",
                bloques: [
                    "Si envías un comprobante de pago por el chat, queda registrado para revisión. El pago se considera aplicado cuando lo confirma nuestro equipo o el sistema de facturación, y te informamos el resultado.",
                ],
            },
            {
                id: "visitas",
                titulo: "Visitas técnicas",
                bloques: [
                    "Las visitas se programan en la fecha y la franja horaria que acordemos contigo. Al terminar, el técnico puede tomar fotos de la instalación y pedirte una firma de recibido.",
                ],
            },
            {
                id: "terceros",
                titulo: "Servicios de terceros",
                bloques: [
                    "WhatsApp es un servicio de Meta. Su uso se rige además por las condiciones y la política de privacidad de WhatsApp, y su disponibilidad no depende de nosotros.",
                ],
            },
            {
                id: "responsabilidad",
                titulo: "Disponibilidad y responsabilidad",
                bloques: [
                    "Procuramos que los canales de atención estén disponibles, pero pueden interrumpirse por mantenimiento o por causas fuera de nuestro control.",
                    "Nada de lo dicho aquí limita los derechos que la ley te reconoce como consumidor.",
                ],
            },
            {
                id: "privacidad",
                titulo: "Privacidad",
                bloques: ["El tratamiento de tus datos se explica en la «Política de privacidad», y cómo pedir que los borremos, en «Eliminación de datos»."],
            },
            {
                id: "cambios",
                titulo: "Cambios a estos términos",
                bloques: ["Podemos actualizar estos términos. Publicaremos aquí la versión nueva con su fecha; si sigues usando los canales de atención, se entiende que la aceptas."],
            },
            {
                id: "ley",
                titulo: "Ley aplicable",
                bloques: [`Estos términos se rigen por ${a.ley}. Cualquier diferencia se resolverá ante las autoridades y los tribunales competentes de esa jurisdicción.`],
            },
        ],
    };
}

function eliminacionEs(t: Titular): Documento {
    const a = ayudas(t, "es");
    return {
        resumen: [
            `Puedes pedir en cualquier momento que ${a.n} elimine los datos personales que tiene de ti por haber usado sus canales de atención (WhatsApp, correo, portal de soporte y visitas técnicas). No tiene costo.`,
            "La eliminación no es automática: tu solicitud la atiende una persona de nuestro equipo, que elimina o anonimiza tus datos dentro del plazo que se indica abajo.",
        ],
        secciones: [
            {
                id: "como",
                titulo: "Cómo pedir la eliminación",
                bloques: [
                    {
                        lista: [
                            t.correo
                                ? `Por correo: escribe a ${t.correo} con el asunto «Eliminación de datos».`
                                : "Por escrito, por el mismo canal en que te atendemos, con el asunto «Eliminación de datos».",
                            "Por WhatsApp: escribe en el chat que quieres eliminar tus datos y pide hablar con una persona. La solicitud la atiende una persona de nuestro equipo, no el asistente virtual.",
                        ],
                    },
                    "Indica tu nombre completo, el número de teléfono con el que nos escribes y, si eres cliente, tu número de cliente o la dirección del servicio. Con eso encontramos tus datos.",
                ],
            },
            {
                id: "verificacion",
                titulo: "Confirmamos que eres tú",
                bloques: [
                    "Antes de borrar nada podemos pedirte que confirmes tu identidad, por ejemplo respondiendo desde el mismo número de WhatsApp. Así evitamos que otra persona borre tus datos.",
                ],
            },
            {
                id: "que-se-borra",
                titulo: "Qué se elimina",
                bloques: [
                    "La persona que atiende tu solicitud elimina o anonimiza:",
                    {
                        lista: [
                            "Tus conversaciones por WhatsApp y por correo con nosotros y los archivos que enviaste.",
                            "Tu ficha de contacto (nombre, teléfono y correo), si no eres cliente.",
                            "Tus respuestas a encuestas de satisfacción.",
                        ],
                    },
                    "Cuando un dato no se puede borrar sin romper un registro que debemos conservar, se anonimiza: queda sin tu nombre ni tus datos de contacto.",
                ],
            },
            {
                id: "que-se-conserva",
                titulo: "Qué debemos conservar",
                bloques: [
                    {
                        lista: [
                            "Si eres o fuiste cliente: los registros de tu contrato, facturación y pagos, y las órdenes de trabajo relacionadas, por los plazos que exige la ley.",
                            "La constancia de que pediste no recibir mensajes, para respetarla.",
                            "Los registros internos de seguridad (quién hizo qué en el sistema), sin el contenido de tus mensajes.",
                            "Lo que haga falta para atender un reclamo o un proceso legal en curso.",
                        ],
                    },
                    "Los datos que estén en copias de seguridad permanecen hasta que esas copias se reemplacen; mientras tanto no se usan para nada más.",
                ],
            },
            {
                id: "plazos",
                titulo: "Plazos",
                bloques: [
                    `Una persona de nuestro equipo te confirma que recibió tu solicitud en un máximo de ${DIAS_ACUSE} días hábiles y la completa en un máximo de ${DIAS_RESPUESTA} días. Te avisa cuando esté hecha y, si algo no se pudo borrar, te dice qué y por qué.`,
                ],
            },
            {
                id: "whatsapp",
                titulo: "Lo que queda en WhatsApp",
                bloques: [
                    "Los mensajes que están en tu teléfono y en tu cuenta de WhatsApp no los controlamos nosotros: puedes borrar el chat desde la aplicación. El tratamiento que hace WhatsApp se rige por su propia política.",
                ],
            },
            {
                id: "solo-avisos",
                titulo: "Si solo quieres dejar de recibir mensajes",
                bloques: [`No hace falta eliminar tus datos: responde ${PALABRAS_BAJA.join(" o ")} en el chat de WhatsApp y dejamos de enviarte avisos.`],
            },
        ],
    };
}

// --- English ------------------------------------------------------------------------------------

function privacidadEn(t: Titular): Documento {
    const a = ayudas(t, "en");
    const responsable = t.razonSocial ? `${t.razonSocial} ("${a.n}")` : a.N;
    return {
        resumen: [
            `${responsable} is responsible for the personal data processed when you message or call us on WhatsApp, send us an email, use the support portal or receive a technical visit.`,
            "To assist you we use Iris, our customer service and field operations system, connected to Meta's WhatsApp Business Platform. This policy explains what data we process, why, who we share it with and how you can ask us to correct or delete it.",
        ],
        secciones: [
            {
                id: "datos",
                titulo: "What data we process",
                bloques: [
                    "We only process the data needed to assist you:",
                    {
                        lista: [
                            "Identification and contact details: your name, your phone number (your WhatsApp number) and your email address.",
                            "Service details, if you are a customer: installation address and location, plan and service status. They come from our management and billing system.",
                            "Messages: what you write to us on WhatsApp or by email and what we reply, including the files you send (photos, audio, documents and locations).",
                            "WhatsApp calls: date, duration and outcome of the call. We do not record or transcribe calls.",
                            "Support cases and technical visits: the history of your tickets and work orders, photos of the installation, materials and equipment used, and the signature you give when the work is done.",
                            "Payments: the payment receipts you send us and the payment promises you register. The customer service system does not process card payments or store card details.",
                            "Satisfaction surveys: the rating and comment you choose to leave.",
                            "Your preference about receiving notifications: whether you accepted or opted out, and when.",
                        ],
                    },
                    "We do not ask through these channels for sensitive data that is not needed for your service. If we ask for an ID number to confirm your identity, it is only compared with the one we already hold.",
                ],
            },
            {
                id: "finalidades",
                titulo: "Why we use it",
                bloques: [
                    {
                        lista: [
                            "To answer your questions and solve problems with your service.",
                            "To open and follow up support tickets, and to schedule and carry out technical visits.",
                            "To send you notifications about your service on WhatsApp (for example appointments, planned outages or payment reminders), if you agreed to receive them.",
                            "To register the payment receipts and payment promises you send us.",
                            "To measure and improve service quality, and to prevent abuse and fraud.",
                            "To comply with our legal, accounting and tax obligations.",
                        ],
                    },
                    "We do not sell your data or use it to show you third-party advertising.",
                ],
            },
            {
                id: "base",
                titulo: "Legal basis",
                bloques: [
                    {
                        lista: [
                            "Your consent: when you write to us first or agree to receive notifications. You can withdraw it at any time.",
                            "The contractual relationship: to provide the service you hired and to support it.",
                            "Legal obligations: for example, keeping billing and payment records.",
                            "Our legitimate interest in keeping the service secure and improving customer service.",
                        ],
                    },
                ],
            },
            {
                id: "asistente",
                titulo: "Virtual assistant and artificial intelligence",
                bloques: [
                    "Part of our customer service may be provided by a virtual assistant. It is configured to introduce itself as a virtual assistant and, if you ask, to tell you that it is not a person.",
                    "You can ask to talk to a person at any time: just say so and the conversation is handed over to our team.",
                    "To write its replies, the content of the conversation and the service details that are needed are sent to the artificial intelligence provider we have configured, which may be a third-party service or a server of our own. The assistant does not approve or reject payments: a person or the billing system decides that.",
                ],
            },
            {
                id: "terceros",
                titulo: "Who we share it with",
                bloques: [
                    {
                        lista: [
                            "Meta Platforms, as the provider of the WhatsApp Business Platform: messages and calls travel through its service, which has its own terms and privacy policy.",
                            "The artificial intelligence provider, only when the virtual assistant is active and only what is needed to reply.",
                            "Our own management and billing system and network monitoring system (ArgosISP and Horus), with which Iris exchanges your service details.",
                            "The providers that host or maintain our systems, under confidentiality obligations.",
                            "Authorities, when a law or a valid order requires it.",
                        ],
                    },
                    "Some of these providers may process data in other countries.",
                ],
            },
            {
                id: "conservacion",
                titulo: "How long we keep it",
                bloques: [
                    {
                        lista: [
                            'Conversations, tickets and work orders: for as long as our relationship with you lasts and, afterwards, for as long as needed to handle claims and meet legal obligations. They are not deleted automatically: when you ask, a member of our team deletes or anonymizes them, as explained on the "Data Deletion" page.',
                            "Billing and payment records: for the periods required by law.",
                        ],
                    },
                ],
            },
            {
                id: "seguridad",
                titulo: "How we protect it",
                bloques: [
                    {
                        lista: [
                            "Encrypted connections (HTTPS).",
                            "Each member of our team signs in with their own account and only sees what their role needs.",
                            "Passwords stored with a one-way algorithm and service credentials stored encrypted.",
                            "A record of who did what in the system.",
                        ],
                    },
                    "No system is infallible. If an incident affects your data, we will notify you as required by law.",
                ],
            },
            {
                id: "derechos",
                titulo: "Your rights",
                bloques: [
                    "You can ask us for access to your data, to correct it, to delete it, to object to a specific use or to withdraw your consent.",
                    `To do so, contact us ${a.medio}. We may ask you to confirm your identity. We reply within ${DIAS_RESPUESTA} days at most.`,
                    'How to request deletion is detailed on the "Data Deletion" page.',
                ],
            },
            {
                id: "baja",
                titulo: "Opting out of messages",
                bloques: [
                    `If you no longer want to receive notifications on WhatsApp, reply ${PALABRAS_BAJA.join(" or ")} in the chat (these are the Spanish words for "unsubscribe" and "stop"). It is recorded immediately and we stop sending them.`,
                    "You can still write to us whenever you need help: opting out only stops the notifications we initiate.",
                ],
            },
            {
                id: "cookies",
                titulo: "Cookies",
                bloques: ["The support portal only uses the cookies needed to keep your session open and protect it. We do not use advertising or third-party tracking cookies."],
            },
            {
                id: "menores",
                titulo: "Minors",
                bloques: ["Our services are contracted by adults. We do not knowingly collect data from minors; if you believe we hold data about a minor, write to us and a member of our team will delete it."],
            },
            {
                id: "cambios",
                titulo: "Changes to this policy",
                bloques: ["If we change this policy, we will publish the new version here with its date. If the change is significant, we will let you know through our service channels."],
            },
        ],
    };
}

function terminosEn(t: Titular): Documento {
    const a = ayudas(t, "en");
    return {
        resumen: [
            `These terms govern the use of the customer service channels of ${a.n}: WhatsApp messages and calls, email, the support portal and technical visits. By using them, you accept these terms.`,
            "The internet service you hired is governed by your contract; these terms only cover how we assist you.",
        ],
        secciones: [
            {
                id: "servicio",
                titulo: "What we offer",
                bloques: [
                    {
                        lista: [
                            "Customer service on WhatsApp (messages and, where available, calls), by email and through the support portal.",
                            "Registration and follow-up of your support cases.",
                            "Scheduling of technical visits: installation, inspection, replacement or removal of equipment.",
                            "Notifications about your service, if you agreed to receive them.",
                        ],
                    },
                ],
            },
            {
                id: "uso",
                titulo: "Acceptable use",
                bloques: [
                    "We ask you to provide truthful information, to treat our team with respect and not to use these channels for unlawful purposes, to send offensive content or to flood them.",
                    "In case of abusive use we may limit or block service to a number.",
                ],
            },
            {
                id: "horarios",
                titulo: "Hours and response times",
                bloques: [
                    "We assist you during the hours we announce on our channels. Outside those hours you can still write to us: you may receive an automatic reply, and a person will assist you when service resumes.",
                    "We do our best to reply quickly, but response times are not guaranteed.",
                ],
            },
            {
                id: "asistente",
                titulo: "Virtual assistant",
                bloques: [
                    "You may be answered by a virtual assistant based on artificial intelligence. It is configured to introduce itself as such and to tell you, if you ask, that it is not a person.",
                    "You can ask to talk to a person at any time by saying so in the chat.",
                    "Automatic replies may contain errors. Anything that decides your service or your payments (for example, approving a payment receipt) is confirmed by a person or by our billing system, not by the assistant.",
                ],
            },
            {
                id: "mensajes",
                titulo: "Messages and notifications",
                bloques: [
                    "If you write to us, we reply on the same channel. Notifications that we initiate are only sent if you agreed to receive them.",
                    `To stop receiving notifications, reply ${PALABRAS_BAJA.join(" or ")} in the chat.`,
                ],
            },
            {
                id: "llamadas",
                titulo: "Calls",
                bloques: [
                    "Before calling you on WhatsApp we ask for your permission; you can deny or withdraw it from WhatsApp.",
                    "We do not record calls.",
                ],
            },
            {
                id: "pagos",
                titulo: "Payment receipts",
                bloques: [
                    "If you send a payment receipt through the chat, it is registered for review. The payment is considered applied once our team or the billing system confirms it, and we let you know the result.",
                ],
            },
            {
                id: "visitas",
                titulo: "Technical visits",
                bloques: ["Visits are scheduled for the date and time window we agree with you. When finished, the technician may take photos of the installation and ask you to sign for the work."],
            },
            {
                id: "terceros",
                titulo: "Third-party services",
                bloques: ["WhatsApp is a Meta service. Its use is also governed by WhatsApp's terms and privacy policy, and its availability does not depend on us."],
            },
            {
                id: "responsabilidad",
                titulo: "Availability and liability",
                bloques: [
                    "We strive to keep our service channels available, but they may be interrupted by maintenance or by causes beyond our control.",
                    "Nothing in these terms limits the rights the law grants you as a consumer.",
                ],
            },
            {
                id: "privacidad",
                titulo: "Privacy",
                bloques: ['How we process your data is explained in the "Privacy Policy", and how to ask us to delete it, in "Data Deletion".'],
            },
            {
                id: "cambios",
                titulo: "Changes to these terms",
                bloques: ["We may update these terms. We will publish the new version here with its date; if you keep using our service channels, you are deemed to accept it."],
            },
            {
                id: "ley",
                titulo: "Governing law",
                bloques: [`These terms are governed by ${a.ley}. Any dispute will be resolved before the competent authorities and courts of that jurisdiction.`],
            },
        ],
    };
}

function eliminacionEn(t: Titular): Documento {
    const a = ayudas(t, "en");
    return {
        resumen: [
            `You can ask at any time that ${a.n} delete the personal data it holds about you from your use of its customer service channels (WhatsApp, email, support portal and technical visits). It is free of charge.`,
            "Deletion is not automatic: your request is handled by a member of our team, who deletes or anonymizes your data within the timeframe stated below.",
        ],
        secciones: [
            {
                id: "como",
                titulo: "How to request deletion",
                bloques: [
                    {
                        lista: [
                            t.correo
                                ? `By email: write to ${t.correo} with the subject "Data deletion".`
                                : 'In writing, through the same channel we use to assist you, with the subject "Data deletion".',
                            "On WhatsApp: say in the chat that you want your data deleted and ask to talk to a person. The request is handled by a member of our team, not by the virtual assistant.",
                        ],
                    },
                    "Include your full name, the phone number you write to us from and, if you are a customer, your customer number or the service address. That lets us find your data.",
                ],
            },
            {
                id: "verificacion",
                titulo: "We confirm it is you",
                bloques: ["Before deleting anything we may ask you to confirm your identity, for example by replying from the same WhatsApp number. This prevents someone else from deleting your data."],
            },
            {
                id: "que-se-borra",
                titulo: "What is deleted",
                bloques: [
                    "The person handling your request deletes or anonymizes:",
                    {
                        lista: [
                            "Your WhatsApp and email conversations with us and the files you sent.",
                            "Your contact record (name, phone and email), if you are not a customer.",
                            "Your answers to satisfaction surveys.",
                        ],
                    },
                    "When a piece of data cannot be deleted without breaking a record we must keep, it is anonymized: it is left without your name or contact details.",
                ],
            },
            {
                id: "que-se-conserva",
                titulo: "What we must keep",
                bloques: [
                    {
                        lista: [
                            "If you are or were a customer: the records of your contract, billing and payments, and the related work orders, for the periods required by law.",
                            "The record that you opted out of messages, so that we can honor it.",
                            "Internal security records (who did what in the system), without the content of your messages.",
                            "Whatever is needed to handle an ongoing claim or legal proceeding.",
                        ],
                    },
                    "Data held in backups remains until those backups are replaced; meanwhile it is not used for anything else.",
                ],
            },
            {
                id: "plazos",
                titulo: "Timeframes",
                bloques: [
                    `A member of our team acknowledges your request within ${DIAS_ACUSE} business days at most and completes it within ${DIAS_RESPUESTA} days at most. They let you know when it is done and, if something could not be deleted, they tell you what and why.`,
                ],
            },
            {
                id: "whatsapp",
                titulo: "What stays on WhatsApp",
                bloques: ["We do not control the messages on your phone and in your WhatsApp account: you can delete the chat from the app. WhatsApp's own processing is governed by its own policy."],
            },
            {
                id: "solo-avisos",
                titulo: "If you only want to stop receiving messages",
                bloques: [`You do not need to delete your data: reply ${PALABRAS_BAJA.join(" or ")} in the WhatsApp chat and we will stop sending you notifications.`],
            },
        ],
    };
}

const DOCUMENTOS: Record<ClavePagina, Record<Idioma, (t: Titular) => Documento>> = {
    privacidad: { es: privacidadEs, en: privacidadEn },
    terminos: { es: terminosEs, en: terminosEn },
    eliminacion: { es: eliminacionEs, en: eliminacionEn },
};

export function documentoDe(clave: ClavePagina, idioma: Idioma, titular: Titular): Documento {
    return DOCUMENTOS[clave][idioma](titular);
}
