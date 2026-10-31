// La orden por pasos independientes (Llegada · Checklist · Materiales y equipos · Fotos) y el paso
// final «Firma del cliente y cierre»: lo que muestran el avance y la navegación entre pasos. Las
// reglas son de `@iris/sync/campo` (`estadoPasos`); aquí solo se arman los textos. Sin React.
import type { OrdenVista } from "@iris/sync";
import { estadoPasos, faltantesAntesDeFirmar, llego, PASOS_ORDEN, progresoPasos, siguientePaso, TITULOS_PASO, vecinosPaso, type PasoOrden, type Vista } from "@iris/sync/campo";

export type MarcaPaso = "listo" | "opcional" | "falta" | "bloqueado" | "para_firmar" | "hecho";

export interface PasoEnAvance {
    paso: PasoOrden;
    numero: number;
    titulo: string;
    marca: MarcaPaso;
    /** «Listo», «Falta 1», «Al terminar lo demás»… */
    detalle: string;
    /** Para lectores de pantalla: «2. Checklist: faltan 1». */
    etiqueta: string;
}

export interface Avance {
    listos: number;
    total: number;
    /** «N de 4 pasos listos». */
    texto: string;
    pasos: PasoEnAvance[];
}

const faltanTexto = (n: number) => `Falta${n === 1 ? "" : "n"} ${n}`;

export function avanceOrden(orden: OrdenVista): Avance {
    const { listos, total } = progresoPasos(orden);
    const listaParaFirmar = faltantesAntesDeFirmar(orden).length === 0;
    const cerrada = orden.estado === "cerrada";
    const pasos = estadoPasos(orden).map((p, i): PasoEnAvance => {
        let marca: MarcaPaso;
        let detalle: string;
        if (p.paso === "cierre") {
            marca = cerrada ? "hecho" : listaParaFirmar ? "para_firmar" : "bloqueado";
            detalle = cerrada ? "Hecho" : listaParaFirmar ? "Listo para firmar" : "Al terminar lo demás";
        } else if (p.completo) {
            marca = p.opcional ? "opcional" : "listo";
            detalle = p.opcional ? "Opcional" : "Listo";
        } else {
            marca = "falta";
            detalle = faltanTexto(p.faltan.length);
        }
        return { paso: p.paso, numero: i + 1, titulo: p.titulo, marca, detalle, etiqueta: `${i + 1}. ${p.titulo}: ${detalle.toLowerCase()}` };
    });
    return { listos, total, texto: `${listos} de ${total} pasos listos`, pasos };
}

/** «Empezar trabajo» lleva a la llegada; «Continuar trabajo», al primer paso con algo pendiente. */
export function botonTrabajo(orden: OrdenVista): { texto: string; vista: Vista } {
    const empezado = llego(orden);
    return { texto: empezado ? "Continuar trabajo" : "Empezar trabajo", vista: { tipo: "orden", id: orden.id, paso: empezado ? siguientePaso(orden) : "llegada" } };
}

/** Encabezado y pie de un paso: «Paso 2 de 5» (o «Paso final»), «Anterior» / «Siguiente». */
export function navegacionPaso(orden: Pick<OrdenVista, "id">, paso: PasoOrden) {
    const { anterior, siguiente } = vecinosPaso(paso);
    const numero = PASOS_ORDEN.indexOf(paso) + 1;
    return {
        posicion: paso === "cierre" ? "Paso final" : `Paso ${numero} de ${PASOS_ORDEN.length}`,
        anterior: anterior ? { texto: "Anterior", vista: { tipo: "orden", id: orden.id, paso: anterior } as Vista } : { texto: "Resumen", vista: { tipo: "orden", id: orden.id } as Vista },
        siguiente: siguiente
            ? { texto: siguiente === "cierre" ? "Firma y cierre" : `Siguiente: ${TITULOS_PASO[siguiente].toLowerCase()}`, vista: { tipo: "orden", id: orden.id, paso: siguiente } as Vista }
            : null,
    };
}
