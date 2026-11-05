// Tema de la app: HeroUI Native con el tema Horus de `src/global.css` (Uniwind) y los mismos tokens
// en JS (`useTema()`) para lo que no se pinta con clases: íconos de Lucide, el lienzo y la cámara.
// Claro u oscuro según el teléfono; las pruebas lo fuerzan con `esquema`.
import { HeroUINativeProvider, type HeroUINativeConfig } from "heroui-native/provider";
import { createContext, useContext, useEffect, useMemo, type ReactNode } from "react";
import { useColorScheme } from "react-native";
import { useSafeAreaInsets, type EdgeInsets } from "react-native-safe-area-context";
import { Uniwind } from "uniwind";
import { crearTema, type Tema } from "./tokens";

const ContextoTema = createContext<Tema>(crearTema(false));

/** Separación del aviso emergente respecto del borde seguro (bajo la barra de estado y la franja). */
export const AIRE_AVISO = { arriba: 8, lados: 14 } as const;

/**
 * Dónde se dibuja el `Toast` de HeroUI Native. Un `insets` propio REEMPLAZA el margen seguro de
 * HeroUI (no se suma), así que va el margen del teléfono más el aire: con edge-to-edge, un `top: 8`
 * a secas dejaba el aviso bajo la hora y la cámara (y detrás de la franja sólida del encabezado).
 */
export function insetsAvisos(margen: Pick<EdgeInsets, "top" | "left" | "right">): { top: number; left: number; right: number } {
    return { top: margen.top + AIRE_AVISO.arriba, left: margen.left + AIRE_AVISO.lados, right: margen.right + AIRE_AVISO.lados };
}

function configHeroui(margen: EdgeInsets): HeroUINativeConfig {
    return {
        // Sin el aviso de estilos de HeroUI en la consola (es para quien empieza con la librería).
        devInfo: { stylingPrinciples: false },
        // Avisos emergentes: el `Toast` de HeroUI Native, que entra y sale desde arriba (con su
        // animación y deslizar para cerrarlo), debajo del margen seguro. El marco decide cuál se ve
        // (`AvisoEmergente`).
        toast: { defaultProps: { placement: "top", isSwipeable: true }, insets: insetsAvisos(margen), maxVisibleToasts: 1 },
        // Textos que crecen con el tamaño de letra del teléfono, con tope para que la barra no se rompa.
        textProps: { maxFontSizeMultiplier: 1.6 },
    };
}

/** Tema claro u oscuro según el teléfono (o forzado en pruebas con `esquema`). */
export function ProveedorTema({ children, esquema }: { children: ReactNode; esquema?: "light" | "dark" }) {
    const sistema = useColorScheme();
    const oscuro = (esquema ?? sistema) === "dark";
    const tema = useMemo(() => crearTema(oscuro), [oscuro]);
    const margen = useSafeAreaInsets();
    const config = useMemo(() => configHeroui(margen), [margen]);
    useEffect(() => {
        // Uniwind sigue al teléfono por sí solo («system»); solo se fija si se pidió un esquema.
        if (esquema) {
            Uniwind.setTheme(esquema);
        }
    }, [esquema]);
    return (
        <ContextoTema.Provider value={tema}>
            <HeroUINativeProvider config={config}>{children}</HeroUINativeProvider>
        </ContextoTema.Provider>
    );
}

export function useTema(): Tema {
    return useContext(ContextoTema);
}
