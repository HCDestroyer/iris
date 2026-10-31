// Recepción de un traslado («Por recibir»): lo que faltó por línea y la firma del técnico van a la
// cola con `motor.recibirTraslado` de `@iris/sync`, igual que la PWA, con guarda contra el doble
// toque. La pantalla solo dibuja.
import type { OperacionTrasladoLocal, Traslado } from "@iris/sync";
import { lineasDeRecepcion, situacionLocal, situacionTraslado, type FormularioRecepcion, type SituacionTraslado } from "@iris/sync/campo";
import { hayFirma } from "@iris/ui/firma";
import { useState } from "react";
import { useCampo } from "./contexto";
import type { LienzoListo } from "./firma";
import { useAccionUnica } from "./una-vez";

/** Situación de un traslado con lo del teléfono: de la lista de Argos o solo local (sin señal). */
export function situacionDe(traslado: Traslado | null, local: OperacionTrasladoLocal | null, recepciones: readonly OperacionTrasladoLocal[]): SituacionTraslado | null {
    if (traslado) {
        return situacionTraslado(traslado, recepciones);
    }
    return local ? situacionLocal(local) : null;
}

/** Se puede (volver a) recibir: nadie lo recibió desde Iris o Argos rechazó la recepción anterior. */
export const sePuedeRecibir = (s: SituacionTraslado | null) => s === "por_recibir" || s === "rechazado_argos";

export function useRecepcion(traslado: Traslado) {
    const { motor, ejecutar } = useCampo();
    const guarda = useAccionUnica();
    const [lienzo, setLienzo] = useState<LienzoListo | null>(null);
    const [errorFirma, setErrorFirma] = useState<string | undefined>();

    /** «Confirmar recepción»: una sola vez por toque. */
    const confirmar = (d: FormularioRecepcion) =>
        guarda.correr(async () => {
            if (!lienzo || !hayFirma(lienzo.trazos)) {
                setErrorFirma("Firma en el recuadro para confirmar que recibiste");
                return false;
            }
            setErrorFirma(undefined);
            return ejecutar(async () => {
                await motor.recibirTraslado({ trasladoId: traslado.id, firma: await lienzo.png(), lineas: lineasDeRecepcion(traslado, d) });
            }, "Recepción guardada");
        });

    return { setLienzo, errorFirma, confirmar, ocupado: guarda.ocupado };
}
