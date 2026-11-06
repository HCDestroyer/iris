// Paso de materiales de la orden (PWA y app nativa): buscar en las existencias de la bodega del
// técnico por nombre o código, la modalidad de cada producto (los equipos en comodato van con su
// serie en «Equipos», no como material) y cuánto quedaría con la cantidad elegida. Todo sale del
// paquete del día (sin señal también), ya descontado lo de la cola.
import type { Existencia, ExistenciasBodega } from "../contrato";
import type { TonoEstado } from "./textos";

/** Minúsculas y sin tildes, para buscar «cable» en «Cablé» o «conector sc» en «Conector SC/APC». */
export function normalizarBusqueda(texto: string): string {
    return texto
        .normalize("NFD")
        .replace(/[̀-ͯ]/g, "")
        .toLowerCase()
        .replace(/\s+/g, " ")
        .trim();
}

/**
 * Las líneas que coinciden con lo escrito (cada palabra en el nombre o el código), primero las que
 * empiezan igual y las que tienen existencia; sin texto, todas en orden alfabético.
 */
export function buscarMateriales(lineas: readonly Existencia[], texto: string): Existencia[] {
    const q = normalizarBusqueda(texto);
    const palabras = q === "" ? [] : q.split(" ");
    const puntaje = (l: Existencia): number | null => {
        const nombre = normalizarBusqueda(l.descripcion);
        const codigo = normalizarBusqueda(l.codigo ?? "");
        const junto = `${nombre} ${codigo}`;
        if (!palabras.every((p) => junto.includes(p))) return null;
        if (q === "") return 0;
        if (codigo !== "" && codigo === q) return 0;
        if (nombre.startsWith(q) || (codigo !== "" && codigo.startsWith(q))) return 1;
        return 2;
    };
    return lineas
        .map((l) => ({ l, p: puntaje(l) }))
        .filter((x): x is { l: Existencia; p: number } => x.p !== null)
        .sort((a, b) => a.p - b.p || Number(b.l.cantidad > 0) - Number(a.l.cantidad > 0) || a.l.descripcion.localeCompare(b.l.descripcion, "es"))
        .map((x) => x.l);
}

export type TipoMaterial = {
    /** Texto corto para la etiqueta (icono + texto + color). */
    texto: string;
    tono: TonoEstado;
    /** `false`: es un equipo con serie, se registra en «Equipos». */
    registrable: boolean;
    /** Qué pasa al usarlo, para el técnico. */
    ayuda: string;
};

/** La modalidad del producto en Argos (≥ 1.6.0), en palabras del técnico. */
export function tipoMaterial(modalidad: Existencia["modalidad"] | undefined): TipoMaterial {
    switch (modalidad) {
        case "comodato":
            return {
                texto: "Equipo con serie",
                tono: "info",
                registrable: false,
                ayuda: "Es un equipo en comodato: regístralo en «Equipos» con su número de serie (escríbelo o escanéalo).",
            };
        case "consumible_cobro":
            return { texto: "Se cobra al cliente", tono: "warning", registrable: true, ayuda: "Argos lo cobra al cliente del servicio al consumirlo." };
        case "consumible_gasto":
            return { texto: "Consumible", tono: "neutral", registrable: true, ayuda: "Se descuenta de tu bodega, sin cobro al cliente." };
        default:
            return { texto: "", tono: "neutral", registrable: true, ayuda: "" };
    }
}

const fmt = new Intl.NumberFormat("es-GT", { maximumFractionDigits: 3 });

/** «120 m», con la unidad si la hay. */
export function textoCantidad(cantidad: number, unidad: string): string {
    return `${fmt.format(cantidad)}${unidad ? ` ${unidad}` : ""}`;
}

/** Nombre de la bodega para «De tu bodega …»: su nombre en Argos o, sin él, su número. */
export function nombreBodegaCampo(b: Pick<ExistenciasBodega, "bodegaId" | "nombre">): string {
    const n = (b.nombre ?? "").trim();
    return n !== "" ? n : `Bodega ${b.bodegaId}`;
}

/** Texto secundario de una línea: «CAB-01 · Quedan 120 m · Consumible». */
export function detalleMaterial(l: Existencia): string {
    const tipo = tipoMaterial(l.modalidad);
    return [l.codigo ?? "", l.cantidad > 0 ? `Quedan ${textoCantidad(l.cantidad, l.unidad)}` : "Sin existencia", tipo.texto].filter((x) => x !== "").join(" · ");
}

/** Cantidad del formulario («2,5») como número; `null` si no es una cantidad válida (> 0). */
export function cantidadDeTexto(texto: string): number | null {
    const n = Number(texto.trim().replace(",", "."));
    return texto.trim() !== "" && Number.isFinite(n) && n > 0 ? n : null;
}

/**
 * Lo que quedaría en la bodega después de usar la cantidad: `excede` si es más de lo que hay (se
 * registra igual y se avisa al supervisor, como en el servidor). `null` sin cantidad válida.
 */
export function existenciaDespues(l: Pick<Existencia, "cantidad">, texto: string): { quedan: number; excede: boolean } | null {
    const c = cantidadDeTexto(texto);
    if (c === null) return null;
    const quedan = Math.round((l.cantidad - c) * 1000) / 1000;
    return { quedan, excede: quedan < 0 };
}
