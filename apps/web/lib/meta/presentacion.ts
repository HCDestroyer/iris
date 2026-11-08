import type { Tono } from "@iris/ui";
import type { CanalMeta, ClaveComprobacion, ConexionMeta, EstadoSincronizacion, MotivoQr, Sincronizacion } from "./tipos";

/* Textos de la conexión con Meta, el QR y los préstamos (contratos/conversaciones.md §15). */

export const SERVICIO_CENTRAL = { nombre: "Iris Cloud", url: "https://cloud.iris.kns.gt" } as const;

export const NOMBRE_COMPROBACION: Record<ClaveComprobacion, string> = {
    app: "App ID y app secret",
    configuracion: "ID de configuración",
    webhook: "Webhook de la app",
    tech_provider: "Tech Provider con acceso avanzado",
};

/** Encabezado del estado de la conexión. */
export function presentarConexionMeta(c: Pick<ConexionMeta, "estado" | "modo" | "comprobaciones">): { titulo: string; texto: string; tono: Tono } {
    switch (c.estado) {
        case "sin_configurar":
            return { titulo: "Sin configurar", texto: "Todavía no se puede conectar números con QR.", tono: "neutral" };
        case "sin_probar":
            return { titulo: "Sin probar", texto: "Guardada. Pruébala para saber si Meta acepta los datos.", tono: "neutral" };
        case "error":
            return { titulo: "Meta no aceptó la conexión", texto: "Revisa lo que está en rojo y vuelve a probar.", tono: "error" };
        case "sin_servicio_central":
            return {
                titulo: "Servicio central no disponible todavía",
                texto: `${SERVICIO_CENTRAL.nombre} aún no está en servicio. La configuración queda guardada; mientras tanto, el QR no está disponible.`,
                tono: "warning",
            };
        case "gestionada":
            return { titulo: "Funcionando", texto: `La administra ${SERVICIO_CENTRAL.nombre}.`, tono: "success" };
        default: {
            const pendiente = c.comprobaciones.some((x) => x.estado !== "ok");
            return pendiente
                ? { titulo: "Lista, falta confirmar la aprobación de Meta", texto: "Los datos están bien. Iris no puede saber si tu app ya es Tech Provider: si no lo es, el QR falla al final.", tono: "warning" }
                : { titulo: "Lista", texto: "Meta aceptó los datos de la app.", tono: "success" };
        }
    }
}

export function tonoComprobacion(estado: ConexionMeta["comprobaciones"][number]["estado"]): Tono {
    return estado === "ok" ? "success" : estado === "error" ? "error" : estado === "advertencia" ? "warning" : "neutral";
}

/** Por qué no se puede conectar con QR (`GET /conexion-meta/registro`). */
export function textoMotivoQr(motivo: MotivoQr | null): string {
    if (motivo === "servicio_central_no_disponible") return `Servicio central no disponible todavía: ${SERVICIO_CENTRAL.nombre} aún no está en servicio.`;
    return "Falta configurar la conexión con Meta.";
}

export function etiquetaModo(modo: ConexionMeta["modo"]): string {
    return modo === "propia" ? "app propia" : modo === "compartida" ? `app compartida de ${SERVICIO_CENTRAL.nombre}` : modo === "saas" ? SERVICIO_CENTRAL.nombre : "sin configurar";
}

// --- Errores del QR ----------------------------------------------------------------------------

export type ErrorQr = {
    titulo: string;
    texto: string;
    revisa?: readonly string[];
    /** «Usar datos de la API» como salida. */
    ofrecerApi: boolean;
    /** Ir a la conexión con Meta (si se tiene el permiso). */
    ofrecerConexion: boolean;
};

const ERRORES_QR: Record<string, ErrorQr> = {
    cancelado: {
        titulo: "Cerraste la ventana de Meta",
        texto: "No se conectó nada. Cuando quieras, vuelve a empezar.",
        ofrecerApi: false,
        ofrecerConexion: false,
    },
    codigo_vencido: {
        titulo: "El permiso de Meta venció",
        texto: "Meta da unos segundos para terminar. Vuelve a empezar y no cierres esta página mientras confirmas en el teléfono.",
        ofrecerApi: false,
        ofrecerConexion: false,
    },
    app_no_aprobada: {
        titulo: "La app de Meta no tiene permiso para el QR",
        texto: "Para conectar con QR, la app de la conexión con Meta debe ser Tech Provider aprobado, con acceso avanzado a whatsapp_business_messaging y whatsapp_business_management.",
        ofrecerApi: true,
        ofrecerConexion: true,
    },
    numero_no_elegible: {
        titulo: "Meta dice que este número no se puede conectar con QR",
        texto: "El número no cumple lo que pide Meta. Revisa:",
        revisa: ["La app es WhatsApp Business 2.24.17 o más reciente (no la personal).", "El número está activo en esa app desde hace unos días.", "No está conectado ya a otra plataforma o a la API."],
        ofrecerApi: true,
        ofrecerConexion: false,
    },
    error_meta: {
        titulo: "Meta no respondió bien",
        texto: "Suele ser pasajero. Vuelve a intentarlo en un momento.",
        ofrecerApi: false,
        ofrecerConexion: false,
    },
    meta: {
        titulo: "Meta mostró un error",
        texto: "La ventana de Meta no terminó el registro.",
        ofrecerApi: false,
        ofrecerConexion: false,
    },
    sin_datos: {
        titulo: "Meta no mandó los datos del número",
        texto: "La ventana se cerró sin decir qué número elegiste. Vuelve a empezar.",
        ofrecerApi: false,
        ofrecerConexion: false,
    },
    canal_existente: {
        titulo: "Ese número ya está en Iris",
        texto: "Ya hay un número activo con ese Phone number ID. Búscalo en la lista.",
        ofrecerApi: false,
        ofrecerConexion: false,
    },
    sdk: {
        titulo: "No se pudo abrir la ventana de Meta",
        texto: "El navegador no cargó el componente de Facebook. Revisa la conexión o un bloqueador de anuncios y vuelve a intentarlo.",
        ofrecerApi: true,
        ofrecerConexion: false,
    },
    servicio_central_no_disponible: {
        titulo: "Servicio central no disponible todavía",
        texto: `${SERVICIO_CENTRAL.nombre} aún no está en servicio. Usa una app propia o los datos de la API.`,
        ofrecerApi: true,
        ofrecerConexion: true,
    },
    base_url_requerida: {
        titulo: "Falta la dirección pública de Iris",
        texto: "Falta configurar la dirección pública de Iris (IRIS_BASE_URL); pide ayuda a quien administra el servidor.",
        ofrecerApi: true,
        ofrecerConexion: false,
    },
    conexion_meta_sin_configurar: {
        titulo: "Falta configurar la conexión con Meta",
        texto: "Pide a un administrador que la configure en Administración → Conexión con Meta.",
        ofrecerApi: true,
        ofrecerConexion: true,
    },
};

/** Texto del error por su código (los de `POST /canales/qr` y los del lado del panel). */
const ERROR_META_QR: ErrorQr = { titulo: "Meta no respondió bien", texto: "Suele ser pasajero. Vuelve a intentarlo en un momento.", ofrecerApi: false, ofrecerConexion: false };

export function errorQr(codigo: string): ErrorQr {
    return ERRORES_QR[codigo] ?? ERROR_META_QR;
}

// --- Números QR --------------------------------------------------------------------------------

export function etiquetaTipoConexion(tipo: CanalMeta["tipoConexion"]): string {
    return tipo === "qr" ? "QR · WhatsApp Business" : "API";
}

const DIA_MS = 86_400_000;
/** Días desde lo último mandado desde el teléfono; avisa a partir de 7 (Meta desconecta a los ~14). */
export function diasSinTelefono(ultimoEcoEn: string | null, conectadoEn: string | null, ahora: number = Date.now()): number | null {
    const desde = ultimoEcoEn ?? conectadoEn;
    if (!desde) return null;
    const dias = Math.floor((ahora - Date.parse(desde)) / DIA_MS);
    return dias >= 7 ? dias : null;
}

export function textoDesconexion(motivo: string | null): string {
    if (motivo === "primary_inactivity") return "El teléfono pasó 14 días sin abrir la app.";
    if (motivo === "account_offboarded") return "La cuenta se quitó de la plataforma en Meta.";
    if (motivo === "partner_removed") return "Se quitó el acceso de Iris desde Meta.";
    return "Meta lo desconectó.";
}

// --- Historial (sincronización) ----------------------------------------------------------------

export function plazoRestante(plazo: string, ahora: number = Date.now()): { vencido: boolean; texto: string } {
    const ms = Date.parse(plazo) - ahora;
    if (ms <= 0) return { vencido: true, texto: "venció" };
    const min = Math.floor(ms / 60_000);
    const h = Math.floor(min / 60);
    return { vencido: false, texto: h > 0 ? `${h} h ${min % 60} min` : `${min} min` };
}

export function presentarSincronizacion(estado: EstadoSincronizacion): { texto: string; tono: Tono } {
    switch (estado) {
        case "pedido":
            return { texto: "Pedido", tono: "info" };
        case "recibiendo":
            return { texto: "Recibiendo", tono: "info" };
        case "listo":
            return { texto: "Listo", tono: "success" };
        case "rechazado":
            return { texto: "No aceptado en el teléfono", tono: "warning" };
        case "fallido":
            return { texto: "Falló", tono: "error" };
        default:
            return { texto: "Sin pedir", tono: "neutral" };
    }
}

/** Minutos sin respuesta de Meta tras los que un «pedido» se puede repetir (§15.2). */
export const MINUTOS_REPETIR_PEDIDO = 10;

/** Se puede pedir este tipo (una vez, dentro del plazo; si falló o Meta no respondió en 10 min, otra vez). */
export function sePuedePedir(s: Sincronizacion, tipo: "contactos" | "mensajes", ahora: number = Date.now()): boolean {
    if (plazoRestante(s.plazo, ahora).vencido) return false;
    const { estado, pedidoEn } = s[tipo];
    if (estado === "sin_pedir" || estado === "fallido") return true;
    if (estado !== "pedido") return false;
    const pedido = pedidoEn ? Date.parse(pedidoEn) : Number.NaN;
    return !Number.isNaN(pedido) && ahora - pedido >= MINUTOS_REPETIR_PEDIDO * 60_000;
}

// --- Préstamos ---------------------------------------------------------------------------------

export function iniciales(nombre: string): string {
    return (
        nombre
            .split(/\s+/)
            .filter(Boolean)
            .slice(0, 2)
            .map((p) => p[0]?.toUpperCase() ?? "")
            .join("") || "?"
    );
}

/** Usos con los que la otra empresa puede conversar (no solo avisos). */
export const USOS_CONVERSACION = ["soporte", "cobros", "ventas"] as const;
export function prestamoConversa(usos: readonly string[]): boolean {
    return usos.some((u) => (USOS_CONVERSACION as readonly string[]).includes(u));
}
