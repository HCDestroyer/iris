// Escáner por campo en el navegador: qué códigos del cuadro cuentan según la geometría del visor
// (`VisorEscaner` de `@iris/ui`) y la vibración corta. La lógica (clases, candidatos, ventana) vive
// en `@iris/sync/campo` y la comparte la app nativa.
import { dentroDelRecuadro, recuadroEnFuente, type CodigoVisto } from "@iris/sync/campo";
import type { CodigoLeido, GeometriaVisor } from "@iris/ui";

/**
 * Los códigos cuyo centro cae dentro del recuadro. `BarcodeDetector` da `boundingBox` en píxeles
 * de la cámara, así que el recuadro (en la vista, `object-fit: cover`) se pasa a esas coordenadas.
 * ZXing no da posición: cuentan todos.
 */
export function codigosQueCuentan(codigos: readonly CodigoLeido[], g: GeometriaVisor): CodigoVisto[] {
    return dentroDelRecuadro(codigos, recuadroEnFuente(g.recuadro, g.fuente, g.vista));
}

/** Vibración corta al detectar, si el navegador la tiene (Android; Safari no). */
export function vibrarCorto(nav: { vibrate?: (patron: number) => boolean } | undefined = globalThis.navigator): void {
    try {
        nav?.vibrate?.(40);
    } catch {
        // Sin vibración (permiso del navegador o política del documento): no pasa nada.
    }
}
