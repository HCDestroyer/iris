"use client";
// Motor de sincronización del teléfono (uno por usuario y empresa): IndexedDB con Dexie y la API
// por el cliente openapi-fetch del panel (cookie + X-CSRF-Token + X-Iris-Empresa fija del motor).
import { CODIGO_SESION_AJENA, crearSubidorApi, crearTransporteApi, ErrorTransporte, MotorSync, type EstadoSync } from "@iris/sync";
import { AlmacenDexie } from "@iris/sync/dexie";
import { useSyncExternalStore } from "react";
import { crearClienteIris } from "@/lib/api/cliente";
import { verificarDueno } from "./sesion";

export interface ClaveMotor {
    usuarioId: number;
    /** `null` = sin conector (empresa_id null). */
    empresaId: number | null;
}

export const nombreBase = ({ usuarioId, empresaId }: ClaveMotor) => `iris-campo-${usuarioId}-${empresaId ?? 0}`;

export interface MotorAbierto {
    motor: MotorSync;
    almacen: AlmacenDexie;
}

/**
 * Abre el motor de ese usuario y empresa. Antes de cada envío o bajada comprueba con GET /yo que
 * la sesión del navegador sigue siendo de ese usuario (y empresa); si entró otra persona, llama
 * `alSesionAjena` y no envía nada (la cola queda intacta en su base).
 */
export async function abrirMotor(clave: ClaveMotor, alSesionAjena: () => void): Promise<MotorAbierto> {
    // La empresa va fija en el cliente: la cola de una empresa nunca sale con la cabecera de otra.
    const cliente = crearClienteIris({ leerEmpresa: () => clave.empresaId ?? undefined, alProblemaEmpresa: () => undefined });
    const almacen = new AlmacenDexie({ nombre: nombreBase(clave) });
    const motor = await MotorSync.abrir({
        almacen,
        transporte: crearTransporteApi(cliente),
        subidor: crearSubidorApi(cliente),
        plataforma: "pwa",
        verificarIdentidad: async () => {
            try {
                await verificarDueno(clave.usuarioId, clave.empresaId);
            } catch (e) {
                if (e instanceof ErrorTransporte && e.codigo === CODIGO_SESION_AJENA) {
                    alSesionAjena();
                }
                throw e;
            }
        },
    });
    return { motor, almacen };
}

/** Estado del motor para React (`useSyncExternalStore`). */
export function useEstadoMotor(motor: MotorSync): EstadoSync {
    return useSyncExternalStore(motor.suscribir, motor.obtenerEstado, motor.obtenerEstado);
}
