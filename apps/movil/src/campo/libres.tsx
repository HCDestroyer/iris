// Órdenes libres (botón «+»): se leen con señal (`GET /campo/ordenes-libres`) y quedan en memoria
// como «Por recibir»; nunca se guardan en el teléfono ni van por la cola. Como la PWA
// (`app/campo/_componentes/libres-datos.tsx`).
import type { LibresCampo, OrdenesLibres } from "@iris/sync";
import {
    createContext,
    useCallback,
    useContext,
    useMemo,
    useState,
    type ReactNode,
} from "react";
import { useLectura, type Lectura } from "./inventario";

interface ContextoLibres {
    api: LibresCampo;
    libres: Lectura<OrdenesLibres>;
    cargarLibres: () => Promise<void>;
    /** Ids quitados de la lista leída (tomadas por el técnico o por otro). */
    quitadas: ReadonlySet<number>;
    quitar: (id: number) => void;
}

const Contexto = createContext<ContextoLibres | null>(null);

export function useLibres(): ContextoLibres {
    const c = useContext(Contexto);
    if (!c) {
        throw new Error("useLibres fuera de ProveedorLibres");
    }
    return c;
}

export function ProveedorLibres({
    api,
    children,
}: {
    api: LibresCampo;
    children: ReactNode;
}) {
    const leer = useCallback(() => api.listar(), [api]);
    const [libres, cargar] = useLectura(leer);
    const [quitadas, setQuitadas] = useState<ReadonlySet<number>>(new Set());
    const cargarLibres = useCallback(async () => {
        await cargar();
        setQuitadas(new Set());
    }, [cargar]);
    const quitar = useCallback(
        (id: number) => setQuitadas((q) => new Set([...q, id])),
        [],
    );
    const valor = useMemo(
        () => ({ api, libres, cargarLibres, quitadas, quitar }),
        [api, libres, cargarLibres, quitadas, quitar],
    );
    return <Contexto.Provider value={valor}>{children}</Contexto.Provider>;
}
