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

/** «3.4 M», «170 K», «10,613»: tokens y peticiones sin ruido. */
export function cantidad(n: number): string {
    if (n >= 1_000_000) return `${(n / 1_000_000).toFixed(1)} M`;
    if (n >= 100_000) return `${Math.round(n / 1000)} K`;
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

/** Una lectura de más de un minuto ya no es «ahora», diga lo que diga `lecturaOk`. Se mide contra la hora del servidor. */
export const LECTURA_VIEJA_MS = 60_000;
/**
 * Sin la hora del servidor solo queda el reloj del navegador, que puede estar mal puesto: la
 * tolerancia sube a 5 minutos y el corte fino queda en `lecturaOk`, que lo decide el servidor.
 */
export const LECTURA_VIEJA_SIN_HORA_MS = 5 * 60_000;

/** ¿La muestra (`tomadoEn`) es reciente respecto de `ahora`? Sin fecha, no. */
export function lecturaReciente(tomadoEn: string | null, ahora: number, tolerancia = LECTURA_VIEJA_MS): boolean {
    const ms = tomadoEn === null ? NaN : Date.parse(tomadoEn);
    return !Number.isNaN(ms) && ahora - ms <= tolerancia;
}

/**
 * ¿Lo que trae `servidor` es de ahora? Solo si el servidor responde, la última lectura salió bien
 * **y es reciente**. Si no, las cifras son viejas y **no se muestran como actuales** («Sin datos»).
 */
export const cifrasVigentes = (t: Tablero, ahora: number, tolerancia = LECTURA_VIEJA_MS): boolean =>
    t.servidor !== null && t.estado.lecturaOk && (t.estado.estado === "listo" || t.estado.estado === "degradado") && lecturaReciente(t.estado.tomadoEn, ahora, tolerancia);

/**
 * La línea de estado, en llano: «En línea · versión 0.1.0 · compatible con esta versión de Iris ·
 * encendido hace 6 días». El `detalle` técnico del servidor solo lo lee quien administra la
 * instalación (`global`): al admin de una empresa no se le enseña aunque llegue.
 */
export function lineaEstado(e: Tablero["estado"], ahora: number, global = false): { tono: Tono; titulo: string; detalle: string } {
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
    if (global && e.estado === "degradado" && e.detalle) partes.push(e.detalle);
    return { ...base, detalle: partes.join(" · ") };
}

/** «hoy a las 10:42 (hace 20 min)». */
export function ultimaVez(iso: string, ahora: number): string {
    const ms = Date.parse(iso);
    if (Number.isNaN(ms)) return "hace un rato";
    const mismoDia = dia.format(ms) === dia.format(ahora);
    return `${mismoDia ? "hoy" : `el ${dia.format(ms)}`} a las ${hora.format(ms)} (${haceCuanto(iso, ahora)})`;
}

/**
 * Los comandos de `iris-voz` que el panel deja copiar. **Siempre en inglés** (regla del PM): son
 * los del instalador (`infra/voz/install-voz.sh`), sin alias en español. Los rótulos y las
 * explicaciones de alrededor sí van en español.
 */
export const COMANDO = {
    enlazar: "sudo iris-voz pair",
    desenlazar: "sudo iris-voz unpair",
    estado: "sudo iris-voz status",
    actualizar: "sudo iris-voz upgrade",
    modeloDescargar: "sudo iris-voz model pull <nombre>",
    modeloPermitir: "sudo iris-voz model allow <nombre>",
    modeloQuitar: "sudo iris-voz model remove <nombre>",
} as const;

/** El comando que actualiza Iris Voz, en el servidor. */
export const COMANDO_ACTUALIZAR = COMANDO.actualizar;

/** Qué hay que actualizar cuando no son compatibles (se comprueba en los dos sentidos, §4.7): solo `iris_antiguo` pide actualizar Iris. */
export function queActualizar(p: Pick<Paquete, "version" | "motivoIncompatible" | "versionDisponible">): { titulo: string; detalle: string; comando: string | null } {
    const servidor = (detalle: string) => ({ titulo: "Hay que actualizar Iris Voz en el servidor", detalle: `${detalle} Se actualiza en el servidor, con este comando:`, comando: COMANDO_ACTUALIZAR });
    // Hoy `versionDisponible` es siempre `null`: sin ella no se dice a cuál.
    const hasta = p.versionDisponible ? ` (a la ${p.versionDisponible} o una más nueva)` : "";
    switch (p.motivoIncompatible) {
        case "iris_antiguo":
            return { titulo: "Hay que actualizar Iris", detalle: `El servidor tiene Iris Voz ${p.version}, más nuevo que lo que esta versión de Iris sabe usar. Actualiza Iris; en el servidor no hay que hacer nada.`, comando: null };
        case "iris_nuevo":
            return servidor(`Esta versión de Iris es más nueva que lo que Iris Voz ${p.version} sabe atender${hasta}.`);
        case "paquete_antiguo":
            return servidor(`El servidor tiene Iris Voz ${p.version}, más antiguo que el mínimo que usa esta versión de Iris${hasta}.`);
        case "protocolo":
            return servidor(`Iris y el servidor (Iris Voz ${p.version}) no hablan la misma versión del protocolo${hasta}.`);
        default:
            return servidor(`El servidor tiene Iris Voz ${p.version}, que no es compatible con esta versión de Iris${hasta}.`);
    }
}

/** La línea de la tarjeta de Integraciones: el estado y cuándo se le vio por última vez (no afirma que la lectura sea de ahora). */
export function lineaTarjeta(m: { estado: EstadoMotor; version: string | null; ultimoLatidoEn: string | null }, ahora: number): { tono: Tono; titulo: string; detalle: string } {
    const partes: string[] = [];
    if (m.version) partes.push(`versión ${m.version}`);
    partes.push(m.ultimoLatidoEn ? `visto por última vez ${ultimaVez(m.ultimoLatidoEn, ahora)}` : "todavía no ha respondido");
    return { ...TITULO_ESTADO[m.estado], detalle: partes.join(" · ") };
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
export const TEXTO_RANGO: Record<Rango, string> = { ahora: "en los últimos 10 minutos", "1h": "en la última hora", "24h": "en las últimas 24 horas", "7d": "en los últimos 7 días" };

/** Cada cuánto se relee el tablero (ms). Nunca más de un minuto: las cifras de «ahora» van en todos los periodos. */
export const REFRESCO_MS: Record<Rango, number> = { ahora: 10_000, "1h": 30_000, "24h": 60_000, "7d": 60_000 };

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
    const marcas: [string, string, string] = rango === "ahora" ? ["hace 10 min", "hace 5 min", "ahora"] : [instantes[0] ?? "", medio, instantes.at(-1) ?? ""];
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
        default:
            // Un tipo que este panel aún no conoce: se dice que lo hay, sin inventar qué es.
            return { titulo: "Aviso del servidor de IA", detalle: "Mira el tablero del servidor de IA para ver qué pasa." };
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

/**
 * La huella **tal como la imprime el servidor** (`sha256:` + 64 letras en minúscula, seguidas): es
 * lo que se compara con lo que mostró el instalador o `iris-voz status`.
 */
export function huellaComoElServidor(huella: string): string {
    const limpia = huella.replace(/^sha256:/i, "").replace(/[^0-9a-f]/gi, "").toLowerCase();
    return `sha256:${limpia}`;
}

/** «3fa9 1c07 b2de …»: la misma huella en grupos de cuatro, solo para leerla más fácil (los espacios no cuentan). */
export function huellaLegible(huella: string): string[] {
    const limpia = huella.replace(/^sha256:/i, "").replace(/[^0-9a-f]/gi, "").toLowerCase();
    return limpia.match(/.{1,4}/g) ?? [];
}

/** Lo que se avisa en el paso de la huella si «Conectar» ya dijo que no son compatibles (no se deja activar). */
export function avisoIncompatibleAlEnlazar(p: Pick<Paquete, "compatible" | "motivoIncompatible" | "version" | "versionDisponible">): { titulo: string; detalle: string; comando: string | null } | null {
    if (p.compatible || p.motivoIncompatible === null) return null;
    const q = queActualizar({ version: p.version ?? "", motivoIncompatible: p.motivoIncompatible, versionDisponible: p.versionDisponible });
    return p.motivoIncompatible === "iris_antiguo"
        ? { titulo: "Este servidor pide una versión de Iris más nueva; actualiza Iris", detalle: "Hasta actualizar Iris no se puede activar. En el servidor no hay que hacer nada.", comando: null }
        : { titulo: "Esta versión de Iris pide una versión más nueva de Iris Voz; actualiza el servidor", detalle: "Hasta actualizarlo no se puede activar. Se actualiza en el servidor, con este comando:", comando: q.comando };
}

/** Los errores de enlazar, conectar y activar, en llano. `null`: uno que no se conoce (se usa el del servidor). */
export function textoErrorEnlace(codigo: string, estado: number, alConectar = false): string | null {
    // Al conectar, el servidor responde lo mismo para cualquier falla (no delata si el código era bueno).
    if (alConectar && codigo === "motor_no_responde") {
        return `Iris no pudo conectarse con el servidor. Puede que el código esté mal escrito allá o ya se haya usado, que la ventana de 15 minutos se haya cerrado o que el servidor no esté al alcance. Vuelve a ejecutar «${COMANDO.enlazar}» en el servidor y escribe el código de nuevo.`;
    }
    if (estado === 401) return `El servidor no aceptó el código. Puede que esté mal escrito allá o que ya se haya usado. Vuelve a ejecutar «${COMANDO.enlazar}» en el servidor y escribe el código de nuevo.`;
    if (estado === 429) return "Demasiados intentos seguidos. Espera unos segundos y vuelve a probar.";
    switch (codigo) {
        case "motor_ya_enlazado":
            return "Ya hay un servidor de IA enlazado. Para cambiarlo, primero desenlaza el actual.";
        case "url_no_permitida":
            return "Esa dirección no se puede usar. Tiene que ser https, con la dirección y el puerto del servidor (por ejemplo https://10.0.0.5:8443).";
        case "motor_no_responde":
            return "Iris no pudo llegar al servidor en esa dirección. Revisa que esté encendido, que la dirección sea la que mostró el instalador y que la ventana de enlace siga abierta.";
        case "motor_incompatible":
            return `La versión de Iris Voz del servidor no es compatible con esta versión de Iris. Actualiza el servidor (${COMANDO.actualizar}) y vuelve a enlazar.`;
        case "incompatible":
            return "El servidor y esta versión de Iris no son compatibles: no se puede activar hasta actualizar el que se indica arriba.";
        case "servidor_desactivado":
            return TEXTO_DESACTIVADO;
        case "huella_cambiada":
            return TEXTO_HUELLA_CAMBIADA;
        case "rotacion_en_curso":
            return TEXTO_ROTACION_EN_CURSO;
        case "enlace_vencido":
            return "Ese paso del enlace ya venció: el código vale 15 minutos y, una vez conectado, hay 24 horas para activar. Hay que empezar de nuevo.";
        default:
            return null;
    }
}

/** El 422 al guardar `servidor_ia` en el asistente de una empresa (§4.11). */
export const TEXTO_NO_DISPONIBLE = "El Servidor de IA no está disponible para esta empresa ahora: no está enlazado y en línea, la empresa no está entre las permitidas o ese modelo ya no está instalado. Pídele a quien administra la instalación que lo revise, o elige otro proveedor.";

/** 409 `huella_cambiada` al activar: lo que se comparó ya no es lo que hay. */
export const TEXTO_HUELLA_CAMBIADA = "El servidor cambió desde que lo comparaste; vuelve a conectar y compara de nuevo.";
/** 409 `rotacion_en_curso` (activar o rotar): no es un fallo, hay que esperar. */
export const TEXTO_ROTACION_EN_CURSO = "Ya hay un cambio de llave en curso; espera unos segundos.";
/** Rotar sin respuesta o con 5xx (§4.2: se aplica entera o nada, e Iris se recupera sola). */
export const TEXTO_ROTACION_SIN_CONFIRMAR = "No se pudo confirmar el cambio. Iris recupera la conexión sola; no hace falta enlazar de nuevo.";

/** 409 `servidor_desactivado` (probar con el servidor desactivado). */
export const TEXTO_DESACTIVADO = "El servidor está desactivado: Iris no lo usa. Para volver a usarlo hay que enlazarlo de nuevo.";

/** Lo mismo, dicho a quien configura el asistente de una empresa (no puede enlazarlo). */
export const TEXTO_DESACTIVADO_EMPRESA = "El Servidor de IA está desactivado. Pídele a quien administra la instalación que lo enlace de nuevo, o elige otro proveedor.";

/** «2 de noviembre, 09:00». */
export const fechaHora = (iso: string): string => {
    const ms = Date.parse(iso);
    return Number.isNaN(ms) ? "" : `${dia.format(ms)}, ${hora.format(ms)}`;
};

/** «310 W». */
export const potencia = (w: number): string => `${Math.round(w)} W`;

/** El aviso de que la plaza asegurada no alcanza para todas las empresas (§4.11). */
export function textoPlazas(empresasActivas: number | undefined, plazas: number | null): string {
    const cuantas = empresasActivas === undefined || plazas === null ? "" : ` (${empresasActivas} ${empresasActivas === 1 ? "empresa" : "empresas"} y ${plazas} ${plazas === 1 ? "turno" : "turnos"})`;
    return `Hay más empresas usando el servidor que turnos disponibles${cuantas}: ya no se le puede asegurar un turno a cada una y a ratos alguna se quedará sin respuesta. Deja menos empresas permitidas o usa un modelo más pequeño.`;
}
