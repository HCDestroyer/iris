// Firma a mano alzada: trazos en coordenadas CSS del lienzo → SVG (texto, para la API) y PNG
// (adjunto). Lo puro (SVG) se prueba en Node; el PNG necesita un canvas del navegador.

export interface Punto {
    x: number;
    y: number;
}

export type Trazo = Punto[];

/** Tope de la API para el SVG de la firma (`firma.capturar.svg`). */
export const MAX_SVG_FIRMA = 200_000;

const r1 = (n: number) => Math.round(n * 10) / 10;

/** Quita puntos a menos de `tolerancia` px del anterior (el dedo repite posiciones). */
export function simplificar(trazo: Trazo, tolerancia: number): Trazo {
    const salida: Trazo = [];
    for (const p of trazo) {
        const ultimo = salida.at(-1);
        if (!ultimo || Math.hypot(p.x - ultimo.x, p.y - ultimo.y) >= tolerancia) {
            salida.push(p);
        }
    }
    const fin = trazo.at(-1);
    if (fin && salida.at(-1) !== fin && salida.length > 0) {
        salida.push(fin);
    }
    return salida;
}

function rutaDe(trazo: Trazo): string {
    const [primero, ...resto] = trazo;
    if (!primero) {
        return "";
    }
    // Un toque sin arrastre se dibuja como un punto (línea mínima con extremo redondo).
    const siguientes = resto.length > 0 ? resto : [{ x: primero.x + 0.1, y: primero.y }];
    return `M${r1(primero.x)} ${r1(primero.y)}` + siguientes.map((p) => `L${r1(p.x)} ${r1(p.y)}`).join("");
}

/** ¿Hay algo firmado? (al menos un trazo con un punto). */
export function hayFirma(trazos: readonly Trazo[]): boolean {
    return trazos.some((t) => t.length > 0);
}

/**
 * SVG de la firma: trazos negros sobre transparente, `viewBox` del tamaño del lienzo. Si pasa
 * del tope de la API, simplifica con más tolerancia hasta que quepa.
 */
export function trazosASvg(trazos: readonly Trazo[], ancho: number, alto: number, grosor = 2.5): string {
    for (let tolerancia = 0.5; ; tolerancia *= 2) {
        const rutas = trazos
            .filter((t) => t.length > 0)
            .map((t) => rutaDe(simplificar(t, tolerancia)))
            .join("");
        const svg =
            `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 ${r1(ancho)} ${r1(alto)}" width="${r1(ancho)}" height="${r1(alto)}">` +
            `<path d="${rutas}" fill="none" stroke="#111111" stroke-width="${grosor}" stroke-linecap="round" stroke-linejoin="round"/></svg>`;
        if (svg.length <= MAX_SVG_FIRMA || tolerancia > 64) {
            return svg;
        }
    }
}

/** Dibuja los trazos en un contexto 2D (lienzo en pantalla y PNG). */
export function dibujarTrazos(ctx: CanvasRenderingContext2D | OffscreenCanvasRenderingContext2D, trazos: readonly Trazo[], color: string, grosor = 2.5): void {
    ctx.strokeStyle = color;
    ctx.fillStyle = color;
    ctx.lineWidth = grosor;
    ctx.lineCap = "round";
    ctx.lineJoin = "round";
    for (const trazo of trazos) {
        const [primero, ...resto] = trazo;
        if (!primero) {
            continue;
        }
        ctx.beginPath();
        ctx.moveTo(primero.x, primero.y);
        if (resto.length === 0) {
            ctx.arc(primero.x, primero.y, grosor / 2, 0, Math.PI * 2);
            ctx.fill();
            continue;
        }
        for (const p of resto) {
            ctx.lineTo(p.x, p.y);
        }
        ctx.stroke();
    }
}

/** PNG de la firma (negro sobre transparente) a `escala` × el tamaño del lienzo. Solo navegador. */
export async function trazosAPng(trazos: readonly Trazo[], ancho: number, alto: number, escala = 2): Promise<Blob> {
    const canvas = document.createElement("canvas");
    canvas.width = Math.max(1, Math.round(ancho * escala));
    canvas.height = Math.max(1, Math.round(alto * escala));
    const ctx = canvas.getContext("2d");
    if (!ctx) {
        throw new Error("El navegador no permite dibujar la firma");
    }
    ctx.scale(escala, escala);
    dibujarTrazos(ctx, trazos, "#111111");
    return new Promise((resolver, rechazar) => {
        canvas.toBlob((blob) => (blob ? resolver(blob) : rechazar(new Error("No se pudo generar el PNG de la firma"))), "image/png");
    });
}
