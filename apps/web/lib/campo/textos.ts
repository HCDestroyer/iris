// Textos de la app de campo: viven en `@iris/sync/campo` (los comparte la app nativa). Aquí solo
// se agrega el icono Lucide de cada estado para la PWA.
import type { EstadoOrden } from "@iris/sync";
import { ESTADOS_TEXTO } from "@iris/sync/campo";
import type { Tono } from "@iris/ui";
import { Ban, CalendarClock, CircleCheck, CircleDashed, CirclePause, CircleX, MapPinCheck, Navigation, type LucideIcon } from "lucide-react";

export {
    AVISOS_SERVIDOR,
    CONDICIONES,
    describirCampo,
    detallesProblema,
    faltantesPorFormulario,
    MOTIVOS_FALLA,
    MOTIVOS_PAUSA,
    OPERACIONES,
    ORIGENES_FORMULARIO,
    textoProblema,
    TIPOS_ORDEN,
    type FaltanteFormulario,
} from "@iris/sync/campo";

export type PresentacionEstado = { tono: Tono; icono: LucideIcon; texto: string };

const ICONOS: Record<EstadoOrden, LucideIcon> = {
    pendiente: CircleDashed,
    programada: CalendarClock,
    en_camino: Navigation,
    en_sitio: MapPinCheck,
    pausada: CirclePause,
    cerrada: CircleCheck,
    fallida: CircleX,
    cancelada: Ban,
};

export const ESTADOS = Object.fromEntries(
    Object.entries(ESTADOS_TEXTO).map(([estado, p]) => [estado, { ...p, icono: ICONOS[estado as EstadoOrden] }]),
) as Record<EstadoOrden, PresentacionEstado>;
