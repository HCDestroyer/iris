// Lo que entrega el lienzo de firma (cliente en el cierre, técnico en la recepción): los trazos,
// su tamaño y el PNG. El lienzo (gestos y dibujo) es de la capa visual; esto es lo que usan los
// casos de la pantalla.
import type { Trazo } from "@iris/ui/firma";

export interface LienzoListo {
    trazos: Trazo[];
    ancho: number;
    alto: number;
    png: () => Promise<Uint8Array>;
}
