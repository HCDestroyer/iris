// Textos de la app de campo en lenguaje claro: estados (icono + texto + tono), tipos, motivos y
// problemas de la API. Un solo lugar para que la pantalla y las pruebas digan lo mismo.
import type { EstadoOrden, MotivoFalla, OrdenCampo, Problema, TipoOperacion, TipoOrden } from "@iris/sync";
import { codigoProblema } from "@iris/sync";
import type { Tono } from "@iris/ui";
import { Ban, CalendarClock, CircleCheck, CircleDashed, CirclePause, CircleX, MapPinCheck, Navigation, type LucideIcon } from "lucide-react";

export type PresentacionEstado = { tono: Tono; icono: LucideIcon; texto: string };

export const ESTADOS: Record<EstadoOrden, PresentacionEstado> = {
    pendiente: { tono: "neutral", icono: CircleDashed, texto: "Pendiente" },
    programada: { tono: "neutral", icono: CalendarClock, texto: "Programada" },
    en_camino: { tono: "info", icono: Navigation, texto: "En camino" },
    en_sitio: { tono: "info", icono: MapPinCheck, texto: "En sitio" },
    pausada: { tono: "warning", icono: CirclePause, texto: "Pausada" },
    cerrada: { tono: "success", icono: CircleCheck, texto: "Cerrada" },
    fallida: { tono: "error", icono: CircleX, texto: "No realizada" },
    cancelada: { tono: "neutral", icono: Ban, texto: "Cancelada" },
};

export const TIPOS_ORDEN: Record<TipoOrden, string> = {
    instalacion: "Instalación",
    visita: "Visita técnica",
    retiro: "Retiro",
    cambio_equipo: "Cambio de equipo",
    mantenimiento: "Mantenimiento",
};

export const MOTIVOS_FALLA: Record<MotivoFalla, string> = {
    cliente_ausente: "El cliente no estaba",
    sin_acceso: "Sin acceso al lugar",
    sin_cobertura: "Sin cobertura",
    falta_material: "Falta material",
    equipo_defectuoso: "Equipo defectuoso",
    clima: "Mal clima",
    cliente_rechaza: "El cliente no aceptó",
    otro: "Otro motivo",
};

export const CONDICIONES = { buena: "Buena", danada: "Dañada", perdido: "Perdido" } as const;

export const OPERACIONES: Record<TipoOperacion, string> = {
    "orden.iniciar_camino": "En camino",
    "orden.llegar": "Llegué",
    "checklist.marcar": "Checklist",
    "nota.agregar": "Nota",
    "material.usar": "Material",
    "equipo.instalar": "Equipo instalado",
    "equipo.retirar": "Equipo retirado",
    "firma.capturar": "Firma del cliente",
    "adjunto.registrar": "Foto o archivo",
    "orden.cerrar": "Cierre de la orden",
    "orden.fallar": "Orden no realizada",
    "orden.pausar": "Pausa",
    "orden.reanudar": "Reanudación",
    "orden.agregar_formulario": "Formulario agregado",
    "orden.quitar_formulario": "Formulario quitado",
};

/** Motivo de un rechazo o error de la API, en palabras del técnico. */
const PROBLEMAS: Record<string, string> = {
    transicion_invalida: "La orden ya no está en un estado que permita esta acción.",
    orden_reasignada: "La orden se asignó a otro técnico.",
    orden_cancelada: "Despacho canceló la orden.",
    bloqueada_por_rechazo: "Quedó detenida por un rechazo anterior de la misma orden.",
    faltan_adjuntos: "Faltan fotos o la firma por terminar de subir.",
    cierre_incompleto: "Falta completar el trabajo antes de cerrar.",
    validacion: "Algún dato no es válido.",
    no_encontrado: "La orden ya no existe o ya no la puedes ver.",
    operacion_ajena: "Esta operación la registró otro usuario.",
    equipo_no_admitido: "Este tipo de orden no admite ese movimiento de equipo.",
    equipo_no_en_comodato: "Ese equipo no está en comodato de este servicio.",
    equipo_duplicado: "Ese equipo ya está registrado en la orden.",
    tecnico_fuera_de_empresa: "La orden es de otra empresa.",
    mime_no_permitido: "Ese tipo de archivo no se acepta.",
    tamano_excedido: "El archivo es demasiado grande.",
    sha256_distinto: "El archivo llegó dañado.",
    mime_distinto: "El archivo no es del tipo que dice ser.",
    adjunto_rechazado: "El servidor rechazó el archivo.",
    adjunto_existente: "Ya existe otro archivo con el mismo identificador.",
    entidad_no_adjuntable: "No se pueden adjuntar archivos a esta orden.",
    demasiadas_subidas: "Hay demasiadas subidas en curso; se reintenta sola.",
};

export function textoProblema(problema: Problema | null | undefined, porDefecto = "El servidor no aceptó la acción."): string {
    const codigo = codigoProblema(problema);
    return (codigo ? PROBLEMAS[codigo] : undefined) ?? problema?.detail ?? problema?.title ?? porDefecto;
}

function etiquetaItem(orden: Pick<OrdenCampo, "checklist"> | null | undefined, codigo: string): string {
    return orden?.checklist?.find((i) => i.codigo === codigo)?.etiqueta ?? codigo;
}

/** Un `errors[].field` del problema, explicado (lo que falta o lo que está mal). */
export function describirCampo(campo: string, mensaje: string, orden?: Pick<OrdenCampo, "checklist"> | null): string {
    const [raiz, ...resto] = campo.split(".");
    const clave = resto.join(".");
    switch (raiz) {
        case "adjuntos": {
            if (clave === "firma") {
                return "Falta subir la firma del cliente.";
            }
            const [tipo, item] = clave.split(":", 2);
            if (tipo === "foto" && item) {
                return `Falta subir la foto «${etiquetaItem(orden, item)}».`;
            }
            return `Falta subir: ${clave}.`;
        }
        case "checklist":
            return `Falta el ítem «${etiquetaItem(orden, clave)}» del checklist.`;
        case "equipos":
            return clave === "retirar" ? "Registra el equipo retirado." : "Registra el equipo instalado.";
        case "firma":
            return "Falta la firma del cliente.";
        default:
            return mensaje || campo;
    }
}

/** Motivo completo de un rechazo: el código en claro y, debajo, cada cosa que falta o está mal. */
export function detallesProblema(problema: Problema | null | undefined, orden?: Pick<OrdenCampo, "checklist"> | null): string[] {
    return (problema?.errors ?? []).map((e) => describirCampo(e.field, e.message, orden));
}
