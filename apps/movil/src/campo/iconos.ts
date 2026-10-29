// Icono Lucide de cada estado de orden (el texto y el tono vienen de `@iris/sync/campo`).
import type { EstadoOrden } from "@iris/sync";
import { Ban, CalendarClock, CircleCheck, CircleDashed, CirclePause, CircleX, MapPinCheck, Navigation, type LucideIcon } from "lucide-react-native";

export const ICONOS_ESTADO: Record<EstadoOrden, LucideIcon> = {
    pendiente: CircleDashed,
    programada: CalendarClock,
    en_camino: Navigation,
    en_sitio: MapPinCheck,
    pausada: CirclePause,
    cerrada: CircleCheck,
    fallida: CircleX,
    cancelada: Ban,
};
