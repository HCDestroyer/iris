// Revisión de seguridad de #136: al cerrar sesión no queda nada de las notificaciones en el teléfono
// (líneas de los grupos, respuestas hechas, pendientes y lo visible), quitar los dispositivos nunca
// cuelga la salida (tope de 3 s) y la llave de dedupe no lleva el texto escrito.
import { ejecutarRespuesta, leerPendientes, type DepsAccion } from "../src/push/acciones";
import type { ApiPush } from "../src/push/api";
import { CLAVE_GRUPOS, CLAVE_HECHAS, CLAVE_PENDIENTES, cerrarPushDeSesion, guardarLineasDeGrupo, lineasDeGrupo, olvidarPushLocal } from "../src/push/locales";
import { leerMensajePush, type MensajePush } from "../src/push/mensaje";
import { clavesMemoria } from "./preparar";
import { dataChat } from "./push-falso";

function mensaje(extra: Record<string, string> = {}): MensajePush {
    const m = leerMensajePush(dataChat(extra));
    if (!m) {
        throw new Error("mensaje inválido");
    }
    return m;
}

const sinRed = () => Promise.reject(Object.assign(new Error("Sin conexión"), { estado: 0 }));

function apiSinRed(): ApiPush {
    return {
        registrarDispositivo: sinRed,
        quitarDispositivo: sinRed,
        leerPreferencias: sinRed,
        guardarPreferencias: sinRed,
        responderChat: sinRed,
        leerChat: sinRed,
        notaBandeja: sinRed,
        leerBandeja: sinRed,
    };
}

function deps(extra: Partial<DepsAccion> = {}): DepsAccion {
    return { api: apiSinRed(), claves: clavesMemoria(), generarId: () => "0193-id", usuarioId: 5, quitarGrupo: async () => undefined, ...extra };
}

describe("cerrar sesión borra lo de las notificaciones", () => {
    it("líneas de los grupos, respuestas hechas y pendientes (sin conservar)", async () => {
        const claves = clavesMemoria();
        guardarLineasDeGrupo(claves, "chat-12", ["Carlos Ruiz: ya llegué a la casa"]);
        guardarLineasDeGrupo(claves, "mencion-55", ["Ana López te mencionó: secreto"]);
        const d = deps({ claves });
        await ejecutarRespuesta({ accion: "responder", texto: "texto privado", mensaje: mensaje() }, d);
        expect(leerPendientes(claves)).toHaveLength(1);
        expect(claves.leer(CLAVE_HECHAS)).not.toBeNull();

        olvidarPushLocal(claves, 5, false);
        expect(lineasDeGrupo(claves, "chat-12")).toEqual([]);
        expect(lineasDeGrupo(claves, "mencion-55")).toEqual([]);
        expect(claves.leer(CLAVE_GRUPOS)).toBeNull();
        expect(claves.leer(CLAVE_HECHAS)).toBeNull();
        expect(claves.leer(CLAVE_PENDIENTES)).toBeNull();
        expect(claves.datos.size).toBe(0);
    });

    it("conservando los pendientes, quedan solo los de esta persona", async () => {
        const claves = clavesMemoria();
        await ejecutarRespuesta({ accion: "leido", mensaje: mensaje() }, deps({ claves }));
        // Uno de otra persona que quedó de antes en el teléfono.
        claves.escribir(CLAVE_PENDIENTES, JSON.stringify([...leerPendientes(claves), { tipo: "leer_chat", usuarioId: 6, empresa: null, conversacionId: 3, hasta: "x" }]));
        olvidarPushLocal(claves, 5, true);
        expect(leerPendientes(claves).map((p) => p.usuarioId)).toEqual([5]);
        olvidarPushLocal(claves, 7, true);
        expect(claves.leer(CLAVE_PENDIENTES)).toBeNull();
    });

    it("quita las notificaciones visibles y no espera más de 3 s a la API (un error tampoco detiene la salida)", async () => {
        jest.useFakeTimers();
        try {
            const claves = clavesMemoria();
            guardarLineasDeGrupo(claves, "chat-12", ["texto"]);
            let visibles = 2;
            let senal: AbortSignal | undefined;
            const colgado = cerrarPushDeSesion({
                servicio: {
                    quitarTodas: async () => {
                        visibles = 0;
                    },
                },
                registro: {
                    quitarTodos: (_u, s) => {
                        senal = s;
                        return new Promise<void>(() => undefined);
                    },
                },
                claves,
                usuarioId: 5,
                conservarPendientes: false,
            });
            let listo = false;
            void colgado.then(() => {
                listo = true;
            });
            await jest.advanceTimersByTimeAsync(2_999);
            expect(listo).toBe(false);
            await jest.advanceTimersByTimeAsync(1);
            expect(listo).toBe(true);
            expect(visibles).toBe(0);
            expect(claves.datos.size).toBe(0);
            if (typeof AbortSignal.timeout === "function") {
                expect(senal).toBeDefined();
            }
        } finally {
            jest.useRealTimers();
        }
        await expect(
            cerrarPushDeSesion({
                servicio: { quitarTodas: () => Promise.reject(new Error("x")) },
                registro: { quitarTodos: () => Promise.reject(new Error("y")) },
                claves: clavesMemoria(),
                usuarioId: 5,
                conservarPendientes: false,
            }),
        ).resolves.toBeUndefined();
    });
});

describe("dedupe de respuestas", () => {
    it("la llave no lleva el texto y la misma acción del mismo aviso sale una vez", async () => {
        const claves = clavesMemoria();
        const escritos: string[] = [];
        const d = deps({ claves, escribirChat: async (c, t) => void escritos.push(`${c}:${t}`) });
        await ejecutarRespuesta({ accion: "responder", texto: "mi texto privado", mensaje: mensaje() }, d);
        await ejecutarRespuesta({ accion: "responder", texto: "otro texto", mensaje: mensaje() }, d);
        expect(escritos).toEqual(["12:mi texto privado"]);
        expect(claves.leer(CLAVE_HECHAS)).toBe(JSON.stringify(["8812:responder"]));
    });

    it("la de otra persona no se marca: después, la de quien tiene la sesión sí sale", async () => {
        const claves = clavesMemoria();
        const escritos: string[] = [];
        await ejecutarRespuesta({ accion: "responder", texto: "x", mensaje: mensaje() }, deps({ claves, usuarioId: 6, escribirChat: async () => void escritos.push("ajeno") }));
        expect(claves.leer(CLAVE_HECHAS)).toBeNull();
        await ejecutarRespuesta({ accion: "responder", texto: "voy", mensaje: mensaje() }, deps({ claves, escribirChat: async (c, t) => void escritos.push(`${c}:${t}`) }));
        expect(escritos).toEqual(["12:voy"]);
    });
});
