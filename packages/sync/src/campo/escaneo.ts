// Escáner de serie y MAC por campo: qué parece cada código leído, cuáles encajan con lo que se está
// escaneando, cuáles caen dentro del recuadro y la ventana corta en la que se juntan. Lo comparten la
// PWA (`/campo`, BarcodeDetector o ZXing) y la app nativa (expo-camera). Sin DOM ni React.
//
// Las etiquetas de ONU, routers y antenas traen varios códigos (S/N, MAC, GPON SN, modelo, EAN y a
// veces un QR): el escáner nunca acepta en silencio el primero que ve. Junta los distintos durante
// `VENTANA_ESCANEO_MS` y el técnico elige, con los que encajan primero.
import { normalizarMac } from "./codigos";

/** Campo que se está escaneando. */
export type ObjetivoEscaneo = "serie" | "mac";

/** Qué parece un código leído. */
export type ClaseCodigo = "mac" | "serie" | "qr_combinado" | "producto" | "desconocido";

export interface Punto {
    x: number;
    y: number;
}

/** Rectángulo en las coordenadas de quien lo da (vista, video o cuadro de la cámara). */
export interface Caja {
    x: number;
    y: number;
    ancho: number;
    alto: number;
}

export interface Medidas {
    ancho: number;
    alto: number;
}

/** Un código que vio el lector, con su posición si la da (`bounds`, `cornerPoints`, `boundingBox`). */
export interface CodigoVisto {
    valor: string;
    /** Formato tal como lo da el lector (`qr`, `code128`, `qr_code`, `EAN_13`…). */
    formato: string;
    caja?: Caja | null;
    esquinas?: readonly Punto[] | null;
}

/** Lo que se llenaría en el formulario al elegir un código. */
export interface UsoCodigo {
    serie?: string;
    mac?: string;
}

export interface Candidato {
    /** Lo leído, sin espacios a los lados. */
    valor: string;
    formato: string;
    tipo: "qr" | "barras";
    /** «QR» o «Código de barras». */
    tipoTexto: string;
    /** Formato legible en texto chico («Code 128», «EAN-13»). */
    formatoTexto: string;
    clase: ClaseCodigo;
    /** Encaja con el objetivo (va primero). */
    encaja: boolean;
    /** Se puede elegir para este objetivo (para la MAC, solo una MAC válida o un QR que la traiga). */
    elegible: boolean;
    /** «Parece MAC», «Parece serie», «QR con serie y MAC», «No es una MAC»… */
    insignia: string;
    tono: "success" | "info" | "warning" | "neutral";
    uso: UsoCodigo;
    /** Campos de `uso` que ya están en otro equipo de esta orden. */
    repetidos: ("serie" | "mac")[];
}

/** Ventana en la que se juntan los códigos distintos antes de mostrar la lista. */
export const VENTANA_ESCANEO_MS = 800;

export const NOMBRE_OBJETIVO: Record<ObjetivoEscaneo, string> = { serie: "Número de serie", mac: "MAC" };

/** Encabezado y ayuda del visor según el objetivo. */
export function textosEscaneo(objetivo: ObjetivoEscaneo): { titulo: string; ayuda: string; botonCampo: string } {
    return objetivo === "serie"
        ? { titulo: NOMBRE_OBJETIVO.serie, ayuda: "Apunta solo al código junto a S/N", botonCampo: "Escanear número de serie" }
        : { titulo: NOMBRE_OBJETIVO.mac, ayuda: "Apunta solo al código junto a MAC", botonCampo: "Escanear MAC" };
}

const FORMATOS_PRODUCTO = new Set(["ean13", "ean8", "upca", "upce", "itf14"]);
const FORMATOS_2D_TEXTO: Record<string, string> = { qr: "QR", datamatrix: "Data Matrix", aztec: "Aztec", pdf417: "PDF417" };
const FORMATOS_TEXTO: Record<string, string> = {
    code128: "Code 128",
    code39: "Code 39",
    code93: "Code 93",
    codabar: "Codabar",
    ean13: "EAN-13",
    ean8: "EAN-8",
    upca: "UPC-A",
    upce: "UPC-E",
    itf: "ITF",
    itf14: "ITF-14",
    ...FORMATOS_2D_TEXTO,
};

/** `qr_code`, `QR_CODE`, `qr` → `qr`; `ean_13`, `EAN_13`, `ean13` → `ean13`; `upc_a` → `upca`. */
export function normalizarFormato(formato: string): string {
    const f = formato.trim().toLowerCase().replace(/[\s_-]/g, "");
    if (f === "qrcode" || f === "qr") {
        return "qr";
    }
    return f;
}

/** Dígito verificador GTIN (EAN-8, UPC-A, EAN-13, ITF-14). */
export function gtinValido(digitos: string): boolean {
    if (!/^\d+$/.test(digitos) || ![8, 12, 13, 14].includes(digitos.length)) {
        return false;
    }
    const cuerpo = digitos.slice(0, -1);
    let suma = 0;
    for (let i = 0; i < cuerpo.length; i += 1) {
        // Desde la derecha del cuerpo: pesos 3, 1, 3, 1…
        const d = Number(cuerpo[cuerpo.length - 1 - i]);
        suma += i % 2 === 0 ? d * 3 : d;
    }
    return (10 - (suma % 10)) % 10 === Number(digitos[digitos.length - 1]);
}

/** Pares `SN:…;MAC:…`, `S/N=…`, `GPON SN:…` o JSON (`{"sn":…,"mac":…}`) dentro de un código. */
function leerPares(texto: string): UsoCodigo | null {
    try {
        const json: unknown = JSON.parse(texto);
        if (json && typeof json === "object" && !Array.isArray(json)) {
            const o = json as Record<string, unknown>;
            const serie = [o.sn, o.serie, o.serial, o.SN, o.Serial, o.SERIAL].find((v): v is string => typeof v === "string" && v.trim() !== "");
            const mac = [o.mac, o.MAC, o.Mac].find((v): v is string => typeof v === "string" && v.trim() !== "");
            const uso: UsoCodigo = { ...(serie ? { serie: serie.trim() } : {}), ...(mac ? { mac: mac.trim() } : {}) };
            return uso.serie || uso.mac ? uso : null;
        }
    } catch {
        // No es JSON.
    }
    const pares = [...texto.matchAll(/(?:GPON\s*)?(S\/?N|SERIE|SERIAL|MAC)\s*[:=]\s*([^;,\s|]+)/gi)];
    if (pares.length === 0) {
        return null;
    }
    const uso: UsoCodigo = {};
    for (const [, clave = "", valor = ""] of pares) {
        if (clave.toUpperCase() === "MAC") {
            uso.mac ??= valor;
        } else {
            uso.serie ??= valor;
        }
    }
    return uso;
}

/** Una serie típica: 10 a 40 letras y números (guiones o barras), con al menos un dígito. */
function pareceSerie(texto: string): boolean {
    return /^[A-Z0-9][A-Z0-9/.-]{9,39}$/i.test(texto) && /\d/.test(texto);
}

/** Qué parece el código y qué llenaría (serie, MAC o ambas). */
export function analizarCodigo(valor: string, formato: string): { clase: ClaseCodigo } & UsoCodigo {
    const limpio = valor.trim();
    if (limpio === "") {
        return { clase: "desconocido" };
    }
    if (FORMATOS_PRODUCTO.has(normalizarFormato(formato))) {
        return { clase: "producto" };
    }
    const pares = leerPares(limpio);
    if (pares) {
        const mac = pares.mac ? normalizarMac(pares.mac) : null;
        if (pares.serie && mac) {
            return { clase: "qr_combinado", serie: pares.serie.slice(0, 100), mac };
        }
        if (mac) {
            return { clase: "mac", mac };
        }
        if (pares.serie) {
            return { clase: "serie", serie: pares.serie.slice(0, 100) };
        }
    }
    // 12 hexadecimales en un código que no es EAN/UPC: MAC (aunque sean solo dígitos).
    const mac = normalizarMac(limpio);
    if (mac) {
        return { clase: "mac", mac };
    }
    // Un EAN-8/13 o ITF-14 dentro de otro formato (o un número que lo parece) es del producto.
    if (gtinValido(limpio)) {
        return { clase: "producto" };
    }
    if (pareceSerie(limpio)) {
        return { clase: "serie", serie: limpio.slice(0, 100) };
    }
    return { clase: "desconocido" };
}

/** Clase de un código: `mac`, `serie`, `qr_combinado`, `producto` (EAN/UPC) o `desconocido`. */
export function clasificarCodigo(valor: string, formato: string): ClaseCodigo {
    return analizarCodigo(valor, formato).clase;
}

const INSIGNIA: Record<ClaseCodigo, string> = {
    mac: "Parece MAC",
    serie: "Parece serie",
    qr_combinado: "QR con serie y MAC",
    producto: "Parece código de producto (EAN/UPC)",
    desconocido: "No parece serie",
};

/** Quita repetidos por valor (el primero que se vio se queda; si luego llega con posición, se completa). */
export function deduplicarCodigos(codigos: readonly CodigoVisto[]): CodigoVisto[] {
    const vistos = new Map<string, CodigoVisto>();
    for (const c of codigos) {
        const valor = c.valor.trim();
        if (valor === "" || vistos.has(valor)) {
            continue;
        }
        vistos.set(valor, { ...c, valor });
    }
    return [...vistos.values()];
}

const mismaSerie = (a: string, b: string) => a.trim().toUpperCase() === b.trim().toUpperCase();

/**
 * Candidatos para el objetivo: sin repetidos, primero los que encajan y al final los EAN/UPC (nunca
 * van primero para la serie). Para la MAC, todo lo que no sea una MAC válida queda «No es una MAC» y
 * no se puede elegir. `existentes` son los equipos ya registrados en esta orden (aviso de repetido).
 */
export function candidatosPara(
    objetivo: ObjetivoEscaneo,
    codigos: readonly CodigoVisto[],
    existentes: readonly { serie?: string | null; mac?: string | null }[] = [],
): Candidato[] {
    const candidatos = deduplicarCodigos(codigos).map((c): Candidato => {
        const formato = normalizarFormato(c.formato);
        const a = analizarCodigo(c.valor, c.formato);
        const encaja = a.clase === "qr_combinado" || a.clase === objetivo;
        const uso: UsoCodigo =
            a.clase === "qr_combinado"
                ? { ...(a.serie ? { serie: a.serie } : {}), ...(a.mac ? { mac: a.mac } : {}) }
                : objetivo === "mac"
                  ? a.mac
                      ? { mac: a.mac }
                      : {}
                  : { serie: (a.serie ?? c.valor).slice(0, 100) };
        const repetidos: ("serie" | "mac")[] = [];
        if (uso.serie && existentes.some((e) => e.serie && mismaSerie(e.serie, uso.serie ?? ""))) {
            repetidos.push("serie");
        }
        if (uso.mac && existentes.some((e) => e.mac && normalizarMac(e.mac) === uso.mac)) {
            repetidos.push("mac");
        }
        const insignia = objetivo === "mac" && !encaja ? "No es una MAC" : INSIGNIA[a.clase];
        return {
            valor: c.valor,
            formato,
            tipo: formato === "qr" ? "qr" : "barras",
            tipoTexto: formato === "qr" ? "QR" : "Código de barras",
            formatoTexto: FORMATOS_TEXTO[formato] ?? c.formato,
            clase: a.clase,
            encaja,
            elegible: objetivo === "serie" || encaja,
            insignia,
            tono: encaja ? "success" : a.clase === "producto" || objetivo === "mac" ? "warning" : "neutral",
            uso,
            repetidos,
        };
    });
    const rango = (c: Candidato) => (c.encaja ? 0 : c.clase === "producto" ? 2 : 1);
    // `sort` es estable: dentro de cada grupo queda el orden en que se vieron.
    return candidatos.sort((a, b) => rango(a) - rango(b));
}

/**
 * El único candidato que encaja (se muestra como confirmación, con los demás debajo), o `null` si
 * encajan 0 o varios.
 */
export function candidatoUnico(candidatos: readonly Candidato[]): Candidato | null {
    const encajan = candidatos.filter((c) => c.encaja);
    return encajan.length === 1 ? (encajan[0] ?? null) : null;
}

/** Texto de la confirmación de un solo candidato: «¿Usar ABC123 como número de serie?». */
export function preguntaConfirmacion(objetivo: ObjetivoEscaneo, c: Candidato): string {
    if (c.uso.serie && c.uso.mac) {
        return `¿Usar la serie ${c.uso.serie} y la MAC ${c.uso.mac}?`;
    }
    const valor = c.uso.serie ?? c.uso.mac ?? c.valor;
    return `¿Usar ${valor} como ${objetivo === "serie" ? "número de serie" : "MAC"}?`;
}

/** Aviso al elegir: «Serie: ABC123», «MAC: AA:BB:…» o los dos. */
export function textoElegido(uso: UsoCodigo): string {
    return [uso.serie ? `Serie: ${uso.serie}` : null, uso.mac ? `MAC: ${uso.mac}` : null].filter(Boolean).join(" · ");
}

/** Aviso de repetido antes de usar: «Esta serie ya está en otro equipo de esta orden». */
export function textoRepetido(repetidos: readonly ("serie" | "mac")[]): string | null {
    if (repetidos.length === 0) {
        return null;
    }
    const que = repetidos.length === 2 ? "Esta serie y esta MAC ya están" : repetidos[0] === "serie" ? "Esta serie ya está" : "Esta MAC ya está";
    return `${que} en otro equipo de esta orden`;
}

// --- Geometría: solo cuentan los códigos dentro del recuadro ---

/** Centro del código: de su caja o del promedio de sus esquinas; `null` si el lector no lo da. */
export function centroDeCodigo(c: CodigoVisto): Punto | null {
    if (c.caja && c.caja.ancho > 0 && c.caja.alto > 0) {
        return { x: c.caja.x + c.caja.ancho / 2, y: c.caja.y + c.caja.alto / 2 };
    }
    if (c.esquinas && c.esquinas.length > 0) {
        const n = c.esquinas.length;
        return { x: c.esquinas.reduce((s, p) => s + p.x, 0) / n, y: c.esquinas.reduce((s, p) => s + p.y, 0) / n };
    }
    return null;
}

/**
 * Los códigos cuyo centro cae dentro del recuadro (en las mismas coordenadas). Los que no traen
 * posición (ZXing, `bounds` vacío) cuentan todos.
 */
export function dentroDelRecuadro<T extends CodigoVisto>(codigos: readonly T[], recuadro: Caja): T[] {
    return codigos.filter((c) => {
        const centro = centroDeCodigo(c);
        if (!centro) {
            return true;
        }
        return centro.x >= recuadro.x && centro.x <= recuadro.x + recuadro.ancho && centro.y >= recuadro.y && centro.y <= recuadro.y + recuadro.alto;
    });
}

/** Recuadro centrado del visor: 80 % del ancho (hasta 340 px) y alto de 0,6 del ancho. */
export function recuadroCentrado(vista: Medidas): Caja {
    const ancho = Math.min(vista.ancho * 0.8, 340);
    const alto = Math.min(ancho * 0.6, vista.alto * 0.5);
    return { x: (vista.ancho - ancho) / 2, y: (vista.alto - alto) / 2, ancho, alto };
}

/**
 * Pasa un recuadro de la vista (el `<video>` con `object-fit: cover`) a las coordenadas del cuadro
 * de la cámara (`videoWidth` × `videoHeight`), que son las de `boundingBox` de `BarcodeDetector`.
 */
export function recuadroEnFuente(recuadro: Caja, fuente: Medidas, vista: Medidas): Caja {
    if (fuente.ancho <= 0 || fuente.alto <= 0 || vista.ancho <= 0 || vista.alto <= 0) {
        return recuadro;
    }
    const escala = Math.max(vista.ancho / fuente.ancho, vista.alto / fuente.alto);
    const desplX = (fuente.ancho * escala - vista.ancho) / 2;
    const desplY = (fuente.alto * escala - vista.alto) / 2;
    return {
        x: (recuadro.x + desplX) / escala,
        y: (recuadro.y + desplY) / escala,
        ancho: recuadro.ancho / escala,
        alto: recuadro.alto / escala,
    };
}

// --- Ventana corta: se juntan los códigos distintos antes de mostrar la lista ---

export interface Coleccion {
    /** Cuándo apareció el primer código (ms); `null` mientras no hay ninguno. */
    desde: number | null;
    codigos: CodigoVisto[];
}

export const COLECCION_VACIA: Coleccion = { desde: null, codigos: [] };

/** Suma los códigos de un cuadro (sin repetidos). La ventana empieza con el primero. */
export function juntarCodigos(c: Coleccion, nuevos: readonly CodigoVisto[], ahora: number): Coleccion {
    const codigos = deduplicarCodigos([...c.codigos, ...nuevos]);
    if (codigos.length === c.codigos.length) {
        return c;
    }
    return { desde: c.desde ?? ahora, codigos };
}

/** La ventana terminó: hay al menos un código y pasaron `ventanaMs` desde el primero. */
export function coleccionLista(c: Coleccion, ahora: number, ventanaMs = VENTANA_ESCANEO_MS): boolean {
    return c.desde !== null && c.codigos.length > 0 && ahora - c.desde >= ventanaMs;
}

// --- Sesión de escaneo: lo que hacen igual la PWA y la app nativa ---

export interface EstadoEscaneo {
    /** Códigos distintos que ya cuentan («Detectados: N»). */
    detectados: number;
    /** Al terminar la ventana, la lista para elegir; `null` mientras se escanea. */
    candidatos: Candidato[] | null;
}

export interface OpcionesSesionEscaneo {
    objetivo: ObjetivoEscaneo;
    /** Equipos ya registrados en esta orden (se leen al armar la lista). */
    existentes?: () => readonly { serie?: string | null; mac?: string | null }[];
    alCambiar: (estado: EstadoEscaneo) => void;
    /** Con el primer código de la ventana (vibración corta). */
    alPrimerCodigo?: () => void;
    ventanaMs?: number;
    ahora?: () => number;
    /** Programa la lista al terminar la ventana; devuelve cómo cancelarla. */
    programar?: (fn: () => void, ms: number) => () => void;
}

const programarConTemporizador = (fn: () => void, ms: number) => {
    const t = setTimeout(fn, ms);
    return () => clearTimeout(t);
};

/**
 * Una apertura del escáner: junta lo que cae dentro del recuadro, avisa con el primero, al terminar
 * la ventana arma los candidatos y deja de juntar hasta «Seguir escaneando». Si no aparece nada,
 * sigue esperando: nunca se elige solo.
 */
export class SesionEscaneo {
    private coleccion: Coleccion = COLECCION_VACIA;
    private cancelar: (() => void) | null = null;
    estado: EstadoEscaneo = { detectados: 0, candidatos: null };

    constructor(private readonly o: OpcionesSesionEscaneo) {}

    /** Los códigos de un cuadro que ya cuentan (dentro del recuadro, ver `dentroDelRecuadro`). */
    detectar(codigos: readonly CodigoVisto[]): void {
        if (this.estado.candidatos !== null || codigos.length === 0) {
            return;
        }
        const antes = this.coleccion;
        this.coleccion = juntarCodigos(antes, codigos, (this.o.ahora ?? Date.now)());
        if (this.coleccion === antes) {
            return;
        }
        this.cambiar({ detectados: this.coleccion.codigos.length, candidatos: null });
        if (antes.desde === null) {
            this.o.alPrimerCodigo?.();
            this.cancelar = (this.o.programar ?? programarConTemporizador)(() => {
                this.cancelar = null;
                this.cambiar({ detectados: this.coleccion.codigos.length, candidatos: candidatosPara(this.o.objetivo, this.coleccion.codigos, this.o.existentes?.() ?? []) });
            }, this.o.ventanaMs ?? VENTANA_ESCANEO_MS);
        }
    }

    /** «Seguir escaneando»: lista cerrada y ventana nueva. */
    seguir(): void {
        this.liberar();
        this.coleccion = COLECCION_VACIA;
        this.cambiar({ detectados: 0, candidatos: null });
    }

    /** Al cerrar el escáner: sin la lista programada. */
    liberar(): void {
        this.cancelar?.();
        this.cancelar = null;
    }

    private cambiar(estado: EstadoEscaneo): void {
        this.estado = estado;
        this.o.alCambiar(estado);
    }
}
