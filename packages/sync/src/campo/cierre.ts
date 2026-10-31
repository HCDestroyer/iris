// «Firma del cliente y cierre», el paso final y único de la orden: el resumen que ve el cliente
// antes de firmar y cómo se encolan la firma y el cierre juntos. Compartido con la app nativa.
import { valorDe } from "../aplicar";
import type { ItemChecklist } from "../contrato";
import type { MotorSync, OrdenVista } from "../motor";
import { TIPOS_ORDEN } from "./textos";

const plural = (n: number, uno: string, varios: string) => `${n} ${n === 1 ? uno : varios}`;

/** Valor de un ítem del checklist en palabras («Sí», «-18,5 dBm», «Foto»). */
export function textoValorItem(item: Pick<ItemChecklist, "tipo" | "unidad" | "valor"> & Parameters<typeof valorDe>[0]): string {
    const v = valorDe(item);
    if (typeof v === "boolean") {
        return v ? "Sí" : "No";
    }
    if (typeof v === "number") {
        return `${v.toLocaleString("es-GT")}${item.unidad ? ` ${item.unidad}` : ""}`;
    }
    return item.tipo === "foto" ? "Foto" : String(v ?? "");
}

export interface ResumenTrabajo {
    /** «Instalación · checklist 3/4 · 2 fotos · 1 material · 1 equipo». */
    descripcion: string;
    checklist: { clave: string; etiqueta: string; valor: string }[];
    materiales: { clave: string; descripcion: string; cantidad: string }[];
    equipos: { clave: string; texto: string; serie: string | null }[];
}

/** Lo hecho en la orden, en palabras del cliente: checklist respondido, fotos, materiales y equipos. */
export function resumenTrabajo(orden: Pick<OrdenVista, "tipo" | "checklist" | "adjuntos" | "materiales" | "equipos">): ResumenTrabajo {
    const items = orden.checklist ?? [];
    const respondidos = items.filter((i) => {
        const v = valorDe(i);
        return v !== null && v !== "";
    });
    const fotos = (orden.adjuntos ?? []).filter((a) => a.tipo === "foto").length;
    const materiales = orden.materiales ?? [];
    const equipos = orden.equipos ?? [];
    return {
        descripcion: [
            TIPOS_ORDEN[orden.tipo],
            `checklist ${respondidos.length}/${items.length}`,
            plural(fotos, "foto", "fotos"),
            plural(materiales.length, "material", "materiales"),
            plural(equipos.length, "equipo", "equipos"),
        ].join(" · "),
        checklist: respondidos.map((i) => ({ clave: `${i.formularioId ?? ""}:${i.codigo}`, etiqueta: i.etiqueta, valor: textoValorItem(i) })),
        materiales: materiales.map((m) => ({ clave: m.id, descripcion: m.descripcion || `Producto ${m.productoId}`, cantidad: m.cantidad.toLocaleString("es-GT") })),
        equipos: equipos.map((e) => ({ clave: e.id, texto: `${e.accion === "instalar" ? "Instalado" : "Retirado"}: ${e.modelo || "equipo"}`, serie: e.serie || null })),
    };
}

export interface EntradaFirmaYCierre {
    orden: Pick<OrdenVista, "id" | "requiereFirma">;
    /** Firma del cliente (si firmó en este paso); sin ella se cierra con la que ya tenía o sin firma. */
    firma?: { nombre: string; documento?: string | undefined; svg: string; png: Uint8Array; tomadoEn: string } | null;
    resultado: "exitosa" | "parcial";
    /** GPS del cierre si el permiso ya estaba dado. */
    posicion?: { lat: number; lon: number } | null;
}

/**
 * Encola la firma (adjunto PNG + `firma.capturar`) y el cierre (`orden.cerrar`), en ese orden: el
 * cierre sale cuando sube la firma. Lo que no llegue a guardarse lanza el error (la pantalla deja
 * volver a intentarlo).
 */
export async function encolarFirmaYCierre(motor: Pick<MotorSync, "agregarAdjunto" | "registrar">, entrada: EntradaFirmaYCierre): Promise<void> {
    const { orden, firma } = entrada;
    if (firma) {
        const { adjunto } = await motor.agregarAdjunto({
            ordenId: orden.id,
            tipo: "firma",
            datos: firma.png,
            mime: "image/png",
            obligatorio: orden.requiereFirma,
            tomadoEn: firma.tomadoEn,
        });
        await motor.registrar("firma.capturar", orden.id, {
            nombre: firma.nombre,
            svg: firma.svg,
            adjuntoId: adjunto.id,
            ...(firma.documento ? { documento: firma.documento } : {}),
        });
    }
    await motor.registrar("orden.cerrar", orden.id, { resultado: entrada.resultado, ...(entrada.posicion ? { lat: entrada.posicion.lat, lon: entrada.posicion.lon } : {}) });
}
