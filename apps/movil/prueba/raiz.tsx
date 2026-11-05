// Raíz de las pruebas como la de `App.tsx`: Gesture Handler por fuera (lo piden el `Toast` de
// HeroUI Native y el lienzo de firma) y el tema, claro u oscuro.
import type { ReactNode } from "react";
import { StyleSheet } from "react-native";
import { GestureHandlerRootView } from "react-native-gesture-handler";
import { ProveedorTema } from "../src/tema/tema";

export function TemaDePrueba({ esquema, children }: { esquema?: "light" | "dark"; children: ReactNode }) {
    return (
        <GestureHandlerRootView style={estilos.raiz}>
            <ProveedorTema {...(esquema ? { esquema } : {})}>{children}</ProveedorTema>
        </GestureHandlerRootView>
    );
}

const estilos = StyleSheet.create({ raiz: { flex: 1 } });
