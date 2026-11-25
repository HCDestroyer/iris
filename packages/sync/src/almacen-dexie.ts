// Adaptador de `Almacen` sobre IndexedDB con Dexie, para la PWA (`@iris/sync/dexie`). Es el
// único archivo del paquete que toca IndexedDB; el núcleo no lo importa.
import Dexie, { type DexieOptions, type Table } from "dexie";
import type { Almacen, CambiosTabla, ContenidoAlmacen, LoteEscritura } from "./almacen";
import { META_INICIAL, type AdjuntoLocal, type MetaSync, type OperacionLocal, type OrdenLocal } from "./modelo";
import type { AlmacenPosiciones, CambiosPosiciones, PosicionLocal } from "./posiciones";
import { META_CHAT_INICIAL, type AlmacenChat, type ContenidoChat, type ConversacionChat, type LoteChat, type MensajeChat, type MetaChat, type SalidaChat } from "./chat/tipos";

interface FilaMetaChat {
    clave: keyof MetaChat;
    valor: unknown;
}

interface FilaMeta {
    clave: keyof MetaSync;
    valor: unknown;
}

interface FilaBytes {
    id: string;
    datos: Uint8Array;
}

/**
 * Operaciones cuyos `datos` llevan datos personales del titular (número de documento, teléfono,
 * correo, NIT/CUI y las fotos del documento; contrato argosisp.md 0.10.0 §3.4.8): en IndexedDB solo
 * se guardan cifrados con AES-GCM y una llave WebCrypto no exportable (`llaves`). Se borran al quedar
 * la operación aplicada o duplicada (el motor la quita de la cola) y con `olvidarSensibles` al
 * cerrar la sesión.
 */
export const TIPOS_CIFRADOS: readonly string[] = [
    "orden.registrar_identidad",
    "orden.cambiar_titular",
    "orden.completar_datos",
    "orden.cambiar_plan",
    // La clave Wi-Fi inicial de «Activar servicio» (0.9.0 §12.1, «Secretos»).
    "orden.activar_servicio",
    // La firma del contrato regenerado (el trazo es dato sensible, como la firma de la orden).
    "orden.firmar_contrato",
];

interface DatosCifrados {
    iv: Uint8Array<ArrayBuffer>;
    datos: ArrayBuffer;
}

/** Fila de `operaciones`: la operación tal cual o, si es sensible, sin `datos` y con `datosCifrados`. */
type FilaOperacion = OperacionLocal | (Omit<OperacionLocal, "datos"> & { datos?: undefined; datosCifrados: DatosCifrados });

interface FilaLlave {
    id: string;
    llave: CryptoKey;
}

const LLAVE_OPERACIONES = "operaciones";

/** Lo sensible de la cola vive como mucho 7 días en el teléfono (como el depósito del servidor). */
export const VIGENCIA_SENSIBLES_MS = 7 * 24 * 60 * 60 * 1000;

/** Prefijo de las bases de la app de campo (`iris-campo-<usuario>-<empresa>`). */
export const PREFIJO_BASES_CAMPO = "iris-campo-";

class BaseSync extends Dexie {
    operaciones!: Table<FilaOperacion, string>;
    ordenes!: Table<OrdenLocal, number>;
    adjuntos!: Table<AdjuntoLocal, string>;
    bytes!: Table<FilaBytes, string>;
    meta!: Table<FilaMeta, string>;
    posiciones!: Table<PosicionLocal, number>;
    chatSalida!: Table<SalidaChat, string>;
    chatConversaciones!: Table<ConversacionChat, number>;
    chatMensajes!: Table<MensajeChat, string>;
    chatBytes!: Table<FilaBytes, string>;
    chatMeta!: Table<FilaMetaChat, string>;
    llaves!: Table<FilaLlave, string>;

    constructor(nombre: string, opciones?: DexieOptions) {
        super(nombre, opciones);
        // Un cambio de forma = nueva `version(n)` con su `upgrade`.
        this.version(1).stores({
            operaciones: "id, secuencia, ordenId",
            ordenes: "id",
            adjuntos: "id, ordenId",
            bytes: "id",
            meta: "clave",
        });
        // Versión 2: ids de orden numéricos (los del OpenAPI). La 1 solo existió en desarrollo,
        // con ids de texto provisionales: lo del servidor se descarta y se baja el paquete otra vez.
        this.version(2)
            .stores({
                operaciones: "id, secuencia, ordenId",
                ordenes: "id",
                adjuntos: "id, ordenId",
                bytes: "id",
                meta: "clave",
            })
            .upgrade(async (tx) => {
                await tx.table("ordenes").clear();
                await tx.table("meta").bulkDelete(["cursor", "fecha", "existencias", "ruta"]);
            });
        // Versión 3: cola de la ubicación en vivo (clave = hora del punto en ms).
        this.version(3).stores({
            operaciones: "id, secuencia, ordenId",
            ordenes: "id",
            adjuntos: "id, ordenId",
            bytes: "id",
            meta: "clave",
            posiciones: "ms",
        });
        // Versión 4: chat interno (cola de salida con sus fotos y caché de conversaciones y mensajes).
        this.version(4).stores({
            operaciones: "id, secuencia, ordenId",
            ordenes: "id",
            adjuntos: "id, ordenId",
            bytes: "id",
            meta: "clave",
            posiciones: "ms",
            chatSalida: "id, secuencia",
            chatConversaciones: "id",
            chatMensajes: "id, conversacion_id",
            chatBytes: "id",
            chatMeta: "clave",
        });
        // Versión 5: llave no exportable de los datos sensibles de la cola (`TIPOS_CIFRADOS`).
        this.version(5)
            .stores({
            operaciones: "id, secuencia, ordenId",
            ordenes: "id",
            adjuntos: "id, ordenId",
            bytes: "id",
            meta: "clave",
            posiciones: "ms",
            chatSalida: "id, secuencia",
            chatConversaciones: "id",
            chatMensajes: "id, conversacion_id",
            chatBytes: "id",
            chatMeta: "clave",
            llaves: "id",
        })
            .upgrade(async (tx) => {
                // Lo que una versión anterior guardó en claro de estas operaciones se borra (no se
                // cifra después: ya estuvo en claro en el disco).
                await tx
                    .table("operaciones")
                    .filter((f: FilaOperacion) => TIPOS_CIFRADOS.includes(f.tipo) && !esCifrada(f))
                    .delete();
            });
    }
}

function esCifrada(fila: FilaOperacion): fila is Omit<OperacionLocal, "datos"> & { datos?: undefined; datosCifrados: DatosCifrados } {
    return "datosCifrados" in fila && fila.datosCifrados !== undefined;
}

const codificador = new TextEncoder();
const decodificador = new TextDecoder();

async function aplicar<T, K extends string | number>(tabla: Table<T, K>, cambios: CambiosTabla<T, K> | undefined): Promise<void> {
    if (cambios?.borrar?.length) {
        await tabla.bulkDelete(cambios.borrar);
    }
    if (cambios?.poner?.length) {
        await tabla.bulkPut(cambios.poner);
    }
}

export interface OpcionesAlmacenDexie {
    /** Nombre de la base IndexedDB; una por usuario y empresa (`iris-campo-<usuario>-<empresa>`). */
    nombre: string;
    /** Hora actual en ms (pruebas). `Date.now` por defecto. */
    ahora?: () => number;
    /** `indexedDB` e `IDBKeyRange` propios (p. ej. `fake-indexeddb` en pruebas). */
    dexie?: DexieOptions;
}

export class AlmacenDexie implements Almacen, AlmacenPosiciones, AlmacenChat {
    private readonly db: BaseSync;
    private readonly ahora: () => number;
    private llave: Promise<CryptoKey> | null = null;
    private borradas = 0;

    constructor(opciones: OpcionesAlmacenDexie) {
        this.db = new BaseSync(opciones.nombre, opciones.dexie);
        this.ahora = opciones.ahora ?? Date.now;
    }

    /**
     * Operaciones sensibles que `cargar` borró por tener más de 7 días (también rechazadas): la app
     * avisa «se borraron por seguridad, vuelve a tomar los datos» (`TEXTO_SENSIBLES_BORRADAS`).
     */
    borradasPorSeguridad(): number {
        return this.borradas;
    }

    /**
     * Olvida lo sensible de todas las bases de campo de este navegador (sesión vencida o revocada,
     * arranque sin sesión) o de todas menos las de `excepto` (prefijo `iris-campo-<usuario>-`: entró
     * otra persona). Devuelve cuántas bases tocó.
     */
    static async olvidarSensiblesDeTodas(opciones: { dexie?: DexieOptions; excepto?: string } = {}): Promise<number> {
        const fabrica = (opciones.dexie?.indexedDB ?? globalThis.indexedDB) as (IDBFactory & { databases?: () => Promise<{ name?: string }[]> }) | undefined;
        if (!fabrica?.databases) {
            return 0;
        }
        const nombres = (await fabrica.databases())
            .map((d) => d.name ?? "")
            .filter((n) => n.startsWith(PREFIJO_BASES_CAMPO) && !(opciones.excepto && n.startsWith(opciones.excepto)));
        let n = 0;
        for (const nombre of nombres) {
            const almacen = new AlmacenDexie({ nombre, ...(opciones.dexie ? { dexie: opciones.dexie } : {}) });
            try {
                await almacen.olvidarSensibles();
                n++;
            } finally {
                almacen.cerrar();
            }
        }
        return n;
    }

    /** ¿Existe ya esa base? (para no crear una vacía al limpiar). */
    static existe(nombre: string): Promise<boolean> {
        return Dexie.exists(nombre);
    }

    /** La llave de la cola (no exportable); la crea la primera vez. */
    private obtenerLlave(): Promise<CryptoKey> {
        this.llave ??= (async () => {
            const fila = await this.db.llaves.get(LLAVE_OPERACIONES);
            if (fila) {
                return fila.llave;
            }
            const llave = await crypto.subtle.generateKey({ name: "AES-GCM", length: 256 }, false, ["encrypt", "decrypt"]);
            await this.db.llaves.put({ id: LLAVE_OPERACIONES, llave });
            return llave;
        })();
        return this.llave;
    }

    /** Cifra los `datos` de las operaciones sensibles (antes de abrir la transacción de Dexie). */
    private async cifrar(ops: OperacionLocal[]): Promise<FilaOperacion[]> {
        if (!ops.some((op) => TIPOS_CIFRADOS.includes(op.tipo))) {
            return ops;
        }
        const llave = await this.obtenerLlave();
        return Promise.all(
            ops.map(async (op): Promise<FilaOperacion> => {
                if (!TIPOS_CIFRADOS.includes(op.tipo)) {
                    return op;
                }
                const iv = crypto.getRandomValues(new Uint8Array(12));
                const datos = await crypto.subtle.encrypt({ name: "AES-GCM", iv, additionalData: codificador.encode(op.id) }, llave,
                    codificador.encode(JSON.stringify(op.datos)));
                const fila: Omit<OperacionLocal, "datos"> & { datos?: unknown } = { ...op };
                delete fila.datos;
                return { ...(fila as Omit<OperacionLocal, "datos">), datosCifrados: { iv, datos } };
            }),
        );
    }

    /** Descifra lo cifrado; lo que ya no se puede abrir (llave olvidada) se descarta. */
    private async descifrar(filas: FilaOperacion[]): Promise<{ operaciones: OperacionLocal[]; ilegibles: string[] }> {
        if (!filas.some(esCifrada)) {
            return { operaciones: filas as OperacionLocal[], ilegibles: [] };
        }
        const llave = await this.obtenerLlave();
        const operaciones: OperacionLocal[] = [];
        const ilegibles: string[] = [];
        for (const fila of filas) {
            if (!esCifrada(fila)) {
                operaciones.push(fila);
                continue;
            }
            try {
                const plano = await crypto.subtle.decrypt(
                    { name: "AES-GCM", iv: fila.datosCifrados.iv, additionalData: codificador.encode(fila.id) },
                    llave,
                    fila.datosCifrados.datos,
                );
                const resto: Partial<typeof fila> = { ...fila };
                delete resto.datosCifrados;
                operaciones.push({ ...resto, datos: JSON.parse(decodificador.decode(plano)) } as OperacionLocal);
            } catch {
                ilegibles.push(fila.id);
            }
        }
        return { operaciones, ilegibles };
    }

    /**
     * Al cerrar la sesión conservando la cola: borra las operaciones sensibles y su llave (los datos
     * del titular no se quedan en el teléfono sin una sesión abierta).
     */
    async olvidarSensibles(): Promise<void> {
        await this.db.transaction("rw", [this.db.operaciones, this.db.llaves], async () => {
            const ids = (await this.db.operaciones.toArray()).filter((f) => TIPOS_CIFRADOS.includes(f.tipo)).map((f) => f.id);
            await this.db.operaciones.bulkDelete(ids);
            await this.db.llaves.delete(LLAVE_OPERACIONES);
        });
        this.llave = null;
    }

    async cargar(): Promise<ContenidoAlmacen> {
        const contenido = await this.db.transaction("r", [this.db.operaciones, this.db.ordenes, this.db.adjuntos, this.db.meta], async () => {
            const [operaciones, ordenes, adjuntos, filas] = await Promise.all([
                this.db.operaciones.orderBy("secuencia").toArray(),
                this.db.ordenes.toArray(),
                this.db.adjuntos.toArray(),
                this.db.meta.toArray(),
            ]);
            const meta: Record<string, unknown> = { ...META_INICIAL };
            for (const fila of filas) {
                meta[fila.clave] = fila.valor;
            }
            return { operaciones, ordenes, adjuntos, meta: meta as unknown as MetaSync };
        });
        const { operaciones: legibles, ilegibles } = await this.descifrar(contenido.operaciones);
        // Más de 7 días en el teléfono (aunque esté rechazada): se borra por seguridad.
        const limite = this.ahora() - VIGENCIA_SENSIBLES_MS;
        const vencidas = legibles.filter((op) => TIPOS_CIFRADOS.includes(op.tipo) && Date.parse(op.ocurridoEn) < limite).map((op) => op.id);
        const borrar = [...ilegibles, ...vencidas];
        if (borrar.length > 0) {
            await this.db.operaciones.bulkDelete(borrar);
        }
        this.borradas = borrar.length;
        const operaciones = vencidas.length > 0 ? legibles.filter((op) => !vencidas.includes(op.id)) : legibles;
        return { ...contenido, operaciones };
    }

    async escribir(lote: LoteEscritura): Promise<void> {
        // WebCrypto fuera de la transacción (una promesa ajena a IndexedDB la cerraría).
        const operaciones: CambiosTabla<FilaOperacion> | undefined = lote.operaciones && {
            ...lote.operaciones,
            ...(lote.operaciones.poner ? { poner: await this.cifrar(lote.operaciones.poner) } : {}),
        };
        const tablas = [this.db.operaciones, this.db.ordenes, this.db.adjuntos, this.db.bytes, this.db.meta];
        await this.db.transaction("rw", tablas, async () => {
            await aplicar(this.db.operaciones, operaciones);
            await aplicar(this.db.ordenes, lote.ordenes);
            await aplicar(this.db.adjuntos, lote.adjuntos);
            await aplicar(this.db.bytes, lote.bytes);
            if (lote.meta) {
                const filas = Object.entries(lote.meta).map(([clave, valor]) => ({ clave: clave as keyof MetaSync, valor }));
                await this.db.meta.bulkPut(filas);
            }
        });
    }

    async leerBytes(id: string): Promise<Uint8Array | null> {
        const fila = await this.db.bytes.get(id);
        return fila ? fila.datos : null;
    }

    async cargarPosiciones(): Promise<PosicionLocal[]> {
        return this.db.posiciones.orderBy("ms").toArray();
    }

    async escribirPosiciones(cambios: CambiosPosiciones): Promise<void> {
        await this.db.transaction("rw", this.db.posiciones, async () => {
            await aplicar(this.db.posiciones, cambios);
        });
    }

    async cargarChat(): Promise<ContenidoChat> {
        const t = [this.db.chatSalida, this.db.chatConversaciones, this.db.chatMensajes, this.db.chatMeta];
        return this.db.transaction("r", t, async () => {
            const [salida, conversaciones, mensajes, filas] = await Promise.all([
                this.db.chatSalida.orderBy("secuencia").toArray(),
                this.db.chatConversaciones.toArray(),
                this.db.chatMensajes.toArray(),
                this.db.chatMeta.toArray(),
            ]);
            const meta: Record<string, unknown> = { ...structuredClone(META_CHAT_INICIAL) };
            for (const fila of filas) {
                meta[fila.clave] = fila.valor;
            }
            return { salida, conversaciones, mensajes, meta: meta as unknown as MetaChat };
        });
    }

    async escribirChat(lote: LoteChat): Promise<void> {
        const t = [this.db.chatSalida, this.db.chatConversaciones, this.db.chatMensajes, this.db.chatBytes, this.db.chatMeta];
        await this.db.transaction("rw", t, async () => {
            await aplicar(this.db.chatSalida, lote.salida);
            await aplicar(this.db.chatConversaciones, lote.conversaciones);
            await aplicar(this.db.chatMensajes, lote.mensajes);
            await aplicar(this.db.chatBytes, lote.bytes);
            if (lote.meta) {
                await this.db.chatMeta.bulkPut(Object.entries(lote.meta).map(([clave, valor]) => ({ clave: clave as keyof MetaChat, valor })));
            }
        });
    }

    async leerBytesChat(id: string): Promise<Uint8Array | null> {
        const fila = await this.db.chatBytes.get(id);
        return fila ? fila.datos : null;
    }

    /** Borra la caché del chat (conversaciones, mensajes y cursor); la cola de salida queda. */
    async borrarCacheChat(): Promise<void> {
        await this.db.transaction("rw", [this.db.chatConversaciones, this.db.chatMensajes, this.db.chatMeta], async () => {
            await this.db.chatConversaciones.clear();
            await this.db.chatMensajes.clear();
            await this.db.chatMeta.bulkDelete(["cursor", "cargadas"]);
        });
    }

    /** Cierra la conexión (al salir de la sesión). */
    cerrar(): void {
        this.db.close();
    }

    /** Borra la base entera (al cerrar sesión en un teléfono compartido). */
    async borrar(): Promise<void> {
        await this.db.delete();
    }
}
