// Tema de la app: HeroUI Native con el tema Horus de `src/global.css` (Uniwind) y los mismos tokens
// en JS (`useTema()`) para lo que no se pinta con clases: íconos de Lucide, el lienzo y la cámara.
// Claro u oscuro según la apariencia elegida en «Yo» (Teléfono · Claro · Oscuro, guardada en el
// dispositivo); con «Teléfono», según el sistema. Las pruebas simulan el sistema con `esquema`.
import { CLAVE_APARIENCIA, esApariencia, esOscuro, type Apariencia } from "@iris/sync/campo";
import { HeroUINativeProvider, type HeroUINativeConfig } from "heroui-native/provider";
import { createContext, useCallback, useContext, useEffect, useMemo, useState, type ReactNode } from "react";
import { useColorScheme } from "react-native";
import { useSafeAreaInsets, type EdgeInsets } from "react-native-safe-area-context";
import { Uniwind } from "uniwind";
import { entorno } from "../entorno";
import { crearTema, type Tema } from "./tokens";

const ContextoTema = createContext<Tema>(crearTema(false));

type ContextoApariencia = { apariencia: Apariencia; elegirApariencia: (a: Apariencia) => void };
const ContextoApariencia = createContext<ContextoApariencia>({ apariencia: "telefono", elegirApariencia: () => undefined });

/** La apariencia guardada en el teléfono («Teléfono» si no hay o no se puede leer). */
function aparienciaGuardada(): Apariencia {
    try {
        const v = entorno().claves.leer(CLAVE_APARIENCIA);
        return esApariencia(v) ? v : "telefono";
    } catch {
        return "telefono";
    }
}

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

/**
 * Tema claro u oscuro según la apariencia de «Yo» y, con «Teléfono», según el sistema (o el
 * `esquema` que simulan las pruebas). Cambiarla aplica al instante: los tokens de `useTema()` y las
 * clases de Uniwind (`Uniwind.setTheme`: «light», «dark» o «system» para seguir al teléfono).
 */
export function ProveedorTema({ children, esquema }: { children: ReactNode; esquema?: "light" | "dark" }) {
    const sistema = useColorScheme();
    const [apariencia, setApariencia] = useState<Apariencia>(aparienciaGuardada);
    const oscuro = esOscuro(apariencia, (esquema ?? sistema) === "dark");
    const tema = useMemo(() => crearTema(oscuro), [oscuro]);
    const margen = useSafeAreaInsets();
    const config = useMemo(() => configHeroui(margen), [margen]);
    useEffect(() => {
        Uniwind.setTheme(apariencia === "claro" ? "light" : apariencia === "oscuro" ? "dark" : (esquema ?? "system"));
    }, [apariencia, esquema]);
    const elegirApariencia = useCallback((a: Apariencia) => {
        try {
            entorno().claves.escribir(CLAVE_APARIENCIA, a);
        } catch {
            // Sin almacén vale para esta vez; al abrir de nuevo vuelve a «Teléfono».
        }
        setApariencia(a);
    }, []);
    const valorApariencia = useMemo(() => ({ apariencia, elegirApariencia }), [apariencia, elegirApariencia]);
    return (
        <ContextoApariencia.Provider value={valorApariencia}>
            <ContextoTema.Provider value={tema}>
                <HeroUINativeProvider config={config}>{children}</HeroUINativeProvider>
            </ContextoTema.Provider>
        </ContextoApariencia.Provider>
    );
}

/** Apariencia elegida en «Yo» y cómo cambiarla (se guarda en el teléfono). */
export function useApariencia(): ContextoApariencia {
    return useContext(ContextoApariencia);
}

export function useTema(): Tema {
    return useContext(ContextoTema);
}
