import { Aviso, InsigniaEstado } from "@iris/ui";
import { RadioTower } from "lucide-react";
import { ESTADOS_PORTAL, type EstadoPortal } from "@/lib/portal/presentacion";

/** Estado del caso en lenguaje del cliente: icono + texto + color. */
export function InsigniaEstadoPortal({ estado }: { estado: EstadoPortal }) {
    const p = ESTADOS_PORTAL[estado];
    return <InsigniaEstado tono={p.tono} icono={p.icono} texto={p.texto} data-estado={estado} />;
}

/** Falla general (incidente maestro abierto) que afecta el servicio del cliente. */
export function AvisoFallaGeneral({ className }: { className?: string }) {
    return (
        <Aviso tono="warning" icono={RadioTower} titulo="Hay una falla general en tu zona" data-testid="falla-general" {...(className ? { className } : {})}>
            Ya estamos trabajando para resolverla. No hace falta que abras otro caso: te avisaremos aquí cuando quede resuelta.
        </Aviso>
    );
}

/** Insignia compacta para la lista. */
export function InsigniaFallaGeneral() {
    return <InsigniaEstado tono="warning" icono={RadioTower} texto="Falla general" />;
}
