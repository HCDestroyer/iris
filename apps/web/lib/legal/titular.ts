import type { Schema } from "@iris/contratos";
import type { Idioma } from "./paginas";

/*
 * El titular (quién opera la instalación) tal como lo usan las páginas públicas. Sale de
 * `GET /api/v1/publico/datos-legales`; lo que falte nunca deja un hueco: el texto usa un nombre
 * genérico y las líneas de contacto vacías no se muestran.
 */

export type DatosPublicos = Schema<"DatosPublicos">;

export type Titular = {
    /** Nombre con que el texto llama a la empresa; genérico si no hay ninguno. */
    nombre: string;
    /** Hay un nombre real (comercial o razón social). */
    conNombre: boolean;
    /** Razón social, si es distinta del nombre. */
    razonSocial: string | null;
    correo: string | null;
    telefono: string | null;
    direccion: string | null;
    pais: string | null;
    actualizadoEn: string | null;
};

const GENERICO: Record<Idioma, string> = { es: "el proveedor de este servicio", en: "the provider of this service" };

const limpio = (v: string | null | undefined): string | null => {
    const t = (v ?? "").trim();
    return t === "" ? null : t;
};

/** Arma el titular con lo que haya. `datos` null: no se pudo leer (o nunca se configuró). */
export function titularDe(datos: Partial<DatosPublicos> | null, idioma: Idioma): Titular {
    const comercial = limpio(datos?.nombreComercial);
    const razon = limpio(datos?.razonSocial);
    const nombre = comercial ?? razon;
    return {
        nombre: nombre ?? GENERICO[idioma],
        conNombre: nombre !== null,
        razonSocial: razon !== null && razon !== nombre ? razon : null,
        correo: limpio(datos?.correo),
        telefono: limpio(datos?.telefono),
        direccion: limpio(datos?.direccion),
        pais: limpio(datos?.pais),
        actualizadoEn: limpio(datos?.actualizadoEn),
    };
}

/** Fecha en que se revisó por última vez el texto de las páginas (cambia con cada versión del texto). */
// Al mediodía UTC: el mismo día en la zona en que se muestra (America/Guatemala).
export const TEXTO_REVISADO_EN = "2026-11-18T12:00:00Z";

/** «Última actualización»: lo más reciente entre el texto y los datos del titular. */
export function ultimaActualizacion(t: Pick<Titular, "actualizadoEn">): Date {
    const texto = new Date(TEXTO_REVISADO_EN);
    const datos = t.actualizadoEn ? new Date(t.actualizadoEn) : null;
    return datos && !Number.isNaN(datos.getTime()) && datos > texto ? datos : texto;
}

const ZONA = "America/Guatemala";

/** `AAAA-MM-DD` del mismo día que muestra `fechaLarga` (para `<time dateTime>`). */
export function fechaISO(fecha: Date): string {
    return new Intl.DateTimeFormat("en-CA", { year: "numeric", month: "2-digit", day: "2-digit", timeZone: ZONA }).format(fecha);
}

export function fechaLarga(fecha: Date, idioma: Idioma): string {
    return new Intl.DateTimeFormat(idioma === "es" ? "es-GT" : "en-US", { dateStyle: "long", timeZone: ZONA }).format(fecha);
}
