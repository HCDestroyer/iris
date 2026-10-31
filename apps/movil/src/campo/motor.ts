// Motor de sincronización del teléfono (uno por usuario y empresa): `@iris/sync` sobre SQLite
// (`expo-sqlite`) y la API por el cliente openapi-fetch con la sesión del teléfono. Hermes no trae
// `crypto.subtle` ni `crypto.getRandomValues`: el SHA-256 de los adjuntos y el azar del UUID v7
// vienen de `expo-crypto`.
import {
    CODIGO_SESION_AJENA,
    ColaPosiciones,
    crearGeneradorUuid7,
    crearInventarioApi,
    crearSubidorApi,
    crearTransporteApi,
    crearTransportePosicionesApi,
    ErrorTransporte,
    MotorSync,
    type IdentidadApp,
    type InventarioCampo,
    type Transporte,
} from "@iris/sync";
import { AlmacenSqlite } from "@iris/sync/sqlite";
import * as Crypto from "expo-crypto";
import { deleteDatabaseAsync, openDatabaseAsync } from "expo-sqlite";
import { Platform } from "react-native";
import { VERSION_APP } from "../api/version";
import { cliente, verificarDueno } from "../sesion/sesion";

export interface ClaveMotor {
    usuarioId: number;
    /** `null` = sin conector (empresa_id null). */
    empresaId: number | null;
}

/** Archivo SQLite del usuario y la empresa (`iris-campo-<usuario>-<empresa>.db`). */
export const nombreBase = ({ usuarioId, empresaId }: ClaveMotor) => `iris-campo-${usuarioId}-${empresaId ?? 0}.db`;

export interface MotorAbierto {
    motor: MotorSync;
    posiciones: ColaPosiciones;
    almacen: AlmacenSqlite;
    nombre: string;
    /** «Por recibir» y «Mi bodega» (solo con señal, tras comprobar el dueño de la sesión). */
    inventario: InventarioCampo;
}

/** El transporte de la API, avisando cada respuesta correcta de paquete o cambios. */
export function conConfirmacion(base: Transporte, alConfirmar: () => void): Transporte {
    return {
        enviarLote: (p) => base.enviarLote(p),
        async obtenerPaquete(fecha) {
            const r = await base.obtenerPaquete(fecha);
            alConfirmar();
            return r;
        },
        async obtenerCambios(cursor) {
            const r = await base.obtenerCambios(cursor);
            alConfirmar();
            return r;
        },
    };
}

export async function sha256Hex(datos: Uint8Array): Promise<string> {
    const resumen = await Crypto.digest(Crypto.CryptoDigestAlgorithm.SHA256, datos as Uint8Array<ArrayBuffer>);
    return Array.from(new Uint8Array(resumen), (x) => x.toString(16).padStart(2, "0")).join("");
}

/**
 * `alConfirmarServidor` se llama cada vez que la API responde el paquete o los cambios: para el GPS
 * es la única confirmación que abre o renueva su ventana (nunca un lote ni lo guardado).
 */
export async function abrirMotor(clave: ClaveMotor, alSesionAjena: () => void, alConfirmarServidor: () => void = () => undefined): Promise<MotorAbierto> {
    const nombre = nombreBase(clave);
    const almacen = new AlmacenSqlite(await openDatabaseAsync(nombre));
    // La empresa va fija en el cliente: la cola de una empresa nunca sale con la cabecera de otra.
    const api = cliente(clave.empresaId);
    const verificarIdentidad = async () => {
        try {
            await verificarDueno(clave.usuarioId, clave.empresaId);
        } catch (e) {
            if (e instanceof ErrorTransporte && e.codigo === CODIGO_SESION_AJENA) {
                alSesionAjena();
            }
            throw e;
        }
    };
    // X-Iris-Pendientes en cada envío (la cola local); X-Iris-App ya lo pone el cliente.
    let abierto: MotorSync | null = null;
    const app: IdentidadApp = { version: VERSION_APP, plataforma: Platform.OS, pendientes: () => abierto?.obtenerEstado().pendientes ?? null };
    const motor = await MotorSync.abrir({
        almacen,
        transporte: conConfirmacion(crearTransporteApi(api, app), alConfirmarServidor),
        subidor: crearSubidorApi(api, app),
        plataforma: Platform.OS,
        sha256: sha256Hex,
        generarId: crearGeneradorUuid7(Date.now, (bytes) => {
            Crypto.getRandomValues(bytes);
        }),
        verificarIdentidad,
    });
    abierto = motor;
    const posiciones = await ColaPosiciones.abrir({
        almacen,
        transporte: crearTransportePosicionesApi(api, app),
        dispositivoId: motor.dispositivoId,
        verificarIdentidad,
    });
    const lecturas = crearInventarioApi(api, app);
    const inventario: InventarioCampo = {
        porRecibir: async () => {
            await verificarIdentidad();
            return lecturas.porRecibir();
        },
        miBodega: async () => {
            await verificarIdentidad();
            return lecturas.miBodega();
        },
    };
    return { motor, posiciones, almacen, nombre, inventario };
}

/** Cierra y borra la base del teléfono (cerrar sesión en un teléfono compartido). */
export async function borrarBase(abierto: MotorAbierto): Promise<void> {
    abierto.motor.detener();
    abierto.posiciones.detener();
    await abierto.almacen.cerrar();
    await deleteDatabaseAsync(abierto.nombre);
}
