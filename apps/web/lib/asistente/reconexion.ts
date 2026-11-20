import type { Schema } from "@iris/contratos";
import { api, resolver } from "../api/cliente";
import { cabeceraIdem } from "../conversaciones/api";

/*
 * Reconexión temporal sin personal de caja (contrato con Argos 0.8.0 §7.3, B4): el interruptor de
 * la empresa (apagado de fábrica, lo enciende un admin) y cómo se muestra el motivo en el hilo.
 */

export type AjustesReconexion = Schema<"AjustesReconexion">;

const M = "/api/v1/mensajeria";

export const leerAjustesReconexion = (signal?: AbortSignal) => resolver(api().GET(`${M}/asistente/reconexion`, { signal }));
export const guardarAjustesReconexion = (cuerpo: Schema<"EntradaAjustesReconexion">, clave: string) =>
    resolver(api().POST(`${M}/asistente/reconexion/actualizar`, { params: { header: cabeceraIdem(clave) }, body: cuerpo }));
