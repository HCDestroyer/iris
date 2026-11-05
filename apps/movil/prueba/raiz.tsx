// Raíz de las pruebas como la de `App.tsx`: Gesture Handler por fuera (lo piden el `Toast` de
// HeroUI Native y el lienzo de firma), el margen seguro si se pide y el tema, claro u oscuro.
import type { ReactNode } from "react";
import { StyleSheet } from "react-native";
import { GestureHandlerRootView } from "react-native-gesture-handler";
import { SafeAreaProvider } from "react-native-safe-area-context";
import { ProveedorTema } from "../src/tema/tema";

export function TemaDePrueba({ esquema, margen, children }: { esquema?: "light" | "dark"; margen?: { top: number; bottom: number; left: number; right: number }; children: ReactNode }) {
    const tema = <ProveedorTema {...(esquema ? { esquema } : {})}>{children}</ProveedorTema>;
    return (
        <GestureHandlerRootView style={estilos.raiz}>
            {/* Con margen, como un teléfono con barra de estado o muesca (`SafeAreaProvider` de prueba). */}
            {margen ? <SafeAreaProvider initialMetrics={{ frame: { x: 0, y: 0, width: 390, height: 844 }, insets: margen }}>{tema}</SafeAreaProvider> : tema}
        </GestureHandlerRootView>
    );
}

const estilos = StyleSheet.create({ raiz: { flex: 1 } });
