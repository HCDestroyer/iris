"use client";

import { useState, useSyncExternalStore } from "react";

/*
 * Un clic no se pierde por un error que aparece al salir de un campo.
 *
 * Con validación al salir del campo (`mode: "onTouched"` o mensajes «vivos»), el `blur` llega con
 * el `pointerdown` sobre el botón siguiente: el error aparece, el contenido baja y el `pointerup`
 * cae en otro elemento, así que el `click` nunca ocurre. Mientras haya un puntero presionado, los
 * mensajes de los campos no cambian de alto; se actualizan justo después del `click`.
 */

let presionado = false;
const oyentes = new Set<() => void>();
let instalado = false;

function avisar() {
    for (const oyente of oyentes) oyente();
}

function instalar() {
    if (instalado || typeof document === "undefined") return;
    instalado = true;
    document.addEventListener(
        "pointerdown",
        () => {
            presionado = true;
            avisar();
        },
        true,
    );
    const soltar = () => {
        // Después del `click` (que sale en la misma tarea que el `pointerup`).
        window.setTimeout(() => {
            presionado = false;
            avisar();
        }, 0);
    };
    document.addEventListener("pointerup", soltar, true);
    document.addEventListener("pointercancel", soltar, true);
}

function suscribir(oyente: () => void) {
    instalar();
    oyentes.add(oyente);
    return () => {
        oyentes.delete(oyente);
    };
}

/** `true` mientras un puntero está presionado en la página. */
export function usePunteroPresionado(): boolean {
    return useSyncExternalStore(
        suscribir,
        () => presionado,
        () => false,
    );
}

/**
 * El error que se muestra: el de ahora, salvo mientras hay un puntero presionado, en que se
 * queda el último mostrado (aparecer o desaparecer movería lo que está debajo del puntero).
 */
export function useErrorEstable(error: string | undefined): string | undefined {
    const conPuntero = usePunteroPresionado();
    const [mostrado, setMostrado] = useState(error);
    if (!conPuntero && mostrado !== error) {
        setMostrado(error);
    }
    return conPuntero ? mostrado : error;
}
