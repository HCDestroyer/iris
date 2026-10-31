import type { Schema } from "@iris/contratos";
import { z } from "zod";
import { ErrorApi } from "@/lib/api/errores";

export type DatosArgosCambiados = Schema<"DatosArgosCambiados">;
export type CampoAplicable = NonNullable<Schema<"EntradaAplicarDatosArgos">["campos"]>[number];

/** «Dato de Argos cambió» de una orden (lista, tablero y detalle); sin aviso, `undefined`. */
export function cambiosDeArgos(orden: object): DatosArgosCambiados | undefined {
    const valor = (orden as { datosArgosCambiados?: unknown }).datosArgosCambiados;
    if (!valor || typeof valor !== "object") return undefined;
    const { en, campos } = valor as Partial<DatosArgosCambiados>;
    if (typeof en !== "string") return undefined;
    return { en, campos: Array.isArray(campos) ? campos.filter((c): c is string => typeof c === "string") : null };
}

const NOMBRES_CAMPO: Readonly<Record<string, string>> = {
    "servicio.direccion": "dirección del servicio",
    "servicio.lat": "ubicación del servicio",
    "servicio.lon": "ubicación del servicio",
    "servicio.referencia": "referencia del servicio",
    "servicio.contacto_nombre": "contacto en sitio",
    "servicio.contacto_telefono": "teléfono de contacto en sitio",
    "servicio.contacto_telefonos": "teléfono de contacto en sitio",
    "cliente.direccion": "dirección del cliente",
    "cliente.referencia": "referencia del cliente",
    "cliente.lat": "ubicación del cliente",
    "cliente.lon": "ubicación del cliente",
};

/** Qué cambió, en palabras y sin repetir («dirección del servicio, contacto en sitio»). */
export function textoCamposArgos(campos: readonly string[] | null | undefined): string {
    const textos = [...new Set((campos ?? []).map((c) => NOMBRES_CAMPO[c] ?? c))];
    return textos.join(", ");
}

/** Lo que se puede aplicar a la orden, en el orden del formulario. */
export const CAMPOS_APLICABLES: readonly { campo: CampoAplicable; texto: string }[] = [
    { campo: "direccion", texto: "Dirección" },
    { campo: "coordenadas", texto: "Coordenadas" },
    { campo: "referencia", texto: "Referencia" },
    { campo: "contacto", texto: "Contacto en sitio" },
];

/** Qué grupo aplicable toca cada campo avisado por Argos (`servicio.direccion` → `direccion`). */
export function campoAplicable(campo: string): CampoAplicable | undefined {
    const nombre = campo.split(".").at(-1) ?? "";
    if (nombre === "direccion") return "direccion";
    if (nombre === "lat" || nombre === "lon") return "coordenadas";
    if (nombre === "referencia") return "referencia";
    if (nombre.startsWith("contacto")) return "contacto";
    return undefined;
}

/** Marcados al abrir «Aplicar»: los que cambiaron; si no se reconoce ninguno, todos. */
export function camposIniciales(cambios: DatosArgosCambiados): CampoAplicable[] {
    const avisados = new Set((cambios.campos ?? []).map(campoAplicable).filter((c): c is CampoAplicable => c !== undefined));
    const lista = CAMPOS_APLICABLES.map((c) => c.campo).filter((c) => avisados.has(c));
    return lista.length > 0 ? lista : CAMPOS_APLICABLES.map((c) => c.campo);
}

export const esquemaAplicarDatosArgos = z.object({
    campos: z.array(z.enum(["direccion", "coordenadas", "referencia", "contacto"])).min(1, "Elige al menos un dato para aplicar"),
});
export type DatosAplicarArgos = z.infer<typeof esquemaAplicarDatosArgos>;

export const esquemaDescartarDatosArgos = z.object({
    motivo: z.string().trim().max(500, "Máximo 500 caracteres"),
});
export type DatosDescartarArgos = z.infer<typeof esquemaDescartarDatosArgos>;

/** Cuerpo de `POST /ordenes/{id}/aplicar-datos-argos` (con la versión y el aviso que se leyeron). */
export function cuerpoAplicar(d: DatosAplicarArgos, version: number, cambios: DatosArgosCambiados): Schema<"EntradaAplicarDatosArgos"> {
    const campos = CAMPOS_APLICABLES.map((c) => c.campo).filter((c) => d.campos.includes(c));
    return { version, avisoEn: cambios.en, ...(campos.length < CAMPOS_APLICABLES.length ? { campos } : {}) };
}

/** Cuerpo de `POST /ordenes/{id}/descartar-datos-argos`. */
export function cuerpoDescartar(d: DatosDescartarArgos, version: number, cambios: DatosArgosCambiados): Schema<"EntradaDescartarDatosArgos"> {
    const motivo = d.motivo.trim();
    return { version, avisoEn: cambios.en, ...(motivo ? { motivo } : {}) };
}

/**
 * Qué hacer con un error de aplicar o descartar:
 * - `aviso_nuevo` (409 `aviso_argos_cambiado`): Argos avisó algo después; se recarga y se muestra.
 * - `version` (409): la orden cambió; se recarga.
 * - `mensaje`: un problema que despacho resuelve a mano (servicio de otro cliente, sin instantánea).
 */
export type ErrorDatosArgos = { tipo: "aviso_nuevo" | "version" } | { tipo: "mensaje"; titulo: string; texto: string } | { tipo: "otro" };

export function errorDatosArgos(e: unknown): ErrorDatosArgos {
    if (!(e instanceof ErrorApi)) return { tipo: "otro" };
    if (e.estado === 409 && e.codigo === "aviso_argos_cambiado") return { tipo: "aviso_nuevo" };
    if (e.estado === 409 && e.codigo !== "idempotencia_en_proceso") return { tipo: "version" };
    if (e.codigo === "instantanea_no_coincide") {
        return { tipo: "mensaje", titulo: "No se aplicó", texto: "El servicio en Argos ya no es de este cliente; revisa la orden a mano." };
    }
    if (e.codigo === "sin_instantanea_argos") {
        return { tipo: "mensaje", titulo: "No se aplicó", texto: "Iris todavía no tiene los datos de Argos de este servicio; revisa la orden a mano." };
    }
    if (e.codigo === "sin_aviso_argos") {
        return { tipo: "mensaje", titulo: "Ya no hay aviso", texto: "Otra persona ya atendió el aviso de Argos de esta orden." };
    }
    return { tipo: "otro" };
}
