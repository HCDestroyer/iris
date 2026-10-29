// Fotos de la app nativa: cámara del sistema (expo-image-picker, con EXIF), compresión a lado
// mayor ≤ 1600 px y ~300 KB en JPEG (expo-image-manipulator), como la PWA. La hora y el GPS del
// EXIF viajan en el alta del adjunto (`tomadoEn`, `lat`, `lon`). Lo puro se prueba con Jest.
import { File } from "expo-file-system";
import { manipulateAsync, SaveFormat } from "expo-image-manipulator";
import * as ImagePicker from "expo-image-picker";

export const LADO_MAXIMO = 1600;
export const BYTES_OBJETIVO = 300 * 1024;
const CALIDADES = [0.82, 0.72, 0.62, 0.52, 0.42];

export interface FotoComprimida {
    bytes: Uint8Array;
    mime: "image/jpeg";
    ancho: number;
    alto: number;
    /** Archivo comprimido en la caché del teléfono (miniatura de esta visita). */
    uri?: string;
}

/** Medidas finales conservando la proporción, sin agrandar. */
export function dimensionesObjetivo(ancho: number, alto: number, maximo = LADO_MAXIMO): { ancho: number; alto: number } {
    const mayor = Math.max(ancho, alto);
    if (mayor <= maximo || mayor === 0) {
        return { ancho: Math.round(ancho), alto: Math.round(alto) };
    }
    const f = maximo / mayor;
    return { ancho: Math.max(1, Math.round(ancho * f)), alto: Math.max(1, Math.round(alto * f)) };
}

export type Codificador = (lado: number, calidad: number) => Promise<FotoComprimida>;

/** Calidades de mayor a menor y, si no cabe, lado más chico; si nada cabe, la más chica lograda. */
export async function comprimirConTope(codificar: Codificador, lado: number, objetivo = BYTES_OBJETIVO): Promise<FotoComprimida> {
    let mejor: FotoComprimida | null = null;
    for (const l of [lado, Math.round(lado * 0.8), Math.round(lado * 0.64)]) {
        for (const calidad of CALIDADES) {
            const foto = await codificar(l, calidad);
            if (!mejor || foto.bytes.byteLength < mejor.bytes.byteLength) {
                mejor = foto;
            }
            if (foto.bytes.byteLength <= objetivo) {
                return foto;
            }
        }
    }
    if (!mejor) {
        throw new Error("No se pudo comprimir la foto");
    }
    return mejor;
}

export interface DatosExif {
    /** ISO 8601 UTC. */
    tomadoEn?: string;
    lat?: number;
    lon?: number;
}

type Diccionario = Record<string, unknown>;

const esDiccionario = (v: unknown): v is Diccionario => typeof v === "object" && v !== null && !Array.isArray(v);

/** Número de un valor EXIF: número, «14.6», racional «14/1» o grados «14/1,36/1,2850/100». */
function numeroExif(v: unknown): number | undefined {
    if (typeof v === "number" && Number.isFinite(v)) {
        return v;
    }
    if (typeof v !== "string") {
        return undefined;
    }
    const partes = v.split(",").map((p) => {
        const [n, d] = p.trim().split("/");
        const valor = d === undefined ? Number(n) : Number(n) / Number(d);
        return Number.isFinite(valor) ? valor : Number.NaN;
    });
    if (partes.some(Number.isNaN) || partes.length === 0 || partes.length > 3) {
        return undefined;
    }
    const [g = 0, m = 0, s = 0] = partes;
    return g + m / 60 + s / 3600;
}

function coordenada(valor: unknown, referencia: unknown, negativa: string): number | undefined {
    const n = numeroExif(valor);
    if (n === undefined) {
        return undefined;
    }
    const signo = typeof referencia === "string" && referencia.toUpperCase().startsWith(negativa) ? -1 : 1;
    return Math.round(signo * Math.abs(n) * 1e6) / 1e6;
}

/** «2026:10:28 09:15:00» (+ «-06:00» si vino) → ISO UTC; sin desfase, es la hora local del teléfono. */
function fechaExif(texto: unknown, desfase: unknown): string | undefined {
    if (typeof texto !== "string") {
        return undefined;
    }
    const m = /^(\d{4}):(\d{2}):(\d{2})[ T](\d{2}):(\d{2}):(\d{2})/.exec(texto.trim());
    if (!m) {
        return undefined;
    }
    const [, a, mes, d, h, mi, s] = m;
    const zona = typeof desfase === "string" && /^[+-]\d{2}:\d{2}$/.test(desfase.trim()) ? desfase.trim() : null;
    const ms = zona
        ? Date.parse(`${a}-${mes}-${d}T${h}:${mi}:${s}${zona}`)
        : new Date(Number(a), Number(mes) - 1, Number(d), Number(h), Number(mi), Number(s)).getTime();
    return Number.isNaN(ms) ? undefined : new Date(ms).toISOString();
}

/**
 * Hora y GPS del EXIF que da expo-image-picker. Android lo da plano (`GPSLatitude`, `DateTimeOriginal`);
 * iOS anidado (`{GPS}.Latitude`, `{Exif}.DateTimeOriginal`). Lo que no se entienda se omite.
 */
export function datosDeExif(exif: Diccionario | null | undefined): DatosExif {
    if (!exif) {
        return {};
    }
    const gps = esDiccionario(exif["{GPS}"]) ? exif["{GPS}"] : {};
    const ex = esDiccionario(exif["{Exif}"]) ? exif["{Exif}"] : {};
    const lat = coordenada(exif.GPSLatitude ?? gps.Latitude, exif.GPSLatitudeRef ?? gps.LatitudeRef, "S");
    const lon = coordenada(exif.GPSLongitude ?? gps.Longitude, exif.GPSLongitudeRef ?? gps.LongitudeRef, "W");
    const tomadoEn = fechaExif(exif.DateTimeOriginal ?? ex.DateTimeOriginal ?? exif.DateTime, exif.OffsetTimeOriginal ?? ex.OffsetTimeOriginal);
    const valido = lat !== undefined && lon !== undefined && Math.abs(lat) <= 90 && Math.abs(lon) <= 180 && !(lat === 0 && lon === 0);
    return { ...(tomadoEn ? { tomadoEn } : {}), ...(valido ? { lat, lon } : {}) };
}

export interface FotoTomada {
    foto: FotoComprimida;
    exif: DatosExif;
}

/** Abre la cámara; `null` si el técnico cancela. Lanza si no hay permiso. */
export async function tomarFoto(): Promise<FotoTomada | null> {
    const permiso = await ImagePicker.requestCameraPermissionsAsync();
    if (!permiso.granted) {
        throw new Error("Sin permiso de cámara: actívalo en los ajustes del teléfono.");
    }
    const r = await ImagePicker.launchCameraAsync({ mediaTypes: ["images"], quality: 1, exif: true, allowsEditing: false });
    const original = r.canceled ? undefined : r.assets[0];
    if (!original) {
        return null;
    }
    const lado = Math.min(LADO_MAXIMO, Math.max(original.width, original.height));
    const foto = await comprimirConTope(async (l, calidad) => {
        const medidas = dimensionesObjetivo(original.width, original.height, l);
        const r2 = await manipulateAsync(original.uri, [{ resize: { width: medidas.ancho, height: medidas.alto } }], { compress: calidad, format: SaveFormat.JPEG });
        const bytes = new Uint8Array(await new File(r2.uri).arrayBuffer());
        return { bytes, mime: "image/jpeg", ancho: r2.width, alto: r2.height, uri: r2.uri };
    }, lado);
    return { foto, exif: datosDeExif(original.exif) };
}
