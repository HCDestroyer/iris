import type { Schema } from "@iris/contratos";
import { z } from "zod";
import { api, resolver } from "../api/cliente";
import { cabeceraIdem } from "./api";

/*
 * Boletas de la conversación: aprobar y rechazar desde Iris (contratos/conversaciones.md §6.4,
 * contrato con Argos 0.7.0 §7.7, pedido B3). **Argos decide**: Iris transmite la decisión de la
 * persona, que debe tener su usuario de Argos vinculado. El asistente de IA nunca puede.
 */

export type Boleta = Schema<"Boleta">;
export type CausaRechazo = NonNullable<Boleta["causaRechazo"]>;

const M = "/api/v1/mensajeria";

export const leerBoletas = async (conversacionId: number, signal?: AbortSignal) =>
    (await resolver(api().GET(`${M}/conversaciones/{id}/boletas`, { params: { path: { id: conversacionId } }, signal }))).data ?? [];
export const leerBoleta = (id: string, signal?: AbortSignal) => resolver(api().GET(`${M}/boletas/{id}`, { params: { path: { id } }, signal }));
/** Repetir con la misma clave es seguro (la idempotencia la da el comprobante), también tras vincular. */
export const aprobarBoleta = (id: string, nota: string, clave: string) =>
    resolver(api().POST(`${M}/boletas/{id}/aprobar`, { params: { path: { id }, header: cabeceraIdem(clave) }, body: nota ? { nota } : {} }));
export const rechazarBoleta = (id: string, cuerpo: Schema<"EntradaRechazarBoleta">, clave: string) =>
    resolver(api().POST(`${M}/boletas/{id}/rechazar`, { params: { path: { id }, header: cabeceraIdem(clave) }, body: cuerpo }));

export const CAUSAS: readonly { valor: CausaRechazo; texto: string }[] = [
    { valor: "no_aparece", texto: "El depósito no aparece en el banco" },
    { valor: "monto_distinto", texto: "El monto no coincide" },
    { valor: "ilegible", texto: "La foto no se lee" },
    { valor: "falta_dato", texto: "Le falta un dato (puede mandarla de nuevo)" },
    { valor: "duplicado", texto: "Ya se había registrado" },
    { valor: "otro", texto: "Otro motivo" },
];

/** Espejo de `EntradaAprobarBoleta` (la nota queda en Argos, ≤ 400). */
export const esquemaAprobar = z.object({ nota: z.string().trim().max(400, "Hasta 400 caracteres") });
/** Espejo de `EntradaRechazarBoleta`: el motivo se le dice al cliente (3 a 500). */
export const esquemaRechazar = z.object({
    motivo: z.string().trim().min(3, "Escribe el motivo (al menos 3 letras): se le dice al cliente").max(500, "Hasta 500 caracteres"),
    causa: z.enum(["no_aparece", "monto_distinto", "ilegible", "falta_dato", "duplicado", "otro"]),
});

/** «Q 250.00» (el monto llega como decimal en texto; nunca se calcula con él). */
export function montoLegible(monto: string): string {
    const n = Number(monto);
    return Number.isFinite(n) ? `Q ${n.toLocaleString("es-GT", { minimumFractionDigits: 2, maximumFractionDigits: 2 })}` : `Q ${monto}`;
}

export type TonoBoleta = "success" | "error" | "warning" | "neutral";

/** Estado de la boleta en palabras (icono + texto + color los pone la tarjeta). */
export function estadoBoleta(b: Pick<Boleta, "estado">): { texto: string; tono: TonoBoleta } {
    switch (b.estado) {
        case "por_enviar":
            return { texto: "Enviándose a Argos", tono: "neutral" };
        case "registrada":
            return { texto: "Pendiente de revisar", tono: "warning" };
        case "aprobada":
            return { texto: "Aprobada", tono: "success" };
        case "rechazada":
            return { texto: "Rechazada", tono: "error" };
        default:
            return { texto: "Argos no la aceptó", tono: "error" };
    }
}

/** «Aprobada por Marta Caal (caja)» / «Rechazada por …»: quién decidió, si se sabe. */
export function quienDecidio(b: Pick<Boleta, "estado" | "verificadoPorNombre" | "decididaPor">): string | null {
    if (b.estado !== "aprobada" && b.estado !== "rechazada") return null;
    const nombre = b.verificadoPorNombre ?? b.decididaPor?.nombre ?? null;
    const verbo = b.estado === "aprobada" ? "La aprobó" : "La rechazó";
    return nombre ? `${verbo} ${nombre}` : b.estado === "aprobada" ? "La aprobó caja en Argos" : "La rechazó caja en Argos";
}

export type AccionBoleta = "aprobar" | "rechazar";

/**
 * Qué hacer con un error de aprobar o rechazar:
 * - `vincular`: falta el vínculo con Argos; se ofrece «Vincular mi usuario de Argos» y luego reintentar con la misma clave;
 * - `releer`: ya la decidió otra persona (o caja): la boleta local quedó sincronizada, se vuelve a leer;
 * - `reintentar`: Argos no respondió o no hay red;
 * - `explicar`: no se arregla reintentando.
 */
export type ErrorBoleta = { tipo: "vincular" | "releer" | "reintentar" | "explicar"; titulo: string; texto: string };

type ErrorConCodigo = { estado: number; codigo: string; titulo: string; detalle: string | undefined };

export function explicarErrorBoleta(e: ErrorConCodigo, accion: AccionBoleta): ErrorBoleta {
    const que = accion === "aprobar" ? "aprobar" : "rechazar";
    const noSePudo = `No se pudo ${que} la boleta`;
    switch (e.codigo) {
        case "argos_sin_vinculo":
            return { tipo: "vincular", titulo: "Vincula tu usuario de Argos para aprobar pagos", texto: "Argos necesita saber quién eres allá. Se abre Argos en otra pestaña: entra con tu correo y tu contraseña de Argos, vuelve aquí y toca «Reintentar»." };
        case "argos_sin_permiso":
            return { tipo: "explicar", titulo: "Tu usuario de Argos no tiene permiso de caja", texto: `Pide a un administrador de Argos el permiso para ${que} comprobantes.` };
        case "argos_sin_empresa":
            return { tipo: "explicar", titulo: "Tu usuario de Argos no trabaja con esta empresa", texto: "La boleta es de una empresa que tu usuario de Argos no tiene asignada. Pide a un administrador de Argos que te la asigne." };
        case "argos_usuario_inactivo":
            return { tipo: "explicar", titulo: "Tu usuario de Argos está inactivo", texto: "Pide a un administrador de Argos que lo active." };
        case "solo_personas":
            return { tipo: "explicar", titulo: "Solo una persona puede decidir una boleta", texto: "Entra con tu sesión del panel para aprobarla o rechazarla." };
        case "otra_persona_decidio":
        case "comprobante_revisado":
        case "boleta_ya_decidida":
            return { tipo: "releer", titulo: "Ya la decidió otra persona", texto: "La boleta se actualizó con lo que se decidió." };
        case "argos_sin_aprobacion":
            return { tipo: "explicar", titulo: "Actualiza Argos a 1.15.0 o posterior", texto: "Esta versión de Argos todavía no deja aprobar ni rechazar boletas desde Iris. Mientras tanto, caja las revisa en Argos." };
        case "boleta_no_registrada":
            return { tipo: "explicar", titulo: "La boleta todavía no llega a Argos", texto: "Espera a que diga «Pendiente de revisar» y vuelve a intentarlo." };
        case "conversacion_solo_lectura":
            return { tipo: "explicar", titulo: "La conversación es de solo lectura", texto: "Pasó a otra empresa: la boleta la decide quien la atiende ahora." };
        case "decision_no_permitida":
            return { tipo: "explicar", titulo: noSePudo, texto: e.detalle ?? "Argos no permite esta decisión." };
        case "iris_apagado":
        case "conector_apagado":
            return { tipo: "explicar", titulo: "La conexión con Argos está apagada", texto: "Un administrador la activa en Administración › Integraciones (y en Argos, su conexión con Iris)." };
        case "comprobante_no_es_de_iris":
        case "comprobante_no_existe":
            return { tipo: "explicar", titulo: noSePudo, texto: "Argos no reconoce este comprobante como una boleta de Iris. Revísalo en Argos." };
        case "argos_no_disponible":
        case "idempotencia_en_curso":
            return { tipo: "reintentar", titulo: "Argos no respondió", texto: "No quedó nada a medias. Vuelve a intentarlo." };
        case "sin_conexion":
        case "api_no_disponible":
            return { tipo: "reintentar", titulo: noSePudo, texto: "No hay conexión con Iris. Revisa tu red e inténtalo de nuevo." };
        default:
            return { tipo: e.estado >= 500 || e.estado === 0 ? "reintentar" : "explicar", titulo: noSePudo, texto: e.detalle ?? e.titulo };
    }
}
