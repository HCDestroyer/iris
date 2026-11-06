// Textos de la app de campo en lenguaje claro: estados (texto + tono), tipos, motivos y problemas
// de la API. Un solo lugar para que la PWA, la app nativa y sus pruebas digan lo mismo; cada app
// pone el icono de cada estado (Lucide de su plataforma).
import type { AvisoResultado, EstadoOrden, FormularioOrden, MotivoFalla, MotivoPausa, OrdenCampo, Problema, TipoOperacionLote, TipoOrden } from "../contrato";
import { codigoProblema } from "../errores";

/** Tono de un estado (mismos nombres que `Tono` de `@iris/ui`). */
export type TonoEstado = "neutral" | "info" | "success" | "warning" | "error";

export const ESTADOS_TEXTO: Record<EstadoOrden, { tono: TonoEstado; texto: string }> = {
    pendiente: { tono: "neutral", texto: "Pendiente" },
    programada: { tono: "neutral", texto: "Programada" },
    en_camino: { tono: "info", texto: "En camino" },
    en_sitio: { tono: "info", texto: "En sitio" },
    pausada: { tono: "warning", texto: "Pausada" },
    cerrada: { tono: "success", texto: "Cerrada" },
    fallida: { tono: "error", texto: "No realizada" },
    cancelada: { tono: "neutral", texto: "Cancelada" },
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

export const MOTIVOS_PAUSA: Record<MotivoPausa, string> = {
    esperando_material: "Esperando material",
    cliente_ausente: "El cliente no está",
    clima: "Mal clima",
    acceso: "Sin acceso por ahora",
    otro: "Otro motivo",
};

export const ORIGENES_FORMULARIO: Record<FormularioOrden["origen"], string> = {
    tipo: "Del tipo de orden",
    despacho: "Agregado por despacho",
    tecnico: "Agregado por ti",
};

/** Avisos de una operación que la API aplicó con un ajuste: el técnico los ve (nada en silencio). */
export const AVISOS_SERVIDOR: Partial<Record<AvisoResultado, string>> = {
    version_anterior:
        "El formulario se editó mientras no tenías señal: se guardó con la versión que viste (tus respuestas son de esos ítems).",
    pausa_ajustada: "La pausa empezaba antes del fin de la anterior: el servidor ajustó su inicio para que no se enciman.",
    solapa_con: "Tu tarea se guardó, pero choca con una cita de tu agenda: revisa la hora.",
};

export const CONDICIONES = { buena: "Buena", danada: "Dañada", perdido: "Perdido" } as const;

export const OPERACIONES: Record<TipoOperacionLote, string> = {
    "orden.iniciar_camino": "En camino",
    "orden.llegar": "Llegué",
    "checklist.marcar": "Checklist",
    "nota.agregar": "Nota",
    "nota.editar": "Nota editada",
    "nota.eliminar": "Nota eliminada",
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
    "traslado.recibir": "Recepción de traslado",
    "tarea.crear": "Tarea propia",
    "tarea.completar": "Tarea hecha",
    "tarea.anular": "Tarea anulada",
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
    tarea_anulada: "Tu supervisor anuló la tarea.",
    tarea_completada: "La tarea ya estaba hecha.",
    tarea_ajena: "Ese id ya es de otra tarea.",
    orden_ya_tomada: "Otro técnico tomó la orden primero.",
    libres_apagado: "La empresa no permite tomar órdenes libres.",
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
    version_anterior: "El formulario tiene una versión más nueva; se usó la que viste.",
    pausa_ajustada: "El servidor ajustó la hora de inicio de la pausa.",
    plantilla_inactiva: "Ese formulario se desactivó mientras no tenías señal. Quítalo del teléfono o pide a despacho que lo active.",
    formulario_duplicado:
        "Despacho ya agregó ese formulario a la orden. Si es la misma versión, tus respuestas pasan al que ya tiene; si la versión es distinta, revisa «Por enviar» antes de descartar: puede haber respuestas que no se guardaron.",
    formulario_ajeno: "Ese formulario lo agregó despacho u otro técnico: no lo puedes quitar.",
    formulario_ya_quitado: "Ese formulario ya se había quitado.",
    formulario_con_respuestas: "El formulario ya tiene respuestas: no se puede quitar.",
    formulario_del_tipo: "El formulario del tipo de orden nunca se quita.",
    formulario_quitado: "Despacho quitó ese formulario mientras no tenías señal: esta respuesta no se guardó. Pide que lo vuelva a asignar.",
    orden_ya_pausada: "La orden ya estaba pausada (despacho u otro teléfono la pausó).",
    orden_no_pausada: "La orden ya no estaba pausada (despacho la reanudó, reprogramó o canceló).",
    orden_pausada: "La orden está pausada: primero reanúdala.",
    conflicto_version: "Alguien la cambió mientras no tenías señal. Revisa cómo quedó y vuelve a intentarlo con «Rehacer», o descártala.",
    nota_eliminada: "Esa nota ya se había eliminado.",
    nota_ajena: "Esa nota la escribió otra persona: solo ella (o despacho) la cambia.",
    nota_no_encontrada: "Esa nota ya no está en la orden.",
    traslado_no_encontrado: "Argos no tiene ese traslado para ti (o ya no es tuyo). Descártalo: si llegó el material, avisa a bodega.",
    traslado_sin_confirmar: "Argos no respondió durante 24 h. Vuelve a intentarlo con «Rehacer».",
    recepcion_en_envio: "Otra recepción de este traslado se estaba enviando. Vuelve a intentarlo con «Rehacer».",
    traslado_cancelado: "Bodega canceló el traslado: ya no se recibe.",
    traslado_ya_recibido: "Ese traslado ya estaba recibido.",
    traslado_de_otro_tecnico: "Ese traslado es para otro técnico.",
    lineas_invalidas: "Las líneas recibidas no coinciden con el traslado.",
    firma_invalida: "La firma no es válida: vuelve a firmar.",
};

export function textoProblema(problema: Problema | null | undefined, porDefecto = "El servidor no aceptó la acción."): string {
    const codigo = codigoProblema(problema);
    return (codigo ? PROBLEMAS[codigo] : undefined) ?? problema?.detail ?? problema?.title ?? porDefecto;
}

function etiquetaItem(orden: Pick<OrdenCampo, "checklist"> | null | undefined, codigo: string): string {
    return orden?.checklist?.find((i) => i.codigo === codigo)?.etiqueta ?? codigo;
}

/** Lo que el técnico lee cuando al envío le faltó un dato (p. ej. `versionNota`). */
export const TEXTO_FALTA_DATO = "Falta un dato; toca «Rehacer» para enviarlo de nuevo.";
/** Lo que el técnico lee cuando un dato no tiene la forma que pide la API. */
export const TEXTO_DATO_INVALIDO = "Un dato no tiene el formato esperado; toca «Rehacer» para enviarlo de nuevo o «Descartar».";

/**
 * Mensaje en inglés del validador de esquema de la API (`expected required property … to be
 * present`, `expected number >= 1`…): nunca llega tal cual al técnico.
 */
function esMensajeDelValidador(mensaje: string, regla?: string): boolean {
    return regla === "esquema" || /^(expected|unexpected)\b/i.test(mensaje.trim());
}

function mensajeDelValidador(mensaje: string): string {
    return /\brequired\b/i.test(mensaje) ? TEXTO_FALTA_DATO : TEXTO_DATO_INVALIDO;
}

/**
 * Un `errors[].field` del problema, explicado (lo que falta o lo que está mal). Con `regla`
 * (`errors[].rule`) `esquema`, o un mensaje en inglés del validador, da un texto en español.
 */
export function describirCampo(campo: string, mensaje: string, orden?: Pick<OrdenCampo, "checklist"> | null, regla?: string): string {
    if (mensaje && esMensajeDelValidador(mensaje, regla)) {
        return mensajeDelValidador(mensaje);
    }
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
        case "formularios":
            // `formularios.<id>.checklist.<código>`: la API ya dice qué ítem y de qué formulario.
            return mensaje || `Falta un ítem de un formulario (${campo}).`;
        default:
            return mensaje || campo;
    }
}

/** Motivo completo de un rechazo: el código en claro y, debajo, cada cosa que falta o está mal. */
export function detallesProblema(problema: Problema | null | undefined, orden?: Pick<OrdenCampo, "checklist"> | null): string[] {
    // Sin repetir: dos datos que faltan se leen como una sola línea.
    return [...new Set((problema?.errors ?? []).map((e) => describirCampo(e.field, e.message, orden, e.rule)))];
}

/** Ítem que falta en el cierre, por formulario (`formularios.<id>.checklist.<código>`). */
export interface FaltanteFormulario {
    formularioId: string;
    codigo: string;
    mensaje: string;
}

export function faltantesPorFormulario(problema: Problema | null | undefined): FaltanteFormulario[] {
    const lista: FaltanteFormulario[] = [];
    for (const e of problema?.errors ?? []) {
        const m = /^formularios\.([^.]+)\.checklist\.(.+)$/.exec(e.field);
        if (m?.[1] && m[2]) {
            lista.push({ formularioId: m[1], codigo: m[2], mensaje: e.message });
        }
    }
    return lista;
}
