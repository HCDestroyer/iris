"use client";

import { avisoGuardado, MS_GUARDADO, RegistroBorradores, type Borrador } from "@iris/sync/campo";
import { createContext, useCallback, useContext, useEffect, useRef, useState, useSyncExternalStore, type ReactNode } from "react";
import { useCampo } from "./contexto";

/*
 * Los borradores de los pasos de la orden (como la app nativa): los campos de texto, número y
 * medición registran su borrador aquí; «Guardar y seguir» de la isla los guarda todos
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
                guardar: (opciones) => actual.current.guardar(opciones),
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
 * Guardar los borradores del paso (lo hace «Guardar y seguir» y también «Anterior»): un error sale
 * arriba con el campo (y devuelve `false`: el técnico se queda); lo guardado se confirma en la isla
 * con «Guardado» por `MS_GUARDADO` (`guardado` lleva lo que oye el lector de pantalla) y, con señal,
 * se manda ya. Sin borradores no hace nada y devuelve `true`.
 */
export function useGuardarPaso(): { guardar: () => Promise<boolean>; guardando: boolean; guardado: string | null } {
    const registro = useRegistro();
    const { motor, notificar } = useCampo();
    const [guardando, setGuardando] = useState(false);
    const [guardado, setGuardado] = useState<string | null>(null);
    const reloj = useRef<ReturnType<typeof setTimeout> | null>(null);
    useEffect(
        () => () => {
            if (reloj.current) {
                clearTimeout(reloj.current);
            }
        },
        [],
    );
    const guardar = useCallback(async () => {
        setGuardando(true);
        try {
            const r = await registro.guardarTodo();
            const enLinea = navigator.onLine;
            const aviso = avisoGuardado(r, { enLinea });
            if (aviso?.en === "aviso") {
                notificar({ tono: aviso.tono, texto: aviso.texto });
            } else if (aviso?.en === "isla") {
                setGuardado(aviso.detalle);
                if (reloj.current) {
                    clearTimeout(reloj.current);
                }
                reloj.current = setTimeout(() => setGuardado(null), MS_GUARDADO);
            }
            if (r.errores.length === 0 && r.guardados > 0 && enLinea) {
                motor.sincronizar().catch(() => undefined);
            }
            return r.errores.length === 0;
        } finally {
            setGuardando(false);
        }
    }, [registro, motor, notificar]);
    return { guardar, guardando, guardado };
}
