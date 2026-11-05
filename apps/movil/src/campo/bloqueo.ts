// Con «Desbloquear con huella» encendido, la app vuelve a pedir la huella si regresa al frente
// (`AppState` → `active`) después de más de 5 minutos en segundo plano. Sin la huella encendida no
// hace nada (quien llama decide con `pedirHuella`).
import { useEffect, useRef } from "react";
import { AppState, type AppStateStatus } from "react-native";

/** Tiempo fuera de la app tras el que se vuelve a pedir la huella. */
export const AUSENCIA_MAX_MS = 5 * 60_000;

/** ¿Estuvo fuera (en segundo plano) más de `AUSENCIA_MAX_MS`? */
export function ausenciaLarga(salioEn: number | null, ahora: number): boolean {
    return salioEn !== null && ahora - salioEn > AUSENCIA_MAX_MS;
}

/**
 * Llama `alVolverTarde` cuando la app vuelve al frente tras una ausencia larga. `activo` en falso
 * (sin sesión abierta) no escucha nada.
 */
export function useVolverTrasAusencia(activo: boolean, alVolverTarde: () => void, ahora: () => number = Date.now): void {
    const llamar = useRef(alVolverTarde);
    useEffect(() => {
        llamar.current = alVolverTarde;
    });
    useEffect(() => {
        if (!activo) {
            return undefined;
        }
        let salioEn: number | null = null;
        const s = AppState.addEventListener("change", (e: AppStateStatus) => {
            if (e === "background") {
                salioEn ??= ahora();
            } else if (e === "active") {
                const tarde = ausenciaLarga(salioEn, ahora());
                salioEn = null;
                if (tarde) {
                    llamar.current();
                }
            }
        });
        return () => s.remove();
    }, [activo, ahora]);
}
