// La acción principal de las pantallas con firma («Firmar y cerrar orden», «Confirmar recepción»;
// como la PWA): la pantalla la registra aquí y el marco la dibuja en una isla fija abajo
// (`IslaAccion`), siempre a la vista y sobre el área segura, en lugar de la isla de menú o la de
// pasos (`RegistroAccionPie` de `@iris/sync/campo`). Antes iba al final del contenido y había que
// bajar para verla (reporte del PM, nov 2026).
//
// También lleva el bloqueo del desplazamiento mientras el dedo firma: el lienzo lo pide al empezar
// el trazo y lo suelta al terminar, y el marco apaga el `scrollEnabled` de su `ScrollView`.
import type { AccionPie, AccionPieRegistrada, RegistroAccionPie } from "@iris/sync/campo";
import { createContext, useContext, useEffect, useLayoutEffect, useRef, useSyncExternalStore, type ReactNode } from "react";

const Contexto = createContext<RegistroAccionPie | null>(null);
const ContextoBloqueo = createContext<((bloqueado: boolean) => void) | null>(null);

/** Lo pone el marco con su propio registro (lo lee él mismo para elegir la isla). */
export function ProveedorAccionPie({ registro, bloquearDesplazamiento, children }: { registro: RegistroAccionPie; bloquearDesplazamiento: (bloqueado: boolean) => void; children: ReactNode }) {
    return (
        <Contexto.Provider value={registro}>
            <ContextoBloqueo.Provider value={bloquearDesplazamiento}>{children}</ContextoBloqueo.Provider>
        </Contexto.Provider>
    );
}

function useRegistro(): RegistroAccionPie {
    const r = useContext(Contexto);
    if (!r) {
        throw new Error("useAccionPie fuera de ProveedorAccionPie");
    }
    return r;
}

/**
 * Pone la acción al pie mientras la pantalla está montada. `presionar` puede cambiar en cada dibujo
 * (se llama la última); la isla solo se vuelve a dibujar cuando cambia lo que muestra.
 */
export function useAccionPie(accion: AccionPie, presionar: () => void): void {
    const registro = useRegistro();
    const ultimo = useRef(presionar);
    useEffect(() => {
        ultimo.current = presionar;
    });
    const { texto, icono, ocupado, prueba } = accion;
    const volver = JSON.stringify(accion.volver);
    // Antes de pintar: la isla de menú no alcanza a verse un instante al abrir la pantalla.
    useLayoutEffect(
        () => registro.registrar({ texto, icono, ocupado, prueba, volver: JSON.parse(volver) as AccionPie["volver"] }, () => ultimo.current()),
        [registro, texto, icono, ocupado, prueba, volver],
    );
}

/** La acción al pie registrada en `registro`, o `null`. */
export function useAccionPieDe(registro: RegistroAccionPie): AccionPieRegistrada | null {
    return useSyncExternalStore(registro.suscribir, registro.obtener, registro.obtener);
}

/** Apaga el desplazamiento del marco mientras el dedo firma (sin marco, no hace nada). */
export function useBloquearDesplazamiento(): (bloqueado: boolean) => void {
    return useContext(ContextoBloqueo) ?? sinMarco;
}

const sinMarco = () => undefined;
