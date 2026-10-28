// Lector mínimo de EXIF de un JPEG: hora de la toma y GPS. Sin dependencias; ante cualquier dato
// raro devuelve lo que pudo leer (o nada). La compresión en canvas quita el EXIF del archivo, así
// que hora y GPS viajan aparte en el alta del adjunto (`tomadoEn`, `lat`, `lon`).

export interface DatosExif {
    /** ISO 8601 UTC. */
    tomadoEn?: string;
    lat?: number;
    lon?: number;
}

const TAG_EXIF = 0x8769;
const TAG_GPS = 0x8825;
const TAG_FECHA_ORIGINAL = 0x9003;
const TAG_DESFASE_ORIGINAL = 0x9011;
const TAG_FECHA = 0x0132;

type Lector = { u16: (o: number) => number; u32: (o: number) => number };

function lector(vista: DataView, le: boolean): Lector {
    return { u16: (o) => vista.getUint16(o, le), u32: (o) => vista.getUint32(o, le) };
}

interface Entrada {
    tipo: number;
    cuenta: number;
    /** Desplazamiento del valor (dentro del TIFF) o del campo en línea. */
    valor: number;
}

function leerIfd(l: Lector, tiff: number, inicio: number, largo: number): Map<number, Entrada> {
    const entradas = new Map<number, Entrada>();
    if (inicio + 2 > largo) {
        return entradas;
    }
    const n = l.u16(tiff + inicio);
    for (let i = 0; i < n; i += 1) {
        const o = tiff + inicio + 2 + i * 12;
        if (o + 12 > tiff + largo) {
            break;
        }
        const tipo = l.u16(o + 2);
        const cuenta = l.u32(o + 4);
        const tamanoTipo = tipo === 5 || tipo === 10 ? 8 : tipo === 3 ? 2 : tipo === 4 || tipo === 9 ? 4 : 1;
        const enLinea = tamanoTipo * cuenta <= 4;
        entradas.set(l.u16(o), { tipo, cuenta, valor: enLinea ? o + 8 - tiff : l.u32(o + 8) });
    }
    return entradas;
}

function texto(bytes: Uint8Array, tiff: number, e: Entrada | undefined): string | undefined {
    if (!e || e.tipo !== 2) {
        return undefined;
    }
    const desde = tiff + e.valor;
    const crudo = bytes.subarray(desde, desde + e.cuenta);
    return new TextDecoder("latin1").decode(crudo).replace(/\0+$/, "");
}

function racionales(vista: DataView, l: Lector, tiff: number, e: Entrada | undefined, n: number): number[] | undefined {
    if (!e || e.tipo !== 5 || e.cuenta < n) {
        return undefined;
    }
    const valores: number[] = [];
    for (let i = 0; i < n; i += 1) {
        const o = tiff + e.valor + i * 8;
        if (o + 8 > vista.byteLength) {
            return undefined;
        }
        const den = l.u32(o + 4);
        valores.push(den === 0 ? 0 : l.u32(o) / den);
    }
    return valores;
}

/** `2026:10:28 09:15:30` (hora local de la cámara) + desfase (`-06:00`) → ISO UTC. */
export function fechaExifAIso(fecha: string, desfase = "-06:00"): string | undefined {
    const m = /^(\d{4}):(\d{2}):(\d{2}) (\d{2}):(\d{2}):(\d{2})/.exec(fecha);
    if (!m) {
        return undefined;
    }
    const z = /^[+-]\d{2}:\d{2}$/.test(desfase) ? desfase : "-06:00";
    const ms = Date.parse(`${m[1]}-${m[2]}-${m[3]}T${m[4]}:${m[5]}:${m[6]}${z}`);
    return Number.isNaN(ms) ? undefined : new Date(ms).toISOString();
}

/**
 * Hora de la toma (`DateTimeOriginal` con su `OffsetTimeOriginal`, o Guatemala −06:00 si no lo
 * trae) y GPS de un JPEG. `{}` si no es JPEG o no tiene EXIF.
 */
export function leerExif(bytes: Uint8Array): DatosExif {
    try {
        const vista = new DataView(bytes.buffer, bytes.byteOffset, bytes.byteLength);
        if (vista.getUint16(0) !== 0xffd8) {
            return {};
        }
        let o = 2;
        while (o + 4 <= vista.byteLength) {
            const marca = vista.getUint16(o);
            const largo = vista.getUint16(o + 2);
            if (marca === 0xffe1 && vista.getUint32(o + 4) === 0x45786966) {
                return leerTiff(bytes, vista, o + 10, largo - 8);
            }
            if ((marca & 0xff00) !== 0xff00 || marca === 0xffda) {
                return {};
            }
            o += 2 + largo;
        }
    } catch {
        // EXIF mal formado: sin datos.
    }
    return {};
}

function leerTiff(bytes: Uint8Array, vista: DataView, tiff: number, largo: number): DatosExif {
    const le = vista.getUint16(tiff) === 0x4949;
    const l = lector(vista, le);
    const ifd0 = leerIfd(l, tiff, l.u32(tiff + 4), largo);
    const resultado: DatosExif = {};
    const exif = ifd0.get(TAG_EXIF);
    const sub = exif ? leerIfd(l, tiff, l.u32(tiff + exif.valor), largo) : new Map<number, Entrada>();
    const fecha = texto(bytes, tiff, sub.get(TAG_FECHA_ORIGINAL)) ?? texto(bytes, tiff, ifd0.get(TAG_FECHA));
    if (fecha) {
        const iso = fechaExifAIso(fecha, texto(bytes, tiff, sub.get(TAG_DESFASE_ORIGINAL)));
        if (iso) {
            resultado.tomadoEn = iso;
        }
    }
    const gps = ifd0.get(TAG_GPS);
    if (gps) {
        const g = leerIfd(l, tiff, l.u32(tiff + gps.valor), largo);
        const lat = racionales(vista, l, tiff, g.get(2), 3);
        const lon = racionales(vista, l, tiff, g.get(4), 3);
        const refLat = texto(bytes, tiff, g.get(1));
        const refLon = texto(bytes, tiff, g.get(3));
        if (lat && lon) {
            const grados = ([d = 0, m = 0, s = 0]: number[]) => d + m / 60 + s / 3600;
            const la = grados(lat) * (refLat === "S" ? -1 : 1);
            const lo = grados(lon) * (refLon === "W" ? -1 : 1);
            if (Math.abs(la) <= 90 && Math.abs(lo) <= 180 && !(la === 0 && lo === 0)) {
                resultado.lat = Math.round(la * 1e6) / 1e6;
                resultado.lon = Math.round(lo * 1e6) / 1e6;
            }
        }
    }
    return resultado;
}
