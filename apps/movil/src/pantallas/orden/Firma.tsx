// Firma del cliente: nombre, documento y trazo con el dedo → SVG (a la API, el mismo de la PWA con
// `trazosASvg` de `@iris/ui/firma`) y PNG (adjunto `firma`, desde react-native-svg).
import { zodResolver } from "@hookform/resolvers/zod";
import type { OrdenVista } from "@iris/sync";
import { esquemaFirma, type DatosFirma } from "@iris/sync/campo";
import { hayFirma, trazosASvg, type Trazo } from "@iris/ui/firma";
import { Eraser, PenLine } from "lucide-react-native";
import { useCallback, useEffect, useState, useSyncExternalStore } from "react";
import { Controller, useForm } from "react-hook-form";
import { PanResponder, StyleSheet, View, type LayoutChangeEvent } from "react-native";
import Svg, { Path } from "react-native-svg";
import { Boton, Campo, Insignia, Tarjeta, Texto } from "../../componentes";
import { base64ABytes } from "../../campo/base64";
import { useCampo } from "../../campo/contexto";
import { useTema } from "../../tema/tema";
import { EstadoAdjunto } from "./Fotos";

/** Lo que el lienzo expone para guardar: los trazos, su tamaño y el PNG. */
export interface LienzoListo {
    trazos: Trazo[];
    ancho: number;
    alto: number;
    png: () => Promise<Uint8Array>;
}

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

export function LienzoFirma({ alCambiar }: { alCambiar: (l: LienzoListo) => void }) {
    const { colores, radio } = useTema();
    const [lienzo] = useState(() => new ControladorFirma());
    const trazos = useSyncExternalStore(lienzo.suscribir, lienzo.obtener, lienzo.obtener);
    useEffect(() => lienzo.conectar(alCambiar), [lienzo, alCambiar]);
    const ponerSvg = useCallback((svg: Svg | null) => lienzo.ponerSvg(svg), [lienzo]);

    return (
        <View style={estilos.pila}>
            <View
                accessibilityLabel="Recuadro para firmar"
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

export function Firma({ orden, editable }: { orden: OrdenVista; editable: boolean }) {
    const { estado, motor, ejecutar } = useCampo();
    const [rehacer, setRehacer] = useState(false);
    const [lienzo, setLienzo] = useState<LienzoListo | null>(null);
    const [errorTrazo, setErrorTrazo] = useState<string | undefined>();
    const form = useForm<DatosFirma>({ resolver: zodResolver(esquemaFirma), defaultValues: { nombre: orden.contacto.nombre ?? "", documento: "" } });
    const firma = orden.firma;

    const guardar = form.handleSubmit(async (d) => {
        if (!lienzo || !hayFirma(lienzo.trazos)) {
            setErrorTrazo("Pide al cliente que firme en el recuadro");
            return;
        }
        setErrorTrazo(undefined);
        const ok = await ejecutar(async () => {
            const svg = trazosASvg(lienzo.trazos, lienzo.ancho, lienzo.alto);
            const png = await lienzo.png();
            const { adjunto } = await motor.agregarAdjunto({ ordenId: orden.id, tipo: "firma", datos: png, mime: "image/png", obligatorio: orden.requiereFirma, tomadoEn: new Date().toISOString() });
            await motor.registrar("firma.capturar", orden.id, { nombre: d.nombre, svg, adjuntoId: adjunto.id, ...(d.documento ? { documento: d.documento } : {}) });
        }, "Firma guardada");
        if (ok) {
            setRehacer(false);
        }
    });

    return (
        <Tarjeta titulo="Firma del cliente" descripcion={orden.requiereFirma ? "Obligatoria para cerrar esta orden." : "Opcional."}>
            {firma && !rehacer ? (
                <View style={estilos.pila}>
                    <Insignia tono="success" icono={PenLine} texto="Firmada" />
                    <Texto>
                        {firma.nombre}
                        {firma.documento ? ` · ${firma.documento}` : ""}
                    </Texto>
                    {firma.adjuntoId ? <EstadoAdjunto adjunto={estado.adjuntos.find((a) => a.id === firma.adjuntoId)} /> : null}
                    {editable ? (
                        <Boton variante="fantasma" alPresionar={() => setRehacer(true)}>
                            Volver a firmar
                        </Boton>
                    ) : null}
                </View>
            ) : editable ? (
                <View style={estilos.pila}>
                    <Controller
                        control={form.control}
                        name="nombre"
                        render={({ field }) => <Campo etiqueta="Nombre de quien firma" valor={field.value} alCambiar={field.onChange} onBlur={field.onBlur} error={form.formState.errors.nombre?.message} />}
                    />
                    <Controller
                        control={form.control}
                        name="documento"
                        render={({ field }) => (
                            <Campo etiqueta="Documento (DPI)" valor={field.value} alCambiar={field.onChange} onBlur={field.onBlur} keyboardType="number-pad" error={form.formState.errors.documento?.message} />
                        )}
                    />
                    <LienzoFirma alCambiar={setLienzo} />
                    {errorTrazo ? <Texto>{errorTrazo}</Texto> : null}
                    <Boton variante="secundario" cargando={form.formState.isSubmitting} alPresionar={() => void guardar()}>
                        Guardar firma
                    </Boton>
                </View>
            ) : (
                <Texto suave>Sin firma.</Texto>
            )}
        </Tarjeta>
    );
}

const estilos = StyleSheet.create({
    pila: { gap: 12 },
    lienzo: { height: 200, borderWidth: 1, borderStyle: "dashed", backgroundColor: "#ffffff", overflow: "hidden" },
});
