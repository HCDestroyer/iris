// Lienzo de firma falso para Jest: «Firmar» entrega un trazo y un PNG fijo (los gestos y el PNG de
// react-native-svg se validan en un teléfono). Se usa con
// `jest.mock("../src/pantallas/orden/LienzoFirma", () => jest.requireActual("./firma-falsa"))`.
import { Text } from "react-native";
import type { LienzoListo } from "../src/campo/firma";

export const PNG_FALSO = new Uint8Array([137, 80, 78, 71, 13, 10, 26, 10]);

let retenido: Promise<void> | null = null;

/** El próximo PNG espera hasta llamar a la función que devuelve (para probar el doble toque). */
export function retenerPng(): () => void {
    let soltar: () => void = () => undefined;
    retenido = new Promise<void>((r) => {
        soltar = r;
    });
    return () => {
        soltar();
        retenido = null;
    };
}

export function LienzoFirma({ alCambiar, etiqueta = "Recuadro para firmar" }: { alCambiar: (l: LienzoListo) => void; etiqueta?: string }) {
    return (
        <Text
            accessibilityRole="button"
            accessibilityLabel={`Firmar en ${etiqueta}`}
            onPress={() =>
                alCambiar({
                    trazos: [
                        [
                            { x: 1, y: 1 },
                            { x: 20, y: 30 },
                        ],
                    ],
                    ancho: 300,
                    alto: 200,
                    png: async () => {
                        await retenido;
                        return PNG_FALSO;
                    },
                })
            }
        >
            {etiqueta}
        </Text>
    );
}
