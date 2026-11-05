"use client";

import { avisoGuardado, RegistroBorradores, totalPorEnviar, type Borrador } from "@iris/sync/campo";
import { createContext, useCallback, useContext, useEffect, useRef, useState, useSyncExternalStore, type ReactNode } from "react";
import { useCampo } from "./contexto";

/*
 * Un solo «Guardar» en los pasos de la orden (como la app nativa): los campos de texto, número y
 * medición registran su borrador aquí; el «Guardar» de la isla los guarda todos
 * (`RegistroBorradores` de `@iris/sync/campo`) y cada uno también se guarda solo al salir del campo.
 */

const Contexto = createContext<RegistroBorradores | null>(null);

export function ProveedorBorradores({ children }: { children: ReactNode }) {
    const [registro] = useState(() => new RegistroBorradores());
    return <Contexto.Provider value={registro}>{children}</Contexto.Provider>;
}

function useRegistro(): RegistroBorradores {
    const r = useContext(Contexto);
    if (!r) {
        throw new Error("useBorrador fuera de ProveedorBorradores");
    }
    return r;
}

/** Registra el borrador de un campo mientras está montado; `pendiente`: algo escrito sin guardar. */
export function useBorrador(clave: string, borrador: Borrador, pendiente: boolean): void {
    const registro = useRegistro();
    const actual = useRef(borrador);
    useEffect(() => {
        actual.current = borrador;
    });
    useEffect(
        () =>
            registro.registrar(clave, {
                get etiqueta() {
                    return actual.current.etiqueta;
                },
                pendiente: () => actual.current.pendiente(),
                guardar: () => actual.current.guardar(),
            }),
        [registro, clave],
    );
    useEffect(() => {
        registro.avisar();
    }, [registro, pendiente]);
}

/** Cuántos campos del paso tienen algo escrito sin guardar. */
export function useSinGuardar(): number {
    const r = useRegistro();
    return useSyncExternalStore(r.suscribir, r.pendientes, r.pendientes);
}

/**
 * «Guardar» de la isla: guarda los borradores, avisa qué pasó (`avisoGuardado`) y, con señal, manda
 * lo pendiente ya. Al pasar de paso (`silencioso`) solo avisa si guardó algo o falló. Devuelve si no
 * hubo errores.
 */
export function useGuardarPaso(): { guardar: (opciones?: { silencioso?: boolean }) => Promise<boolean>; guardando: boolean } {
    const registro = useRegistro();
    const { motor, notificar } = useCampo();
    const [guardando, setGuardando] = useState(false);
    const guardar = useCallback(
        async ({ silencioso = false }: { silencioso?: boolean } = {}) => {
            setGuardando(true);
            try {
                const r = await registro.guardarTodo();
                const enLinea = navigator.onLine;
                if (!silencioso || r.guardados > 0 || r.errores.length > 0) {
                    notificar(avisoGuardado(r, { porEnviar: totalPorEnviar(motor.obtenerEstado()), enLinea }));
                }
                if (r.errores.length === 0 && enLinea && (!silencioso || r.guardados > 0)) {
                    motor.sincronizar().catch(() => undefined);
                }
                return r.errores.length === 0;
            } finally {
                setGuardando(false);
            }
        },
        [registro, motor, notificar],
    );
    return { guardar, guardando };
}
