import { Aviso, InsigniaEstado } from "@iris/ui";
import { RefreshCcwDot } from "lucide-react";
import { cambiosDeArgos, textoCamposArgos, type DatosArgosCambiados } from "@/lib/ordenes/datos-argos";
import { formatearFecha } from "@/lib/fechas";

/** Marca «Dato de Argos cambió» (cabecera de la orden y tablero). */
export function MarcaDatosArgos({ cambios }: { cambios: DatosArgosCambiados }) {
    const que = textoCamposArgos(cambios.campos);
    return (
        <InsigniaEstado
            tono="warning"
            icono={RefreshCcwDot}
            texto="Dato de Argos cambió"
            title={que ? `Cambió en Argos: ${que}` : "Cambió un dato en Argos"}
            data-testid="datos-argos-cambiados"
        />
    );
}

/**
 * Aviso en la ficha: qué cambió en Argos y cuándo. La orden conserva lo que copió; despacho lo
 * revisa. Iris aún no tiene la acción para aplicarlo (pendiente en la API): solo se avisa.
 */
export function AvisoDatosArgos({ orden }: { orden: object }) {
    const cambios = cambiosDeArgos(orden);
    if (!cambios) return null;
    const que = textoCamposArgos(cambios.campos);
    return (
        <Aviso tono="warning" titulo="Dato de Argos cambió" icono={RefreshCcwDot} data-testid="aviso-datos-argos">
            {que ? `Cambió en Argos: ${que}` : "Cambió un dato del cliente o del servicio en Argos"} ({formatearFecha(cambios.en)}). La orden conserva lo que copió al
            crearse: revísalo con el cliente antes de la visita.
        </Aviso>
    );
}
