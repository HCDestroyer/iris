// Fotos de la orden: cámara → compresión (≤ 1600 px, ~300 KB) → hora y GPS del EXIF (o la
// posición actual) → cola del teléfono; se sube sola por partes (tus) en segundo plano.
import type { AdjuntoLocal, OrdenVista } from "@iris/sync";
import { Camera, CircleCheck, CloudUpload, ImageOff, TriangleAlert } from "lucide-react-native";
import { useCallback, useState } from "react";
import { Image, StyleSheet, View } from "react-native";
import { Boton, Insignia, Tarjeta, Texto } from "../../componentes";
import { useCampo } from "../../campo/contexto";
import { useTema } from "../../tema/tema";

export interface OpcionesFoto {
    ordenId: number;
    obligatorio?: boolean;
    item?: { itemId: number; codigo: string };
}

export function useTomarFoto() {
    const { motor, servicios, agregarMiniatura, notificar } = useCampo();
    const [procesando, setProcesando] = useState(false);
    const tomar = useCallback(
        async (opciones: OpcionesFoto): Promise<string | null> => {
            setProcesando(true);
            try {
                const tomada = await servicios.tomarFoto();
                if (!tomada) {
                    return null;
                }
                const { foto, exif } = tomada;
                const posicion = exif.lat !== undefined ? null : await servicios.posicion(4000);
                const lat = exif.lat ?? posicion?.lat;
                const lon = exif.lon ?? posicion?.lon;
                const { adjunto } = await motor.agregarAdjunto({
                    ordenId: opciones.ordenId,
                    tipo: "foto",
                    datos: foto.bytes,
                    mime: foto.mime,
                    tomadoEn: exif.tomadoEn ?? new Date().toISOString(),
                    ...(opciones.obligatorio ? { obligatorio: true } : {}),
                    ...(opciones.item ? { item: opciones.item } : {}),
                    ...(lat !== undefined && lon !== undefined ? { lat, lon } : {}),
                });
                if (foto.uri) {
                    agregarMiniatura(adjunto.id, foto.uri);
                }
                notificar({ tono: "success", texto: "Foto guardada en el teléfono. Se sube en segundo plano." });
                return adjunto.id;
            } catch (e) {
                notificar({ tono: "error", texto: e instanceof Error && e.message ? `No se pudo guardar la foto: ${e.message}` : "No se pudo guardar la foto." });
                return null;
            } finally {
                setProcesando(false);
            }
        },
        [motor, servicios, agregarMiniatura, notificar],
    );
    return { tomar, procesando };
}

/** Estado de subida de un adjunto: icono + texto + tono. */
export function EstadoAdjunto({ adjunto }: { adjunto: AdjuntoLocal | undefined }) {
    if (!adjunto || adjunto.estado === "subido") {
        return <Insignia tono="success" icono={CircleCheck} texto={adjunto ? "Subida" : "Registrada"} />;
    }
    if (adjunto.estado === "rechazado") {
        return <Insignia tono="error" icono={TriangleAlert} texto="Rechazada" />;
    }
    const pct = adjunto.tamano > 0 ? Math.floor((adjunto.offset / adjunto.tamano) * 100) : 0;
    return <Insignia tono="warning" icono={CloudUpload} texto={adjunto.estado === "por_crear" ? "Por subir" : `Subiendo ${pct} %`} />;
}

export function Miniatura({ id, alt }: { id: string; alt: string }) {
    const { miniaturas } = useCampo();
    const { colores, radio } = useTema();
    const uri = miniaturas.get(id);
    return uri ? (
        <Image source={{ uri }} accessibilityLabel={alt} style={[estilos.miniatura, { borderRadius: radio, borderColor: colores.borde }]} />
    ) : (
        <View accessibilityLabel={alt} style={[estilos.miniatura, estilos.vacia, { borderRadius: radio, borderColor: colores.borde }]}>
            <ImageOff size={20} color={colores.textoSuave} />
        </View>
    );
}

/** Fotos sueltas de la orden (evidencia), también en una orden cancelada. */
export function FotosOrden({ orden }: { orden: OrdenVista }) {
    const { estado } = useCampo();
    const { tomar, procesando } = useTomarFoto();
    const delItem = new Set((orden.checklist ?? []).map((i) => String(i.valor)));
    const fotos = (orden.adjuntos ?? []).filter((a) => a.tipo === "foto" && !delItem.has(a.id) && a.itemId === null);
    return (
        <Tarjeta titulo="Fotos">
            {fotos.length > 0 ? (
                fotos.map((f, i) => (
                    <View key={f.id} style={estilos.fila}>
                        <Miniatura id={f.id} alt={`Foto ${i + 1}`} />
                        <View style={estilos.flex}>
                            <Texto>Foto {i + 1}</Texto>
                        </View>
                        <EstadoAdjunto adjunto={estado.adjuntos.find((a) => a.id === f.id)} />
                    </View>
                ))
            ) : (
                <Texto suave>Sin fotos todavía.</Texto>
            )}
            <Boton variante="secundario" icono={Camera} cargando={procesando} alPresionar={() => void tomar({ ordenId: orden.id })}>
                {procesando ? "Procesando foto…" : "Agregar foto"}
            </Boton>
        </Tarjeta>
    );
}

const estilos = StyleSheet.create({
    flex: { flex: 1 },
    fila: { flexDirection: "row", alignItems: "center", gap: 12 },
    miniatura: { width: 64, height: 64, borderWidth: 1 },
    vacia: { alignItems: "center", justifyContent: "center", borderStyle: "dashed" },
});
