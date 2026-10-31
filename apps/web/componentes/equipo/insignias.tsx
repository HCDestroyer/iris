import { InsigniaEstado } from "@iris/ui";
import { CircleCheck, CirclePause } from "lucide-react";

/** Activo / inactivo de una ficha, cuadrilla o zona: icono + texto + color. */
export function InsigniaActivo({ activo, femenino = false }: { activo: boolean; femenino?: boolean }) {
    return activo ? (
        <InsigniaEstado tono="success" icono={CircleCheck} texto={femenino ? "Activa" : "Activo"} />
    ) : (
        <InsigniaEstado tono="neutral" icono={CirclePause} texto={femenino ? "Inactiva" : "Inactivo"} />
    );
}
