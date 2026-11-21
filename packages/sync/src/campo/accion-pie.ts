// La acción principal de las pantallas con firma (PWA y app nativa): «Firmar y cerrar orden» en el
// paso final y «Confirmar recepción» en un traslado. Va en una isla fija abajo, siempre a la vista y
// sobre el área segura, en lugar de la isla de menú o la de pasos (sobre la isla no flota nada):
// antes quedaba al final del contenido y había que bajar para verla (reporte del PM, nov 2026).
// La pantalla la registra mientras está montada y el marco la dibuja. Sin DOM ni React.
import { vecinosPaso, type Vista } from "./vista";

export interface AccionPie {
    /** Texto del botón primario («Firmar y cerrar orden»). */
    texto: string;
    /** Icono del primario (cada lado pone el suyo de Lucide). */
    icono: "cerrar" | "recibir";
    /** Guardando: el primario muestra que trabaja y no se puede volver a tocar. */
    ocupado: boolean;
    /** Botón redondo a la izquierda, como «Anterior» en la isla de pasos. */
    volver: { texto: string; destino: Vista };
    /** `data-slot` en la PWA y `testID` en la app nativa. */
    prueba: string;
}

export interface AccionPieRegistrada {
    accion: AccionPie;
    presionar: () => void;
}

/** La acción al pie de la pantalla abierta (una a la vez: la última que se registró). */
export class RegistroAccionPie {
    private actual: AccionPieRegistrada | null = null;
    private readonly oyentes = new Set<() => void>();

    /** La pone al pie; devuelve cómo quitarla (solo quita la suya si otra la reemplazó). */
    registrar(accion: AccionPie, presionar: () => void): () => void {
        const registrada: AccionPieRegistrada = { accion, presionar };
        this.actual = registrada;
        this.avisar();
        return () => {
            if (this.actual === registrada) {
                this.actual = null;
                this.avisar();
            }
        };
    }

    /** Lo que dibuja el marco; la misma referencia mientras no cambie. */
    obtener = (): AccionPieRegistrada | null => this.actual;

    suscribir = (oyente: () => void): (() => void) => {
        this.oyentes.add(oyente);
        return () => {
            this.oyentes.delete(oyente);
        };
    };

    private avisar(): void {
        for (const o of this.oyentes) {
            o();
        }
    }
}

/** «Anterior» del paso final: el paso de antes (las fotos), como en la isla de pasos. */
export function volverDelCierre(id: number): AccionPie["volver"] {
    const { anterior } = vecinosPaso("cierre");
    return anterior ? { texto: "Anterior", destino: { tipo: "orden", id, paso: anterior } } : { texto: "Resumen", destino: { tipo: "orden", id } };
}
