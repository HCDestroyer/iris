// El hilo del chat sigue al último mensaje (reporte del PM, APK 0.18.0: con el teclado abierto el
// compositor subía pero el hilo no, y el último mensaje quedaba debajo del teclado o del compositor).
// El hilo es un `ScrollView` normal (no una lista invertida): el final es abajo. Reglas:
// - Si la persona está al final, se queda al final: al abrirse el teclado (el `KeyboardAvoidingView`
//   acorta el hilo: `onLayout`, y el alto del teclado de keyboard-controller por si acaso), al crecer
//   el contenido (llega o se envía un mensaje) y tras enviar.
// - Enviar siempre baja al final (lo propio se ve aunque estuviera leyendo arriba).
// - Si subió a leer anteriores, no se la mueve: un mensaje nuevo de otro muestra «Mensajes nuevos»
//   y tocarlo baja al final.
import { useEffect, useRef, useState, type RefObject } from "react";
import type { LayoutChangeEvent, NativeScrollEvent, NativeSyntheticEvent } from "react-native";

/** Margen (px) dentro del cual se cuenta como «al final». */
export const MARGEN_FINAL = 48;

interface Desplazable {
    scrollToEnd(o?: { animated?: boolean }): void;
}

export interface SeguirFinal {
    /** Props para el `ScrollView` del hilo. */
    alDesplazar: (e: NativeSyntheticEvent<NativeScrollEvent>) => void;
    alCambiarContenido: () => void;
    alMedir: (e: LayoutChangeEvent) => void;
    /** ¿Está al final? (para marcar leído). */
    enFinal: boolean;
    /** Llegó algo de otro mientras leía arriba. */
    hayNuevos: boolean;
    /** «Mensajes nuevos»: baja al final. */
    irAlFinal: () => void;
    /** Tras enviar: baja al final aunque estuviera arriba. */
    alEnviar: () => void;
}

/**
 * @param ultimo  id del último mensaje y si es propio (cambia al llegar o enviar uno).
 * @param teclado alto del teclado abierto (0 cerrado).
 */
export function useSeguirFinal(lista: RefObject<Desplazable | null>, ultimo: { id: string; propio: boolean } | null, teclado: number): SeguirFinal {
    const [enFinal, setEnFinal] = useState(true);
    const [hayNuevos, setHayNuevos] = useState(false);
    // Cada «bajar» (enviar, «Mensajes nuevos», uno propio que llega) cuenta una vez: el efecto desplaza.
    const [bajadas, setBajadas] = useState(0);
    const alto = useRef(0);

    const bajar = () => {
        setEnFinal(true);
        setHayNuevos(false);
        setBajadas((b) => b + 1);
    };

    // Un mensaje nuevo (comparado con el render anterior): propio → al final; de otro con la persona
    // arriba → «Mensajes nuevos». La primera carga (o el hilo vacío) no cuenta.
    const id = ultimo?.id ?? null;
    const [previo, setPrevio] = useState<string | null>(id);
    if (previo !== id) {
        setPrevio(id);
        if (previo !== null && id !== null) {
            if (ultimo?.propio) {
                bajar();
            } else if (!enFinal) {
                setHayNuevos(true);
            }
        }
    }

    useEffect(() => {
        if (bajadas > 0) {
            lista.current?.scrollToEnd({ animated: true });
        }
    }, [bajadas, lista]);

    useEffect(() => {
        if (teclado > 0 && enFinal) {
            lista.current?.scrollToEnd({ animated: true });
        }
    }, [teclado, enFinal, lista]);

    return {
        alDesplazar: (e) => {
            const { contentOffset, contentSize, layoutMeasurement } = e.nativeEvent;
            const final = contentSize.height - contentOffset.y - layoutMeasurement.height < MARGEN_FINAL;
            setEnFinal(final);
            if (final) {
                setHayNuevos(false);
            }
        },
        // Llega o se envía algo (el contenido crece): si estaba al final, sigue al final.
        alCambiarContenido: () => {
            if (enFinal) {
                lista.current?.scrollToEnd({ animated: false });
            }
        },
        // El teclado (o el compositor que crece) acorta el hilo: si estaba al final, sigue al final.
        alMedir: (e) => {
            const nuevo = e.nativeEvent.layout.height;
            const cambio = nuevo !== alto.current;
            alto.current = nuevo;
            if (cambio && enFinal) {
                lista.current?.scrollToEnd({ animated: false });
            }
        },
        enFinal,
        hayNuevos,
        irAlFinal: bajar,
        alEnviar: bajar,
    };
}
