// Tema de la app: HeroUI Native con el tema Horus de `src/global.css` (Uniwind) y los mismos tokens
// en JS (`useTema()`) para lo que no se pinta con clases: íconos de Lucide, el lienzo y la cámara.
// Claro u oscuro según el teléfono; las pruebas lo fuerzan con `esquema`.
import { HeroUINativeProvider, type HeroUINativeConfig } from "heroui-native/provider";
import { createContext, useContext, useEffect, useMemo, type ReactNode } from "react";
import { useColorScheme } from "react-native";
import { Uniwind } from "uniwind";
import { crearTema, type Tema } from "./tokens";

const ContextoTema = createContext<Tema>(crearTema(false));

const CONFIG_HEROUI: HeroUINativeConfig = {
    // Sin el aviso de estilos de HeroUI en la consola (es para quien empieza con la librería).
    devInfo: { stylingPrinciples: false },
    // Avisos emergentes: el `Toast` de HeroUI Native, que entra y sale desde arriba (con su
    // animación y deslizar para cerrarlo). El marco decide cuál se ve (`AvisoEmergente`).
    toast: { defaultProps: { placement: "top", isSwipeable: true }, insets: { top: 8, left: 14, right: 14 }, maxVisibleToasts: 1 },
    // Textos que crecen con el tamaño de letra del teléfono, con tope para que la barra no se rompa.
    textProps: { maxFontSizeMultiplier: 1.6 },
};

/** Tema claro u oscuro según el teléfono (o forzado en pruebas con `esquema`). */
export function ProveedorTema({ children, esquema }: { children: ReactNode; esquema?: "light" | "dark" }) {
    const sistema = useColorScheme();
    const oscuro = (esquema ?? sistema) === "dark";
    const tema = useMemo(() => crearTema(oscuro), [oscuro]);
    useEffect(() => {
        // Uniwind sigue al teléfono por sí solo («system»); solo se fija si se pidió un esquema.
        if (esquema) {
            Uniwind.setTheme(esquema);
        }
    }, [esquema]);
    return (
        <ContextoTema.Provider value={tema}>
            <HeroUINativeProvider config={CONFIG_HEROUI}>{children}</HeroUINativeProvider>
        </ContextoTema.Provider>
    );
}

export function useTema(): Tema {
    return useContext(ContextoTema);
}
