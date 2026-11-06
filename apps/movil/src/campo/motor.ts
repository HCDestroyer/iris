// Motor de sincronización del teléfono (uno por usuario y empresa): `@iris/sync` sobre SQLite
// (`expo-sqlite`) y la API por el cliente openapi-fetch con la sesión del teléfono. Hermes no trae
// `crypto.subtle` ni `crypto.getRandomValues`: el SHA-256 de los adjuntos y el azar del UUID v7
// vienen de `expo-crypto`.
import {
    CODIGO_SESION_AJENA,
    ColaPosiciones,
    crearGeneradorUuid7,
    crearHistorialApi,
    crearInventarioApi,
    crearLibresApi,
    crearSubidorApi,
    crearTransporteApi,
    crearTransportePosicionesApi,
    ErrorTransporte,
    MotorSync,
    type IdentidadApp,
    type InventarioCampo,
    type LibresCampo,
    type OrdenCampo,
    type OrdenHistorial,
    type Transporte,
} from "@iris/sync";
import { crearTransporteChatApi, MotorChat } from "@iris/sync/chat";
import { AlmacenSqlite } from "@iris/sync/sqlite";
import { File, Paths } from "expo-file-system";
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
    /** Agenda de «Mañana» y «Semana» (`GET /campo/paquete?fecha=hasta`, solo con señal; ver `campo/agenda.tsx`). */
    agenda: (hasta: string) => Promise<OrdenCampo[]>;
    /**
     * Chat interno, en la misma base. Tiempo real por sondeo de `/chat/cambios` (cada 12 s con la
     * app al frente): el `fetch` de React Native 0.88 (whatwg-fetch sobre XHR) resuelve al terminar
     * la respuesta y no da `response.body` como flujo, así que no puede leer `GET /chat/eventos`.
     */
    chat: MotorChat;
    /** «Historial del cliente» del menú de la orden (`GET /ordenes?clienteId=`, solo con señal). */
    historial: (clienteId: number) => Promise<OrdenHistorial[]>;
    /** Órdenes libres y tomar una (solo con señal; nunca por la cola). */
    libres: LibresCampo;
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
    const transporte = crearTransporteApi(api, app);
    const motor = await MotorSync.abrir({
        almacen,
        transporte: conConfirmacion(transporte, alConfirmarServidor),
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
    // La agenda va por el transporte sin la confirmación del GPS: no es el paquete del día.
    const agenda = async (hasta: string) => {
        await verificarIdentidad();
        return (await transporte.obtenerPaquete(hasta)).datos.ordenes ?? [];
    };
    const leerHistorial = crearHistorialApi(api, app);
    const historial = async (clienteId: number) => {
        await verificarIdentidad();
        return leerHistorial(clienteId);
    };
    const apiLibres = crearLibresApi(api, app);
    const libres: LibresCampo = {
        listar: async (cursor) => {
            await verificarIdentidad();
            return apiLibres.listar(cursor);
        },
        tomar: async (id) => {
            await verificarIdentidad();
            return apiLibres.tomar(id);
        },
    };
    const chat = await MotorChat.abrir({
        almacen,
        transporte: crearTransporteChatApi(api, app),
        subidor: crearSubidorApi(api, app),
        usuarioId: clave.usuarioId,
        sha256: sha256Hex,
        generarId: crearGeneradorUuid7(Date.now, (bytes) => {
            Crypto.getRandomValues(bytes);
        }),
        verificarIdentidad,
        abrirFlujo: null,
    });
    return { motor, posiciones, almacen, nombre, inventario, agenda, libres, historial, chat };
}

/**
 * «¿No eres tú?»: borra la caché del chat (no la cola) de las bases de ese usuario que existan
 * (`<documentos>/SQLite/<nombre>`, donde las crea `expo-sqlite`).
 */
export async function borrarCachesChat(usuarioId: number, empresas: readonly (number | null)[]): Promise<void> {
    for (const empresaId of empresas) {
        const nombre = nombreBase({ usuarioId, empresaId });
        if (!new File(Paths.document, "SQLite", nombre).exists) {
            continue;
        }
        const almacen = new AlmacenSqlite(await openDatabaseAsync(nombre));
        try {
            await almacen.borrarCacheChat();
        } finally {
            await almacen.cerrar();
        }
    }
}

/** Cierra y borra la base del teléfono (cerrar sesión en un teléfono compartido). */
export async function borrarBase(abierto: MotorAbierto): Promise<void> {
    abierto.chat.detener();
    abierto.motor.detener();
    abierto.posiciones.detener();
    await abierto.almacen.cerrar();
    await deleteDatabaseAsync(abierto.nombre);
}
