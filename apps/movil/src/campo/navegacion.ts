// Navegación de la app nativa: las mismas vistas que la PWA (`@iris/sync/campo`) con una pila
// propia para el «Atrás» de Android (en la web lo hace el historial del navegador). Sin React.
import { mismaVista, PESTANAS, type Vista } from "@iris/sync/campo";

export interface PilaVistas {
    actual: Vista;
    /** Vistas anteriores, la última es a la que vuelve «Atrás». */
    atras: readonly Vista[];
}

/** Tope de la pila: con más, se olvidan las más viejas. */
export const MAXIMO_PILA = 30;

const INICIO: Vista = { tipo: "lista" };

const esRaizDePestana = (v: Vista) => PESTANAS.some((p) => mismaVista(p.vista, v));

export function pilaInicial(v: Vista = INICIO): PilaVistas {
    return { actual: v, atras: mismaVista(v, INICIO) ? [] : [INICIO] };
}

/**
 * Ir a una vista. Tocar una pestaña empieza de nuevo desde ella (con «Hoy» debajo, como abrir la
 * app); lo demás (una orden, un paso, un traslado, «Mi bodega») se apila para volver con «Atrás».
 */
export function irA(pila: PilaVistas, v: Vista): PilaVistas {
    if (mismaVista(pila.actual, v)) {
        return pila;
    }
    if (esRaizDePestana(v)) {
        return pilaInicial(v);
    }
    return { actual: v, atras: [...pila.atras, pila.actual].slice(-MAXIMO_PILA) };
}

/** «Atrás»: la vista anterior, o `null` si ya está en «Hoy» sin nada debajo (Android sale de la app). */
export function retroceder(pila: PilaVistas): PilaVistas | null {
    const anterior = pila.atras.at(-1);
    if (!anterior) {
        return null;
    }
    return { actual: anterior, atras: pila.atras.slice(0, -1) };
}
