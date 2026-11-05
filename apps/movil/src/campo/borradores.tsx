// Los borradores de los pasos de la orden (como la PWA): los campos de texto, número y medición
// registran su borrador aquí; «Guardar y seguir» de la isla los guarda todos (`RegistroBorradores` de
// `@iris/sync/campo`), y cada uno también se guarda solo al salir del campo.
import { RegistroBorradores, type Borrador } from "@iris/sync/campo";
import { createContext, useCallback, useContext, useEffect, useRef, useState, useSyncExternalStore, type ReactNode } from "react";
import { useCampo } from "./contexto";

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

/**
 * Registra el borrador de un campo mientras está montado; `pendiente`: algo escrito sin guardar.
 * Devuelve lo que el campo llama al salir de él: lo guarda en silencio y la isla dice «Guardado»
 * (un error queda en el campo, sin aviso arriba).
 */
export function useBorrador(clave: string, borrador: Borrador, pendiente: boolean): () => Promise<void> {
    const registro = useRegistro();
    const { servicios } = useCampo();
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
    return useCallback(async () => {
        registro.confirmar(await registro.guardarUno(clave), { enLinea: servicios.enLinea() });
    }, [registro, clave, servicios]);
}

/** Cuántos campos del paso tienen algo escrito sin guardar. */
export function useSinGuardar(): number {
    const r = useRegistro();
    return useSyncExternalStore(r.suscribir, r.pendientes, r.pendientes);
}

/** Mientras se ve «Guardado» en la isla, lo que oye el lector de pantalla; si no, `null`. */
export function useRecienGuardado(): string | null {
    const r = useRegistro();
    return useSyncExternalStore(r.suscribir, r.guardado, r.guardado);
}

/**
 * Guardar los borradores del paso (lo hace «Guardar y seguir» y también «Anterior»): un error sale
 * arriba con el campo (y devuelve `false`: el técnico se queda); lo guardado se confirma en la isla
 * con «Guardado» (`RegistroBorradores.confirmar`) y, con señal, se manda ya. Sin borradores no hace
 * nada y devuelve `true`.
 */
export function useGuardarPaso(): { guardar: () => Promise<boolean>; guardando: boolean } {
    const registro = useRegistro();
    const { motor, notificar, servicios } = useCampo();
    const [guardando, setGuardando] = useState(false);
    const guardar = useCallback(async () => {
        setGuardando(true);
        try {
            const r = await registro.guardarTodo();
            const enLinea = servicios.enLinea();
            const aviso = registro.confirmar(r, { enLinea });
            if (aviso?.en === "aviso") {
                notificar({ tono: aviso.tono, texto: aviso.texto });
            }
            if (r.errores.length === 0 && r.guardados > 0 && enLinea) {
                motor.sincronizar().catch(() => undefined);
            }
            return r.errores.length === 0;
        } finally {
            setGuardando(false);
        }
    }, [registro, motor, notificar, servicios]);
    return { guardar, guardando };
}
