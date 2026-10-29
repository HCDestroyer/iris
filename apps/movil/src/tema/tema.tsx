import { createContext, useContext, useMemo, type ReactNode } from "react";
import { useColorScheme } from "react-native";
import { crearTema, type Tema } from "./tokens";

const ContextoTema = createContext<Tema>(crearTema(false));

/** Tema claro u oscuro según el teléfono (o forzado en pruebas con `esquema`). */
export function ProveedorTema({ children, esquema }: { children: ReactNode; esquema?: "light" | "dark" }) {
    const sistema = useColorScheme();
    const oscuro = (esquema ?? sistema) === "dark";
    const tema = useMemo(() => crearTema(oscuro), [oscuro]);
    return <ContextoTema.Provider value={tema}>{children}</ContextoTema.Provider>;
}

export function useTema(): Tema {
    return useContext(ContextoTema);
}
