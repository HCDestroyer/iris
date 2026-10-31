// Lienzo de firma con el dedo: los trazos (para el SVG de la API, el mismo de la PWA con
// `trazosASvg` de `@iris/ui/firma`) y el PNG (adjunto, desde react-native-svg). Lo usan solo el
// paso final «Firma del cliente y cierre» y la recepción de traslados (firma del técnico).
import type { Trazo } from "@iris/ui/firma";
import { Eraser } from "lucide-react-native";
import { useCallback, useEffect, useState, useSyncExternalStore } from "react";
import { PanResponder, StyleSheet, View, type LayoutChangeEvent } from "react-native";
import Svg, { Path } from "react-native-svg";
import { Boton } from "../../componentes";
import { base64ABytes } from "../../campo/base64";
import type { LienzoListo } from "../../campo/firma";
import { useTema } from "../../tema/tema";

export type { LienzoListo };

const rutaDe = (t: Trazo) => {
    const [p, ...resto] = t;
    return p ? `M${p.x} ${p.y}` + (resto.length ? resto.map((q) => `L${q.x} ${q.y}`).join("") : `L${p.x + 0.1} ${p.y}`) : "";
};

/**
 * Estado del lienzo fuera de React: los gestos llegan muy seguido y se acumulan aquí; la pantalla
 * se suscribe (`useSyncExternalStore`) y el formulario recibe `LienzoListo` en cada trazo.
 */
export class ControladorFirma {
    private trazos: Trazo[] = [];
    private ancho = 0;
    private alto = 0;
    private svg: Svg | null = null;
    private oyentes = new Set<() => void>();
    private alCambiar: (l: LienzoListo) => void = () => undefined;

    readonly gestos = PanResponder.create({
        onStartShouldSetPanResponder: () => true,
        onMoveShouldSetPanResponder: () => true,
        onPanResponderGrant: (e) => this.empezar(e.nativeEvent.locationX, e.nativeEvent.locationY),
        onPanResponderMove: (e) => this.seguir(e.nativeEvent.locationX, e.nativeEvent.locationY),
    });

    suscribir = (oyente: () => void) => {
        this.oyentes.add(oyente);
        return () => this.oyentes.delete(oyente);
    };

    obtener = (): Trazo[] => this.trazos;

    /** Quién recibe cada trazo (el formulario de la firma). */
    conectar(alCambiar: (l: LienzoListo) => void): () => void {
        this.alCambiar = alCambiar;
        return () => {
            this.alCambiar = () => undefined;
        };
    }

    ponerSvg = (svg: Svg | null) => {
        this.svg = svg;
    };

    medir = (e: LayoutChangeEvent) => {
        this.ancho = e.nativeEvent.layout.width;
        this.alto = e.nativeEvent.layout.height;
    };

    empezar(x: number, y: number): void {
        this.publicar([...this.trazos, [{ x, y }]]);
    }

    seguir(x: number, y: number): void {
        const t = this.trazos.slice();
        const ultimo = t.pop() ?? [];
        this.publicar([...t, [...ultimo, { x, y }]]);
    }

    borrar(): void {
        this.publicar([]);
    }

    private png(): Promise<Uint8Array> {
        return new Promise<Uint8Array>((resolver, rechazar) => {
            if (!this.svg) {
                rechazar(new Error("No se pudo generar el PNG de la firma"));
                return;
            }
            this.svg.toDataURL((b64) => resolver(base64ABytes(b64)), { width: Math.round(this.ancho * 2), height: Math.round(this.alto * 2) });
        });
    }

    private publicar(t: Trazo[]): void {
        this.trazos = t;
        for (const o of this.oyentes) {
            o();
        }
        this.alCambiar({ trazos: t, ancho: this.ancho, alto: this.alto, png: () => this.png() });
    }
}

export function LienzoFirma({ alCambiar, etiqueta = "Recuadro para firmar" }: { alCambiar: (l: LienzoListo) => void; etiqueta?: string }) {
    const { colores, radio } = useTema();
    const [lienzo] = useState(() => new ControladorFirma());
    const trazos = useSyncExternalStore(lienzo.suscribir, lienzo.obtener, lienzo.obtener);
    useEffect(() => lienzo.conectar(alCambiar), [lienzo, alCambiar]);
    const ponerSvg = useCallback((svg: Svg | null) => lienzo.ponerSvg(svg), [lienzo]);

    return (
        <View style={estilos.pila}>
            <View
                accessibilityLabel={etiqueta}
                testID="lienzo-firma"
                onLayout={lienzo.medir}
                style={[estilos.lienzo, { borderColor: colores.borde, borderRadius: radio }]}
                {...lienzo.gestos.panHandlers}
            >
                {/* Fondo blanco siempre: la firma es tinta oscura también en modo oscuro (igual que el PNG). */}
                <Svg ref={ponerSvg} width="100%" height="100%">
                    {trazos.map((t, i) => (
                        <Path key={i} d={rutaDe(t)} stroke="#111111" strokeWidth={2.5} strokeLinecap="round" strokeLinejoin="round" fill="none" />
                    ))}
                </Svg>
            </View>
            <Boton variante="fantasma" icono={Eraser} alPresionar={() => lienzo.borrar()}>
                Borrar firma
            </Boton>
        </View>
    );
}

const estilos = StyleSheet.create({
    pila: { gap: 12 },
    lienzo: { height: 200, borderWidth: 1, borderStyle: "dashed", backgroundColor: "#ffffff", overflow: "hidden" },
});
