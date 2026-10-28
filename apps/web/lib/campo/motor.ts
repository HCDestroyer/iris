"use client";
// Motor de sincronización del teléfono (uno por usuario y empresa): IndexedDB con Dexie y la API
// por el cliente openapi-fetch del panel (cookie + X-CSRF-Token + X-Iris-Empresa fija del motor).
import { crearSubidorApi, crearTransporteApi, MotorSync, type EstadoSync } from "@iris/sync";
import { AlmacenDexie } from "@iris/sync/dexie";
import { useSyncExternalStore } from "react";
import { crearClienteIris } from "@/lib/api/cliente";

export interface ClaveMotor {
    usuarioId: number;
    /** `null` = sin conector (empresa_id null). */
    empresaId: number | null;
}

export const nombreBase = ({ usuarioId, empresaId }: ClaveMotor) => `iris-campo-${usuarioId}-${empresaId ?? 0}`;

export async function abrirMotor(clave: ClaveMotor): Promise<MotorSync> {
    // La empresa va fija en el cliente: la cola de una empresa nunca sale con la cabecera de otra.
    const cliente = crearClienteIris({ leerEmpresa: () => clave.empresaId ?? undefined, alProblemaEmpresa: () => undefined });
    return MotorSync.abrir({
        almacen: new AlmacenDexie({ nombre: nombreBase(clave) }),
        transporte: crearTransporteApi(cliente),
        subidor: crearSubidorApi(cliente),
        plataforma: "pwa",
    });
}

/** Estado del motor para React (`useSyncExternalStore`). */
export function useEstadoMotor(motor: MotorSync): EstadoSync {
    return useSyncExternalStore(motor.suscribir, motor.obtenerEstado, motor.obtenerEstado);
}
