// Notificaciones push (`contratos/push.md`), sin pantallas: el mensaje de FCM validado, los enlaces
// `iris://` (un id inválido o una ruta ajena nunca navegan), cómo se apilan los mensajes, el registro y
// la quita del teléfono, y las acciones «Responder» / «Marcar como leído» (con y sin señal).
import type { Schema } from "@iris/contratos";
import { ErrorApi } from "../src/api/errores";
import { ejecutarRespuesta, leerPendientes, vaciarPendientes, type DepsAccion } from "../src/push/acciones";
import type { ApiPush } from "../src/push/api";
import { armarNotificacion, destinoDeEnlace, leerMensajePush, seEstaViendo, type MensajePush } from "../src/push/mensaje";
import { DIA_MS, RegistroDispositivo } from "../src/push/registro";
import { enNoMolestar, errorHora, lineaNoMolestar, resumenNotificaciones } from "../src/push/textos";
import { clavesMemoria } from "./preparar";
import { dataChat, preferencias } from "./push-falso";

const TOKEN = "f".repeat(150);

function mensaje(extra: Record<string, string> = {}, datos: Record<string, unknown> = {}): MensajePush {
    const m = leerMensajePush(dataChat(extra, datos));
    if (!m) {
        throw new Error("mensaje inválido");
    }
    return m;
}

describe("mensaje de FCM", () => {
    it("lee un push de chat del contrato", () => {
        const m = mensaje();
        expect(m).toMatchObject({ tipo: "chat", usuarioId: 5, empresaId: null, grupo: "chat-12", acciones: ["responder", "leido"] });
        expect(m.datos).toEqual({ tipo: "chat", conversacionId: 12, mensajeId: "0193a", remitente: { id: 9, nombre: "Carlos Ruiz", iniciales: "CR" } });
    });

    it("descarta lo que no es del contrato: otra versión, otro tipo, enlace ajeno, datos rotos", () => {
        expect(leerMensajePush(dataChat({ v: "2" }))).toBeNull();
        expect(leerMensajePush(dataChat({ tipo: "spam" }))).toBeNull();
        expect(leerMensajePush(dataChat({ enlace: "https://malo.example/robar" }))).toBeNull();
        expect(leerMensajePush(dataChat({ usuarioId: "x" }))).toBeNull();
        expect(leerMensajePush(dataChat({ datos: "{no es json" }))).toBeNull();
        expect(leerMensajePush(dataChat({ empresaId: "-3" }))).toBeNull();
        expect(leerMensajePush(null)).toBeNull();
    });

    it("acepta la empresa del mensaje y quita acciones desconocidas", () => {
        const m = mensaje({ empresaId: "3", acciones: "responder,borrar_todo,leido" });
        expect(m.empresaId).toBe(3);
        expect(m.acciones).toEqual(["responder", "leido"]);
    });
});

describe("enlaces iris://", () => {
    it("chat, orden y bandeja (esta en el panel web)", () => {
        expect(destinoDeEnlace("iris://chat/12")).toEqual({ tipo: "vista", vista: { tipo: "conversacion", id: 12 } });
        expect(destinoDeEnlace("iris://orden/123")).toEqual({ tipo: "vista", vista: { tipo: "orden", id: 123 } });
        expect(destinoDeEnlace("iris://conversaciones/55")).toEqual({ tipo: "panel", ruta: "/conversaciones/55" });
    });

    it.each(["iris://chat/0", "iris://chat/-1", "iris://chat/abc", "iris://chat/12/../../yo", "iris://chat/12?x=1", "iris://orden/99999999999999999999", "iris://yo/1", "https://iris.gt/chat/1", "iris-campo://chat/1", "", "javascript:alert(1)"])(
        "id o ruta inválidos no navegan: %s",
        (enlace) => {
            expect(destinoDeEnlace(enlace)).toBeNull();
        },
    );
});

describe("cómo se ve", () => {
    it("los mensajes de una conversación se apilan en una sola notificación", () => {
        const uno = armarNotificacion(mensaje(), []);
        expect(uno).toMatchObject({ identificador: "chat-12", canal: "chat", titulo: "Cuadrilla Norte", cuerpo: "Carlos Ruiz: ya llegué a la casa", categoria: "iris-leido+responder" });
        const dos = armarNotificacion(mensaje({ cuerpo: "Ana López: Le marco yo, espérame 5 min" }), uno.lineas);
        expect(dos.titulo).toBe("Cuadrilla Norte · 2 mensajes");
        expect(dos.cuerpo).toBe("Carlos Ruiz: ya llegué a la casa\nAna López: Le marco yo, espérame 5 min");
    });

    it("una orden reemplaza a la anterior (no se apila) y lleva «Ver orden» y «Cómo llegar»", () => {
        const orden = leerMensajePush({
            ...dataChat(),
            tipo: "orden",
            canal: "orden",
            titulo: "OT-000123 asignada para hoy",
            cuerpo: "14:00–16:00 · Villa Nueva",
            grupo: "orden-123",
            enlace: "iris://orden/123",
            acciones: "ver_orden,como_llegar",
            datos: JSON.stringify({ ordenId: 123, numero: "OT-000123", cambio: "asignada", hoy: true }),
        });
        expect(orden).not.toBeNull();
        const n = armarNotificacion(orden as MensajePush, ["algo viejo"]);
        expect(n).toMatchObject({ titulo: "OT-000123 asignada para hoy", cuerpo: "14:00–16:00 · Villa Nueva", categoria: "iris-como_llegar+ver_orden", lineas: [] });
    });

    it("con la app al frente no se avisa el chat que se está viendo", () => {
        const m = mensaje();
        expect(seEstaViendo(m, { tipo: "conversacion", id: 12 })).toBe(true);
        expect(seEstaViendo(m, { tipo: "conversacion", id: 13 })).toBe(false);
        expect(seEstaViendo(m, { tipo: "chat" })).toBe(false);
    });
});

function apiFalsa(): ApiPush & { llamadas: string[]; fallar: Set<string> } {
    const llamadas: string[] = [];
    const fallar = new Set<string>();
    const registrar = (n: string) => {
        llamadas.push(n);
        if (fallar.has(n.split(":")[0] ?? "")) {
            throw new ErrorApi({ estado: 0, codigo: "sin_conexion", titulo: "Sin conexión" });
        }
    };
    let id = 40;
    return {
        llamadas,
        fallar,
        async registrarDispositivo(empresa, entrada) {
            registrar(`registrar:${empresa ?? "-"}:${entrada.token.slice(0, 3)}:${entrada.plataforma}:${entrada.appVersion}`);
            id += 1;
            return { id, plataforma: "android", appVersion: entrada.appVersion, tokenFin: entrada.token.slice(-6), empresaId: empresa, registradoEn: "", actualizadoEn: "", ultimoEnvioEn: null } as unknown as Schema<"Dispositivo">;
        },
        async quitarDispositivo(empresa, d) {
            registrar(`quitar:${empresa ?? "-"}:${d}`);
        },
        async leerPreferencias() {
            registrar("leerPreferencias");
            return preferencias();
        },
        async guardarPreferencias(e) {
            registrar(`guardar:${e.version}`);
            return { ...preferencias(), ...e, version: e.version + 1 };
        },
        async responderChat(_e, c, _id, t) {
            registrar(`responderChat:${c}:${t}`);
        },
        async leerChat(_e, c, h) {
            registrar(`leerChat:${c}:${h}`);
        },
        async notaBandeja(_e, c, _id, t, m) {
            registrar(`notaBandeja:${c}:${t}:${m.join(",")}`);
        },
        async leerBandeja(_e, c, h) {
            registrar(`leerBandeja:${c}:${h}`);
        },
    };
}

describe("registro del teléfono", () => {
    it("al entrar registra con el token nativo, una vez al día salvo token nuevo o forzado, y al salir quita los suyos", async () => {
        const api = apiFalsa();
        const claves = clavesMemoria();
        let ahora = 1_000_000;
        const r = new RegistroDispositivo({ api, claves, obtenerToken: async () => TOKEN, appVersion: "0.17.0", generarClave: () => "k", ahora: () => ahora });
        expect(await r.registrar(5, 3, { forzar: true })).toBe(true);
        expect(api.llamadas).toEqual(["registrar:3:fff:android:0.17.0"]);
        await r.registrar(5, 3);
        expect(api.llamadas).toHaveLength(1);
        ahora += DIA_MS;
        await r.registrar(5, 3);
        expect(api.llamadas).toHaveLength(2);
        // FCM rotó el token: siempre se registra.
        await r.registrar(5, 3, { token: "n".repeat(120) });
        expect(api.llamadas.at(-1)).toBe("registrar:3:nnn:android:0.17.0");
        // Otra empresa (al cambiar de empresa).
        await r.registrar(5, null, { forzar: true });
        expect(api.llamadas.at(-1)).toBe("registrar:-:fff:android:0.17.0");
        // El token nunca se guarda en el teléfono.
        expect([...claves.datos.values()].join()).not.toContain(TOKEN);

        await r.quitarTodos(5);
        expect(api.llamadas.filter((l) => l.startsWith("quitar"))).toEqual(["quitar:-:44", "quitar:3:43"]);
        expect(claves.datos.size).toBe(0);
    });

    it("sin token (APK sin Firebase) no registra; quitar sin señal no detiene la salida", async () => {
        const api = apiFalsa();
        const claves = clavesMemoria();
        const sinToken = new RegistroDispositivo({ api, claves, obtenerToken: async () => null, appVersion: "0.17.0", generarClave: () => "k" });
        expect(await sinToken.registrar(5, 3, { forzar: true })).toBe(false);
        expect(api.llamadas).toEqual([]);
        const r = new RegistroDispositivo({ api, claves, obtenerToken: async () => TOKEN, appVersion: "0.17.0", generarClave: () => "k" });
        await r.registrar(5, 3, { forzar: true });
        api.fallar.add("quitar");
        await expect(r.quitarTodos(5)).resolves.toBeUndefined();
        expect(claves.datos.size).toBe(0);
    });
});

describe("acciones de la notificación", () => {
    const deps = (api: ApiPush, extra: Partial<DepsAccion> = {}): DepsAccion & { quitados: string[] } => {
        const quitados: string[] = [];
        return { api, claves: clavesMemoria(), generarId: () => "0193-nuevo", usuarioId: 5, quitarGrupo: async (g) => void quitados.push(g), ...extra, quitados };
    };

    it("«Responder» del chat va por la cola del chat con la app abierta, y quita la notificación", async () => {
        const api = apiFalsa();
        const escritos: string[] = [];
        const d = deps(api, { escribirChat: async (c, t) => void escritos.push(`${c}:${t}`) });
        expect(await ejecutarRespuesta({ accion: "responder", texto: " voy ", mensaje: mensaje() }, d)).toBeNull();
        expect(escritos).toEqual(["12:voy"]);
        expect(api.llamadas).toEqual([]);
        expect(d.quitados).toEqual(["chat-12"]);
        // La misma respuesta otra vez (oyente y tarea): no se manda dos veces.
        await ejecutarRespuesta({ accion: "responder", texto: " voy ", mensaje: mensaje() }, d);
        expect(escritos).toHaveLength(1);
    });

    it("sin la app abierta va a la API; sin señal queda guardada y sale al abrir la app", async () => {
        const api = apiFalsa();
        api.fallar.add("responderChat").add("leerChat");
        const d = deps(api);
        await ejecutarRespuesta({ accion: "responder", texto: "ya voy", mensaje: mensaje() }, d);
        await ejecutarRespuesta({ accion: "leido", mensaje: mensaje() }, d);
        expect(leerPendientes(d.claves).map((p) => p.tipo)).toEqual(["responder_chat", "leer_chat"]);
        api.fallar.clear();
        const escritos: string[] = [];
        await vaciarPendientes({ ...d, escribirChat: async (c, t) => void escritos.push(`${c}:${t}`) });
        expect(escritos).toEqual(["12:ya voy"]);
        expect(api.llamadas.at(-1)).toBe("leerChat:12:0193a");
        expect(leerPendientes(d.claves)).toEqual([]);
    });

    it("mención de la bandeja: «Responder» es una nota interna que menciona a quien escribió; «leído», el leer de la bandeja", async () => {
        const api = apiFalsa();
        const d = deps(api);
        const m = mensaje(
            { tipo: "mencion", canal: "mencion", grupo: "mencion-55", enlace: "iris://conversaciones/55" },
            { origen: "bandeja", conversacionId: 55, mensajeId: "0194b", remitente: { id: 9, nombre: "Ana López", iniciales: "AL" } },
        );
        await ejecutarRespuesta({ accion: "responder", texto: "Sí, lo llevo", mensaje: m }, d);
        await ejecutarRespuesta({ accion: "leido", mensaje: m }, d);
        expect(api.llamadas).toEqual(["notaBandeja:55:Sí, lo llevo:9", "leerBandeja:55:0194b"]);
    });

    it("tocar o «Ver orden» devuelve el destino; «Cómo llegar» lo marca", async () => {
        const api = apiFalsa();
        const d = deps(api);
        expect(await ejecutarRespuesta({ accion: "abrir", mensaje: mensaje() }, d)).toEqual({ destino: { tipo: "vista", vista: { tipo: "conversacion", id: 12 } } });
        const orden = mensaje({ tipo: "orden", grupo: "orden-7", enlace: "iris://orden/7", acciones: "ver_orden,como_llegar" }, { ordenId: 7, hoy: true });
        expect(await ejecutarRespuesta({ accion: "como_llegar", mensaje: orden }, d)).toEqual({ destino: { tipo: "vista", vista: { tipo: "orden", id: 7 } }, comoLlegar: true });
    });

    it("para otra persona (otra sesión en el teléfono) o sin sesión no hace nada", async () => {
        const api = apiFalsa();
        const escritos: string[] = [];
        expect(await ejecutarRespuesta({ accion: "abrir", mensaje: mensaje({ usuarioId: "6" }) }, deps(api))).toBeNull();
        expect(await ejecutarRespuesta({ accion: "responder", texto: "x", mensaje: mensaje() }, deps(api, { usuarioId: null, escribirChat: async () => void escritos.push("x") }))).toBeNull();
        expect(escritos).toEqual([]);
        expect(api.llamadas).toEqual([]);
    });
});

describe("textos de No molestar", () => {
    it("valida las horas y dice si está activo", () => {
        expect(errorHora("25:00", "06:00")).toMatch(/21:00/);
        expect(errorHora("06:00", "06:00")).toMatch(/iguales/);
        expect(errorHora("22:30", "06:00")).toBeNull();
        const nm = { activo: true, desde: "21:00", hasta: "06:00", urgenteIgual: true };
        expect(enNoMolestar(nm, 22 * 60)).toBe(true);
        expect(enNoMolestar(nm, 5 * 60)).toBe(true);
        expect(enNoMolestar(nm, 12 * 60)).toBe(false);
        // 14:32 en Guatemala (20:32 UTC).
        expect(lineaNoMolestar(preferencias({ noMolestar: nm }), Date.parse("2026-11-06T20:32:00Z"))).toBe("Hoy empieza a las 21:00. Lo que llegue se ve al abrir Iris. Lo urgente te llega igual.");
        expect(lineaNoMolestar(preferencias({ noMolestar: nm }), Date.parse("2026-11-07T04:00:00Z"))).toMatch(/^Activo hasta las 06:00/);
    });

    it("resumen de la fila en «Yo»", () => {
        expect(resumenNotificaciones(preferencias(), "denegado")).toBe("Bloqueadas en este teléfono");
        expect(resumenNotificaciones(preferencias({ noMolestar: { activo: true, desde: "21:00", hasta: "06:00", urgenteIgual: true } }), "concedido")).toBe("No molestar de 21:00 a 06:00");
        expect(resumenNotificaciones(preferencias({ push: { estado: "sin_configurar", texto: "Notificaciones push: sin configurar" } }), "concedido")).toBe("Notificaciones push: sin configurar");
        expect(resumenNotificaciones(preferencias(), "concedido")).toBe("Activas");
    });
});
