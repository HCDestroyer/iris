import { Aviso, cn, InsigniaEstado } from "@iris/ui";
import { FlaskConical } from "lucide-react";

/**
 * Las llamadas de WhatsApp aún no se han validado con llamadas reales contra Meta. Mientras sea
 * `true`, todo lo de llamadas lo dice. **Para quitar la marca cuando se validen: pasar a `false`**
 * (y borrar sus pruebas en `e2e/voz.spec.ts`).
 */
export const LLAMADAS_EN_PRUEBAS = true;

export const TEXTO_EN_PRUEBAS = "Las llamadas de WhatsApp están en pruebas: aún no se han validado con llamadas reales. Enciéndelas solo en un número de prueba.";

/** Franja llana arriba de las pantallas de ajustes de llamadas. */
export function FranjaEnPruebas({ className }: { className?: string }) {
    if (!LLAMADAS_EN_PRUEBAS) return null;
    return (
        <Aviso tono="warning" titulo="En pruebas" icono={FlaskConical} className={className} data-slot="llamadas-en-pruebas">
            {TEXTO_EN_PRUEBAS}
        </Aviso>
    );
}

/** Etiqueta discreta junto a los interruptores de llamadas. */
export function EtiquetaEnPruebas({ className }: { className?: string }) {
    if (!LLAMADAS_EN_PRUEBAS) return null;
    return <InsigniaEstado tono="warning" icono={FlaskConical} texto="En pruebas" className={cn("w-fit", className)} data-etiqueta="en-pruebas" />;
}
