import type { MotivoAviso } from "./maquina";
import type { Llamada, LlamadaEnMensaje, RequisitoLlamadas, ResultadoLlamada } from "./tipos";

/* Textos llanos de la voz E1: lo que la persona lee en el aviso, la barra, el hilo y los ajustes. */

/** «01:12», «1:02:07». */
export function reloj(segundos: number): string {
    const s = Math.max(0, Math.floor(segundos));
    const h = Math.floor(s / 3600);
    const m = Math.floor((s % 3600) / 60);
    const dos = (n: number) => String(n).padStart(2, "0");
    return h > 0 ? `${h}:${dos(m)}:${dos(s % 60)}` : `${dos(m)}:${dos(s % 60)}`;
}

/** «48 s», «3 min», «1 min 5 s». */
export function duracionCorta(segundos: number): string {
    const s = Math.max(0, Math.round(segundos));
    if (s < 60) return `${s} s`;
    const m = Math.floor(s / 60);
    return s % 60 && m < 10 ? `${m} min ${s % 60} s` : `${m} min`;
}

/** Quién llama: el nombre del cliente o «Número desconocido» (nunca se inventa un nombre). */
export function quienLlama(l: Pick<Llamada, "titulo" | "numero" | "contacto">): { nombre: string; conocido: boolean } {
    const conocido = l.contacto.tipo === "cliente" && l.titulo.trim() !== "" && l.titulo !== l.numero;
    return { nombre: conocido ? l.titulo : "Número desconocido", conocido };
}

/** El mensaje llano de cada cierre sin hablar. */
export function textoAviso(motivo: MotivoAviso, quien?: string): { titulo: string; detalle: string } {
    switch (motivo) {
        case "otro_la_contesto":
            // Sin saber quién (409 al reclamar): pudo tomarla otra persona o pasar el tiempo de timbre.
            return quien ? { titulo: `La contestó ${quien}`, detalle: "Ya la están atendiendo." } : { titulo: "Ya no se puede contestar", detalle: "Otra persona la contestó o dejó de sonar." };
        case "reserva_vencida":
            return { titulo: "No se alcanzó a conectar", detalle: "Pasó el tiempo para contestar. Si sigue sonando, vuelve a intentarlo." };
        case "termino_antes":
            return { titulo: "La llamada terminó antes de conectar", detalle: "Quedó en la conversación." };
        case "sin_microfono":
            return { titulo: "No se encontró un micrófono", detalle: "Conecta uno y vuelve a encender «Recibir llamadas»." };
        case "microfono_denegado":
            return { titulo: "El navegador no dio permiso para el micrófono", detalle: "Permítelo en el candado de la barra de direcciones y vuelve a intentarlo." };
        case "no_recibe_llamadas":
            return { titulo: "Este navegador no está recibiendo llamadas", detalle: "Enciende «Recibir llamadas» para contestar desde aquí." };
        case "sin_red":
            return { titulo: "Se perdió la conexión", detalle: "No se pudo contestar. La llamada sigue sonándoles a los demás." };
        case "medio_fallido":
            return { titulo: "No se pudo conectar el audio", detalle: "Tu red no dejó pasar la llamada. Prueba otra vez o desde otra red." };
        case "meta_rechazo":
            return { titulo: "WhatsApp no aceptó la llamada", detalle: "Puede que el cliente ya haya colgado." };
        case "otro_navegador":
            return { titulo: "La estás contestando en otro navegador", detalle: "Sigue allá: aquí no se cambia nada." };
        case "otra_llamada":
            return { titulo: "Ya tienes otra llamada", detalle: "Termínala para contestar esta. Mientras tanto les sigue sonando a los demás." };
        default:
            return { titulo: "No se pudo contestar", detalle: "Vuelve a intentarlo." };
    }
}

/** El motivo de aviso que corresponde a un código de error del servidor (§3.11) y a su regla. */
export function motivoDeCodigo(codigo: string, estado: number, regla?: string): MotivoAviso {
    if (codigo === "llamada_ya_contestada") return regla === "otro_navegador" ? "otro_navegador" : "otro_la_contesto";
    if (codigo === "reserva_vencida") return "reserva_vencida";
    if (codigo === "llamada_terminada") return "termino_antes";
    if (codigo === "no_recibe_llamadas") return "no_recibe_llamadas";
    if (codigo === "meta_rechazo") return "meta_rechazo";
    if (codigo === "llamada_en_curso") return "otra_llamada";
    if (codigo === "sdp_invalido") return "medio_fallido";
    return estado === 0 ? "sin_red" : "error";
}

export type TonoLlamada = "ok" | "error" | "aviso" | "neutro" | "info";
export type IconoLlamada = "contestada" | "perdida" | "rechazada" | "fuera_de_horario" | "en_curso" | "fallida";

/**
 * Cómo se ve una llamada en el hilo (`tipo: "llamada"`): icono + texto + color. La perdida es la
 * única que pide actuar (rojo); las demás informan.
 */
export function llamadaEnHilo(l: LlamadaEnMensaje): { icono: IconoLlamada; tono: TonoLlamada; titulo: string; detalle: string | null } {
    const quien = l.atendidaPor === "asistente" ? "el asistente" : (l.usuario?.nombre ?? null);
    const duracion = l.duracionSeg !== undefined ? duracionCorta(l.duracionSeg) : null;
    switch (l.estado) {
        case "terminada":
            return { icono: "contestada", tono: "ok", titulo: "Llamada de WhatsApp", detalle: [quien ? `La atendió ${quien}` : null, duracion].filter(Boolean).join(" · ") || null };
        case "perdida":
            return { icono: "perdida", tono: "error", titulo: "Llamada perdida de WhatsApp", detalle: duracion ? `Sonó ${duracion} y nadie contestó` : "Nadie contestó" };
        case "rechazada":
            return { icono: "rechazada", tono: "neutro", titulo: "Llamada rechazada", detalle: quien ? `La rechazó ${quien}` : null };
        case "fuera_de_horario":
            return { icono: "fuera_de_horario", tono: "aviso", titulo: "Llamó fuera de horario", detalle: "Se le avisó el horario de atención." };
        case "fallida":
            return { icono: "fallida", tono: "aviso", titulo: "La llamada no se pudo conectar", detalle: null };
        case "en_curso":
        case "conectando":
            return { icono: "en_curso", tono: "info", titulo: "En llamada", detalle: quien ? `Con ${quien}` : null };
        default:
            return { icono: "en_curso", tono: "info", titulo: "Llamada de WhatsApp sonando", detalle: null };
    }
}

export const RESULTADOS: readonly { valor: ResultadoLlamada; texto: string }[] = [
    { valor: "resuelto", texto: "Quedó resuelto" },
    { valor: "ticket_creado", texto: "Se creó un ticket" },
    { valor: "devolver", texto: "Hay que devolverle la llamada" },
    { valor: "equivocado", texto: "Número equivocado" },
    { valor: "otro", texto: "Otro" },
];

const NOMBRE_REQUISITO: Record<RequisitoLlamadas["codigo"], string> = {
    conexion_api: "El número está conectado por la API de WhatsApp",
    limite_mensajeria: "Puede escribirle a 2,000 personas al día o más",
    webhook_calls: "Meta le avisa a Iris de las llamadas",
    calling_habilitado: "Las llamadas están encendidas en Meta",
    plantilla_permiso: "Hay una plantilla para pedir permiso de llamar",
};

export type EstadoRequisito = "cumple" | "falta" | "aviso" | "sin_comprobar";

/**
 * Un requisito de Meta, en llano. `aviso`: no impide encender (solo limita algo). `sinComprobar`:
 * Iris no lo puede leer de Meta (se sabrá al llegar la primera llamada); nunca se muestra como cumplido.
 */
export function textoRequisito(r: RequisitoLlamadas): { nombre: string; detalle: string; estado: EstadoRequisito } {
    const estado: EstadoRequisito = r.sinComprobar ? "sin_comprobar" : r.cumple ? "cumple" : r.aviso ? "aviso" : "falta";
    return { nombre: NOMBRE_REQUISITO[r.codigo] ?? r.codigo.replace(/_/g, " "), detalle: r.detalle, estado };
}
