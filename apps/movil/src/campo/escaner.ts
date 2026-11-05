// Escáner de serie y MAC por campo en la app nativa (expo-camera): lo que entrega
// `onBarcodeScanned` pasado a los códigos de `@iris/sync/campo`, solo los del recuadro, y la sesión
// de escaneo (ventana corta, candidatos, «Seguir escaneando») con la vibración corta. La pantalla
// (`pantallas/orden/Escaner.tsx`) solo dibuja.
import {
    dentroDelRecuadro,
    recuadroCentrado,
    SesionEscaneo,
    type Caja,
    type CodigoVisto,
    type EstadoEscaneo,
    type Medidas,
    type ObjetivoEscaneo,
} from "@iris/sync/campo";
import type { BarcodeScanningResult } from "expo-camera";
import { useEffect, useState } from "react";
import { Vibration } from "react-native";

/**
 * Un resultado de expo-camera como código visto: `bounds` (si no es un rectángulo vacío) y
 * `cornerPoints`, en puntos de la vista de la cámara.
 */
export function codigoDeExpo(r: Pick<BarcodeScanningResult, "data" | "type"> & Partial<Pick<BarcodeScanningResult, "bounds" | "cornerPoints">>): CodigoVisto {
    const b = r.bounds;
    const caja = b && b.size.width > 0 && b.size.height > 0 ? { x: b.origin.x, y: b.origin.y, ancho: b.size.width, alto: b.size.height } : null;
    return { valor: r.data, formato: r.type, caja, esquinas: r.cornerPoints && r.cornerPoints.length > 0 ? r.cornerPoints : null };
}

/** Vibración corta al detectar (sin `expo-haptics`: el `Vibration` de React Native). */
export function vibrarCorto(): void {
    Vibration.vibrate(40);
}

/**
 * Estado del visor: la sesión de esta apertura, el recuadro (cuando se conoce la medida de la vista;
 * antes, cuentan todos los códigos) y qué hacer con cada resultado del lector.
 */
export function useEscaneo(objetivo: ObjetivoEscaneo, existentes: readonly { serie?: string | null; mac?: string | null }[]) {
    const [estado, setEstado] = useState<EstadoEscaneo>({ detectados: 0, candidatos: null });
    const [vista, setVista] = useState<Medidas | null>(null);
    // Una sesión por apertura: mientras el escáner está abierto no cambian el objetivo ni los equipos.
    const [sesion] = useState(() => new SesionEscaneo({ objetivo, existentes: () => existentes, alCambiar: setEstado, alPrimerCodigo: vibrarCorto }));
    useEffect(() => () => sesion.liberar(), [sesion]);
    const recuadro: Caja | null = vista ? recuadroCentrado(vista) : null;
    const leer = (r: BarcodeScanningResult) => {
        if (!r.data) {
            return;
        }
        const codigo = codigoDeExpo(r);
        sesion.detectar(recuadro ? dentroDelRecuadro([codigo], recuadro) : [codigo]);
    };
    return { estado, recuadro, setVista, leer, seguir: () => sesion.seguir() };
}
