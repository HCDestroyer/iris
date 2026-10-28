// Compresión de fotos en el teléfono antes de guardarlas en la cola: lado mayor ≤ 1600 px y
// ~300 KB en JPEG. Lo puro (medidas y elección de calidad) se prueba en Node; el dibujo usa
// `createImageBitmap` + canvas del navegador (respeta la orientación EXIF).

export const LADO_MAXIMO = 1600;
export const BYTES_OBJETIVO = 300 * 1024;
const CALIDADES = [0.82, 0.72, 0.62, 0.52, 0.42];

/** Medidas finales conservando la proporción, sin agrandar. */
export function dimensionesObjetivo(ancho: number, alto: number, maximo = LADO_MAXIMO): { ancho: number; alto: number } {
    const mayor = Math.max(ancho, alto);
    if (mayor <= maximo || mayor === 0) {
        return { ancho: Math.round(ancho), alto: Math.round(alto) };
    }
    const f = maximo / mayor;
    return { ancho: Math.max(1, Math.round(ancho * f)), alto: Math.max(1, Math.round(alto * f)) };
}

export type Codificador = (lado: number, calidad: number) => Promise<Blob>;

/**
 * Prueba calidades de mayor a menor (y, si aún no cabe, lado más chico) hasta quedar en
 * `objetivo` bytes; si nada cabe, devuelve el más chico logrado.
 */
export async function codificarConTope(codificar: Codificador, lado: number, objetivo = BYTES_OBJETIVO): Promise<Blob> {
    let mejor: Blob | null = null;
    for (const l of [lado, Math.round(lado * 0.8), Math.round(lado * 0.64)]) {
        for (const calidad of CALIDADES) {
            const blob = await codificar(l, calidad);
            if (!mejor || blob.size < mejor.size) {
                mejor = blob;
            }
            if (blob.size <= objetivo) {
                return blob;
            }
        }
    }
    if (!mejor) {
        throw new Error("No se pudo comprimir la foto");
    }
    return mejor;
}

export interface FotoComprimida {
    bytes: Uint8Array;
    mime: "image/jpeg";
    ancho: number;
    alto: number;
}

/** Comprime una foto de la cámara (solo navegador). */
export async function comprimirFoto(archivo: Blob): Promise<FotoComprimida> {
    const imagen = await createImageBitmap(archivo, { imageOrientation: "from-image" });
    try {
        const lado = Math.min(LADO_MAXIMO, Math.max(imagen.width, imagen.height));
        let medidas = { ancho: imagen.width, alto: imagen.height };
        const blob = await codificarConTope(async (l, calidad) => {
            medidas = dimensionesObjetivo(imagen.width, imagen.height, l);
            const canvas = document.createElement("canvas");
            canvas.width = medidas.ancho;
            canvas.height = medidas.alto;
            const ctx = canvas.getContext("2d");
            if (!ctx) {
                throw new Error("El navegador no permite procesar la foto");
            }
            ctx.drawImage(imagen, 0, 0, medidas.ancho, medidas.alto);
            return new Promise<Blob>((resolver, rechazar) =>
                canvas.toBlob((b) => (b ? resolver(b) : rechazar(new Error("No se pudo comprimir la foto"))), "image/jpeg", calidad),
            );
        }, lado);
        return { bytes: new Uint8Array(await blob.arrayBuffer()), mime: "image/jpeg", ...medidas };
    } finally {
        imagen.close();
    }
}
