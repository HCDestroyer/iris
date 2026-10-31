import type { Schema } from "@iris/contratos";

export type DatosArgosCambiados = Schema<"DatosArgosCambiados">;

/**
 * «Dato de Argos cambió» de una orden. Viene en `Orden` (lista y tablero); el detalle
 * (`OrdenDetalle`) aún no lo trae porque su `datosArgos` (la copia) tapa al de `Orden` en la API
 * (pedido en .claude/integration/i3.md): se lee si llega y, si no, no se muestra nada.
 */
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
