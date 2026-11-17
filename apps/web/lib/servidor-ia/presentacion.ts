import { haceCuanto, ZONA } from "@/lib/fechas";
import type { AvisoServidor, EstadoMotor, Gpu, MotivoError, Paquete, PasoPrueba, PuntoSerie, Rango, Tablero, Umbrales } from "./tipos";

/* Textos llanos del Servidor de IA: lo que lee quien administra, sin jerga del servidor. */

export type Tono = "ok" | "aviso" | "error" | "neutro";

const hora = new Intl.DateTimeFormat("es-GT", { hour: "2-digit", minute: "2-digit", hourCycle: "h23", timeZone: ZONA });
const horaSeg = new Intl.DateTimeFormat("es-GT", { hour: "2-digit", minute: "2-digit", second: "2-digit", hourCycle: "h23", timeZone: ZONA });
const diaHora = new Intl.DateTimeFormat("es-GT", { weekday: "short", day: "numeric", hour: "2-digit", minute: "2-digit", hourCycle: "h23", timeZone: ZONA });
const dia = new Intl.DateTimeFormat("es-GT", { day: "numeric", month: "long", timeZone: ZONA });
const entero = new Intl.NumberFormat("es-GT", { maximumFractionDigits: 0 });

/** «6 días», «3 horas», «12 minutos»: cuánto lleva encendido. */
export function duracionLlana(segundos: number): string {
    const s = Math.max(0, Math.floor(segundos));
    const plural = (n: number, uno: string, varios: string) => `${n} ${n === 1 ? uno : varios}`;
    if (s >= 86_400) return plural(Math.floor(s / 86_400), "día", "días");
    if (s >= 3_600) return plural(Math.floor(s / 3_600), "hora", "horas");
    if (s >= 60) return plural(Math.floor(s / 60), "minuto", "minutos");
    return "menos de un minuto";
}

/** «13.1 GB», «274 MB». */
export function tamano(mb: number): string {
    if (mb >= 1024) {
        const gb = mb / 1024;
        return `${gb >= 100 ? Math.round(gb) : gb.toFixed(1)} GB`;
    }
    return `${Math.round(mb)} MB`;
}

/** «17 K», «2,883»: tokens y peticiones sin ruido. */
export function cantidad(n: number): string {
    if (n >= 1_000_000) return `${(n / 1_000_000).toFixed(1)} M`;
    if (n >= 10_000) return `${Math.round(n / 1000)} K`;
    return entero.format(n);
}

/** «1.7 s», «850 ms». */
export function tiempoRespuesta(ms: number): string {
    return ms >= 1000 ? `${(ms / 1000).toFixed(1)} s` : `${Math.round(ms)} ms`;
}

export const fechaDia = (iso: string): string => {
    const ms = Date.parse(iso);
    return Number.isNaN(ms) ? "" : dia.format(ms);
};

// --- Estado ---------------------------------------------------------------------------------------

const TITULO_ESTADO: Record<EstadoMotor, { tono: Tono; titulo: string }> = {
    listo: { tono: "ok", titulo: "En línea" },
    degradado: { tono: "aviso", titulo: "Con problemas" },
    no_disponible: { tono: "error", titulo: "No responde" },
    incompatible: { tono: "error", titulo: "Versión incompatible" },
    desactivado: { tono: "neutro", titulo: "Desactivado" },
    sin_enlazar: { tono: "neutro", titulo: "Falta activarlo" },
};

/** ¿El asistente puede usar el servidor ahora? Solo `listo`. */
export const enServicio = (estado: EstadoMotor | null): boolean => estado === "listo";

/**
 * ¿Lo que trae `servidor` es de ahora? Solo si el servidor responde y la última lectura salió
 * bien. Si no, las cifras son viejas y **no se muestran como actuales** (zona «Sin datos»).
 */
export const cifrasVigentes = (t: Tablero): boolean => t.servidor !== null && t.estado.lecturaOk && (t.estado.estado === "listo" || t.estado.estado === "degradado");

/** La línea de estado, en llano: «En línea · versión 0.1.0 · compatible con esta versión de Iris · encendido hace 6 días». */
export function lineaEstado(e: Tablero["estado"], ahora: number): { tono: Tono; titulo: string; detalle: string } {
    if (e.estado === null) return { tono: "neutro", titulo: "Sin servidor de IA", detalle: "Todavía no hay ninguno enlazado." };
    const base = TITULO_ESTADO[e.estado];
    const partes: string[] = [];
    if (e.version) partes.push(`versión ${e.version}`);
    if (e.estado === "listo" || e.estado === "degradado") {
        if (e.compatible) partes.push("compatible con esta versión de Iris");
        if (e.encendidoSeg !== null) partes.push(`encendido hace ${duracionLlana(e.encendidoSeg)}`);
        if (!e.lecturaOk) partes.push("la última lectura falló");
    } else if (e.tomadoEn) {
        partes.push(`visto por última vez ${ultimaVez(e.tomadoEn, ahora)}`);
    }
    if (e.estado === "degradado" && e.detalle) partes.push(e.detalle);
    return { ...base, detalle: partes.join(" · ") };
}

/** «hoy a las 10:42 (hace 20 min)». */
export function ultimaVez(iso: string, ahora: number): string {
    const ms = Date.parse(iso);
    if (Number.isNaN(ms)) return "hace un rato";
    const mismoDia = dia.format(ms) === dia.format(ahora);
    return `${mismoDia ? "hoy" : `el ${dia.format(ms)}`} a las ${hora.format(ms)} (${haceCuanto(iso, ahora)})`;
}

/** Qué hay que actualizar cuando no son compatibles (se comprueba en los dos sentidos, §4.7). */
export function queActualizar(p: Pick<Paquete, "version" | "motivoIncompatible" | "versionDisponible">): { titulo: string; detalle: string; comando: string | null } {
    const hasta = p.versionDisponible ? ` a la ${p.versionDisponible} o una más nueva` : "";
    // Supuesto de forma: `iris_antiguo` = el servidor es más nuevo que lo que esta versión de Iris conoce.
    if (p.motivoIncompatible === "iris_antiguo") {
        return { titulo: "Hay que actualizar Iris", detalle: `El servidor tiene Iris Voz ${p.version}, más nuevo que lo que esta versión de Iris sabe usar. Actualiza Iris; en el servidor no hay que hacer nada.`, comando: null };
    }
    return {
        titulo: "Hay que actualizar Iris Voz en el servidor",
        detalle: `El servidor tiene Iris Voz ${p.version} y esta versión de Iris necesita una más nueva${hasta ? `: actualízalo${hasta}` : ""}. Se actualiza en el servidor, con este comando:`,
        comando: "sudo iris-voz actualizar",
    };
}

// --- Tarjetas -----------------------------------------------------------------------------------

/** Tono de una parte usada frente a su umbral: ámbar al acercarse (90 % del umbral), rojo al pasarlo. */
export function tonoFrente(valor: number, umbral: number): "normal" | "aviso" | "error" {
    if (valor >= umbral) return "error";
    return valor >= umbral * 0.9 ? "aviso" : "normal";
}

export type TarjetaGpu = {
    nombre: string;
    uso: { valor: string; parte: number } | null;
    memoria: { valor: string; de: string; detalle: string; parte: number; tono: "normal" | "aviso" | "error" } | null;
    temperatura: { valor: string; parte: number; tono: "normal" | "aviso" | "error"; detalle: string } | null;
};

/** Lo que dice cada tarjeta gráfica; un dato que la tarjeta no da queda `null` («Sin datos»), nunca cero. */
export function tarjetaGpu(g: Gpu, u: Pick<Umbrales, "vramPct" | "temperaturaC"> | null): TarjetaGpu {
    const vram = g.vramUsadaMb !== null && g.vramTotalMb !== null && g.vramTotalMb > 0 ? { usada: g.vramUsadaMb, total: g.vramTotalMb } : null;
    const parteVram = vram ? vram.usada / vram.total : 0;
    const tonoTemp = g.temperaturaC !== null && u ? tonoFrente(g.temperaturaC, u.temperaturaC) : "normal";
    return {
        nombre: g.nombre,
        uso: g.usoPct === null ? null : { valor: `${Math.round(g.usoPct)} %`, parte: g.usoPct / 100 },
        memoria: vram
            ? {
                  valor: (vram.usada / 1024).toFixed(1),
                  de: `de ${tamano(vram.total)}`,
                  detalle: `${Math.round(parteVram * 100)} % · quedan ${tamano(Math.max(vram.total - vram.usada, 0))}`,
                  parte: parteVram,
                  tono: u ? tonoFrente(parteVram * 100, u.vramPct) : "normal",
              }
            : null,
        temperatura:
            g.temperaturaC === null
                ? null
                : {
                      valor: `${Math.round(g.temperaturaC)}`,
                      parte: g.temperaturaC / 100,
                      tono: tonoTemp,
                      detalle: `${tonoTemp === "error" ? "Alta" : tonoTemp === "aviso" ? "Subiendo" : "Normal"}${u ? ` · avisa a los ${u.temperaturaC} °C` : ""}`,
                  },
    };
}

// --- Series -------------------------------------------------------------------------------------

export const NOMBRE_RANGO: Record<Rango, string> = { ahora: "Ahora", "1h": "1 hora", "24h": "24 horas", "7d": "7 días" };
export const TEXTO_RANGO: Record<Rango, string> = { ahora: "en los últimos 5 minutos", "1h": "en la última hora", "24h": "en las últimas 24 horas", "7d": "en los últimos 7 días" };

/** Cada cuánto se relee el tablero (ms). En 7 días los datos van por hora: no hace falta correr. */
export const REFRESCO_MS: Record<Rango, number> = { ahora: 10_000, "1h": 30_000, "24h": 60_000, "7d": 300_000 };

export type SeriesTablero = {
    instantes: string[];
    marcas: [string, string, string];
    uso: (number | null)[];
    vram: (number | null)[];
    peticiones: (number | null)[];
    media: (number | null)[];
    p95: (number | null)[];
    /** ¿Hay alguna lectura de tarjeta gráfica en el rango? (sin tarjeta, esa gráfica no se dibuja) */
    hayGpu: boolean;
};

/** Arma las series del rango con sus instantes y las tres marcas del eje. */
export function seriesDe(puntos: readonly PuntoSerie[], rango: Rango): SeriesTablero {
    const formato = rango === "7d" ? diaHora : rango === "ahora" ? horaSeg : hora;
    const instantes = puntos.map((p) => formato.format(Date.parse(p.t)));
    const medio = instantes[Math.floor((instantes.length - 1) / 2)] ?? "";
    const marcas: [string, string, string] = rango === "ahora" ? ["hace 5 min", "hace 2 min", "ahora"] : [instantes[0] ?? "", medio, instantes.at(-1) ?? ""];
    const seg = (ms: number | null) => (ms === null ? null : ms / 1000);
    return {
        instantes,
        marcas,
        uso: puntos.map((p) => p.usoPct),
        vram: puntos.map((p) => p.vramPct),
        peticiones: puntos.map((p) => p.peticionesMin),
        media: puntos.map((p) => seg(p.latenciaMediaMs)),
        p95: puntos.map((p) => seg(p.latenciaP95Ms)),
        hayGpu: puntos.some((p) => p.usoPct !== null || p.vramPct !== null),
    };
}

// --- Errores, avisos y prueba ------------------------------------------------------------------

const ERROR: Record<MotivoError, string> = {
    tiempo_agotado: "La respuesta tardó demasiado y se cortó",
    no_disponible: "El servidor no respondía",
    ocupado: "El servidor estaba lleno y la rechazó",
    respuesta_invalida: "Llegó una respuesta que no se pudo usar",
    modelo_no_instalado: "Se pidió un modelo que no está instalado en el servidor",
    otro: "Falló por otra causa",
};

/** Cada tipo de error en llano, con cuántas veces pasó. */
export function textoError(motivo: MotivoError, cuenta: number): { titulo: string; veces: string } {
    return { titulo: ERROR[motivo] ?? ERROR.otro, veces: cuenta === 1 ? "1 vez" : `${entero.format(cuenta)} veces` };
}

export const NOMBRE_AVISO: Record<AvisoServidor["tipo"], string> = {
    caido: "Servidor caído",
    incompatible: "Versión incompatible",
    vram: "Memoria de video",
    disco: "Disco",
    temperatura: "Temperatura alta",
    saturacion: "Saturación",
};

/** Cada aviso en llano: qué pasó y qué hacer. */
export function textoAviso(a: AvisoServidor): { titulo: string; detalle: string } {
    const valor = a.valor === null ? null : Math.round(a.valor);
    switch (a.tipo) {
        case "caido":
            return { titulo: "Servidor caído", detalle: "El servidor de IA dejó de responder. Lo que el asistente atendía solo pasó a personas; las sugerencias muestran error con «Reintentar»." };
        case "incompatible":
            return { titulo: "Versión incompatible", detalle: "El servidor y esta versión de Iris no son compatibles. Mientras tanto el asistente no usa el servidor." };
        case "vram":
            return { titulo: valor === null ? "Memoria de video alta" : `Memoria de video al ${valor} %`, detalle: `${a.umbral === null ? "Pasó del umbral" : `Pasó del ${a.umbral} %`}. Si se llena, las respuestas fallan. Qué hacer: usar un modelo más pequeño o dejar menos modelos permitidos.` };
        case "disco":
            return { titulo: valor === null ? "Disco casi lleno" : `Disco al ${valor} %`, detalle: "Es el disco donde viven los modelos. Quita en el servidor los modelos que ya no se usan." };
        case "temperatura":
            return { titulo: "Temperatura alta", detalle: `${valor === null ? "La tarjeta gráfica se calentó de más" : `La tarjeta gráfica llegó a ${valor} °C`}. Revisa la ventilación del servidor; con mucho calor trabaja más lento para protegerse.` };
        case "saturacion":
            return {
                titulo: "Saturación",
                detalle: `${valor === null ? "Se rechazaron peticiones" : `Se rechazaron ${valor} ${valor === 1 ? "petición" : "peticiones"}`} porque el servidor estaba lleno. No hay cola: una petición sin lugar falla al instante y el asistente la pasa a una persona. Qué hacer: menos empresas, un modelo más pequeño o más memoria de video.`,
            };
    }
}

export const NOMBRE_PASO: Record<PasoPrueba["codigo"], string> = {
    conexion: "Conexión segura",
    compatibilidad: "Versión compatible",
    gpu: "Tarjeta gráfica",
    stt: "Entender la voz",
    tts: "Hablar",
    llm: "El modelo responde",
    medios: "Audio de las llamadas",
    capacidad: "Capacidad",
};

/** Qué pasa con el asistente mientras el servidor no está (§4.11), dicho tal cual. */
export const MIENTRAS_TANTO = ["Las sugerencias muestran error con «Reintentar».", "Lo que el asistente atendía solo pasa a una persona.", "No se pierde nada: las conversaciones siguen en la bandeja."] as const;

/** «7F3A 91C2 0B5E …»: la huella en grupos de cuatro, para compararla letra por letra. */
export function huellaLegible(huella: string): string[] {
    const limpia = huella.replace(/^sha256:/i, "").replace(/[^0-9a-f]/gi, "").toUpperCase();
    return limpia.match(/.{1,4}/g) ?? [];
}

/** Los errores de enlazar, conectar y activar, en llano. `null`: uno que no se conoce (se usa el del servidor). */
export function textoErrorEnlace(codigo: string, estado: number): string | null {
    if (estado === 401) return "El servidor no aceptó el código. Puede que esté mal escrito allá, que ya se haya usado o que la ventana de 15 minutos se haya cerrado. Vuelve a ejecutar «iris-voz enlazar» en el servidor y escribe el código de nuevo.";
    if (estado === 429) return "Demasiados intentos seguidos. Espera unos segundos y vuelve a probar.";
    switch (codigo) {
        case "motor_ya_enlazado":
            return "Ya hay un servidor de IA enlazado. Para cambiarlo, primero desenlaza el actual.";
        case "url_no_permitida":
            return "Esa dirección no se puede usar. Tiene que ser https, con la dirección y el puerto del servidor (por ejemplo https://10.0.0.5:8443).";
        case "motor_no_responde":
            return "Iris no pudo llegar al servidor en esa dirección. Revisa que esté encendido, que la dirección sea la que mostró el instalador y que la ventana de enlace siga abierta.";
        case "motor_incompatible":
            return "La versión de Iris Voz del servidor no es compatible con esta versión de Iris. Actualiza el servidor (sudo iris-voz actualizar) y vuelve a enlazar.";
        case "enlace_vencido":
            return "El enlace caducó sin activarse. Hay que empezar de nuevo.";
        default:
            return null;
    }
}

/** El 422 al guardar `servidor_ia` en el asistente de una empresa (§4.11). */
export const TEXTO_NO_DISPONIBLE = "El Servidor de IA no está disponible para esta empresa ahora: no está enlazado y en línea, la empresa no está entre las permitidas o ese modelo ya no está instalado. Pídele a quien administra la instalación que lo revise, o elige otro proveedor.";
