import type { Schema } from "@iris/contratos";
import type { Route } from "@playwright/test";

/*
 * Centralita Issabel (E4a) en la API falsa (e2e/api-falsa.ts), tipada con packages/contratos y fiel a
 * contratos/voz.md §3.12: la conexión (probar sin guardar, guardar con la huella, probar la guardada
 * y encender), las extensiones y las llamadas, que llegan por el feed de la bandeja como
 * `llamada_*` con `llamada.origen = centralita` y `conversacionId = 0`.
 */

type Centralita = Schema<"Centralita">;
type Extension = Schema<"Extension">;
type Llamada = Schema<"Llamada">;
type Prueba = Schema<"PruebaCentralita">;
type Evento = Schema<"EventoConversacion">;
type Problema = Schema<"Problema">;

const json = (route: Route, cuerpo: unknown, status = 200) => route.fulfill({ status, contentType: "application/json", body: JSON.stringify(cuerpo) });
const problema = (route: Route, p: Problema) => route.fulfill({ status: p.status, contentType: "application/problem+json", body: JSON.stringify(p) });
const P = (codigo: string, status: number, title: string, detail?: string): Problema => ({ type: `urn:iris:problema:${codigo}`, title, status, ...(detail ? { detail } : {}) });

export const HUELLA_AMI = "4f9a21c07e55b318aa02cc4d5e6f708192a3b4c5d6e7f8091a2b3c4d5e6fd261";
export const HUELLA_NUEVA = "880be4170000000000000000000000000000000000000000000000000000003c9f";
const BASE = "/api/v1/mensajeria";

export type OpcionesCentralita = {
    /** Ya hay una centralita conectada (por defecto, ninguna). */
    conectada?: boolean;
    /** Estado de la conexión al empezar (con `conectada`). */
    estado?: "conectada" | "desconectada" | "huella_cambiada";
    /** Lo que responde «Probar» (por defecto, ok). */
    prueba?: Prueba["estado"];
    /** Usuarios con extensión al empezar: id → extensión. */
    extensiones?: Record<number, string>;
    /** De esos, los que contestan en Iris (E4c) con la contraseña guardada y probada. */
    navegador?: number[];
    /** Llamadas guardadas al empezar (ficha del cliente, ticket). */
    llamadas?: Llamada[];
};

export const PERSONAS_CENTRALITA = [
    { id: 1, nombre: "Ana López", conversacion_id: null },
    { id: 14, nombre: "Rosa Pop", conversacion_id: null },
    { id: 15, nombre: "Luis Chávez", conversacion_id: null },
];

function centralitaInicial(o: OpcionesCentralita): Centralita | null {
    if (!o.conectada) return null;
    const ahora = new Date().toISOString();
    const base: Centralita = {
        id: 7, nombre: "Oficina central", tipo: "issabel", host: "10.20.0.5", puerto: 5039, usuario: "iris_lectura", secretoGuardadoEn: "2026-11-19T15:00:00Z",
        huellaTls: HUELLA_AMI, huellaVista: null, modo: "lectura", estado: "conectada", errorCodigo: null, probadaEn: ahora, conectadaEn: ahora,
        desconectadaDesde: null, ultimoEventoEn: new Date(Date.now() - 60_000).toISOString(), activa: true, telefono: { activo: false, wssPuerto: 8089, wssRuta: "/ws", huellaWss: null, sipDominio: null, prefijoSalida: "", llamadasPorUsuarioDia: 60, llamadasSimultaneas: 10, probadoEn: null, restriccionesConfirmadasEn: null }, version: 3,
    };
    if (o.estado === "desconectada") return { ...base, estado: "desconectada", desconectadaDesde: new Date(Date.now() - 10 * 60_000).toISOString() };
    if (o.estado === "huella_cambiada") return { ...base, estado: "error", errorCodigo: "huella_cambiada", huellaVista: HUELLA_NUEVA };
    return base;
}

export function apiCentralita(o: OpcionesCentralita, emitir: (e: Omit<Evento, "id" | "ocurridoEn" | "noLeidos">) => void) {
    let centralita = centralitaInicial(o);
    let siguienteExt = 100;
    const extensiones: Extension[] = Object.entries(o.extensiones ?? {}).map(([u, ext]) => ({
        id: siguienteExt++, extension: ext, usuarioId: Number(u), usuarioNombre: PERSONAS_CENTRALITA.find((p) => p.id === Number(u))?.nombre ?? "Persona",
        ...(o.navegador?.includes(Number(u))
            ? { tipo: "navegador" as const, sipUsuario: ext, sipGuardadoEn: "2026-11-18T15:00:00Z", sipProbadoEn: "2026-11-18T15:01:00Z", sipRechazadoEn: null }
            : { tipo: "escritorio" as const, sipUsuario: null, sipGuardadoEn: null, sipProbadoEn: null, sipRechazadoEn: null }),
    }));
    const llamadas: Llamada[] = structuredClone(o.llamadas ?? []);
    const recibido = { pruebas: [] as unknown[], creadas: [] as unknown[], actualizadas: [] as unknown[], activaciones: 0, asignaciones: [] as unknown[], quitadas: 0,
        anotaciones: [] as unknown[], vinculos: [] as unknown[], asociaciones: [] as unknown[] };

    function prueba(): Prueba {
        const estado = o.prueba ?? "ok";
        if (estado === "ok") return { estado, huella: HUELLA_AMI, version: "Asterisk Call Manager/7.0.3", accionesDeMas: [] };
        if (estado === "permisos_de_mas") return { estado, huella: HUELLA_AMI, version: "Asterisk Call Manager/7.0.3", accionesDeMas: ["Originate", "Command"] };
        return { estado, huella: null, version: null, accionesDeMas: [] };
    }

    /** Suena la extensión: la llamada al feed de quien le sonó. */
    function sonar(l: Llamada) {
        const i = llamadas.findIndex((x) => x.id === l.id);
        if (i >= 0) llamadas[i] = l;
        else llamadas.unshift(l);
        emitir({ tipo: "llamada_entrante", conversacionId: 0, llamada: structuredClone(l) });
    }
    function actualizar(l: Llamada, tipo: "llamada_actualizada" | "llamada_terminada" = "llamada_actualizada") {
        const i = llamadas.findIndex((x) => x.id === l.id);
        if (i >= 0) llamadas[i] = l;
        emitir({ tipo, conversacionId: 0, llamada: structuredClone(l) });
    }

    async function manejar({ route, metodo, ruta, query, cuerpo, actor, puede }: {
        route: Route; metodo: string; ruta: string; query: URLSearchParams; cuerpo: unknown; actor: number; puede: (p: Schema<"Permiso">) => boolean;
    }): Promise<boolean> {
        if (!ruta.startsWith(`${BASE}/centralita`) && ruta !== `${BASE}/chat/personas`) return false;
        const c = cuerpo as Record<string, unknown> | null;
        if (ruta === `${BASE}/chat/personas`) return (await json(route, { data: PERSONAS_CENTRALITA, nextCursor: null }), true);
        if (metodo === "GET" && ruta === `${BASE}/centralita/mi-extension`) {
            const x = centralita?.activa ? extensiones.find((e) => e.usuarioId === actor) : undefined;
            return (await json(route, x && centralita ? { extension: x.extension, centralitaId: centralita.id, estado: centralita.estado, tipo: x.tipo } : { extension: null, centralitaId: null, estado: null, tipo: null }), true);
        }
        const configura = puede("centralita.configurar");
        const asigna = puede("centralita.asignar_extension");
        if (metodo === "GET" && ruta === `${BASE}/centralita`) {
            if (!asigna && !configura) return (await problema(route, P("prohibido", 403, "Sin permiso")), true);
            return (await json(route, centralita ? { centralita } : {}), true);
        }
        if (metodo === "GET" && ruta === `${BASE}/centralitas/llamadas`) {
            const cliente = Number(query.get("clienteId") ?? 0);
            const ticket = Number(query.get("ticketId") ?? 0);
            const data = llamadas.filter((l) => (cliente ? l.contacto.clienteId === cliente : true) && (ticket ? l.ticketId === ticket : true));
            return (await json(route, { data, nextCursor: null }), true);
        }
        const accionLlamada = /^\/api\/v1\/mensajeria\/centralitas\/llamadas\/([0-9a-f-]+)\/(anotar|asociar-cliente|vincular-ticket)$/.exec(ruta);
        if (metodo === "POST" && accionLlamada) {
            const l = llamadas.find((x) => x.id === accionLlamada[1]);
            if (!l) return (await problema(route, P("no_encontrado", 404, "La llamada no existe")), true);
            const v = structuredClone(l);
            if (accionLlamada[2] === "anotar") {
                recibido.anotaciones.push(c);
                if (typeof c?.nota === "string") v.nota = c.nota;
                if (typeof c?.resultado === "string") v.resultado = c.resultado as Llamada["resultado"];
            } else if (accionLlamada[2] === "vincular-ticket") {
                recibido.vinculos.push(c);
                v.ticketId = Number(c?.ticketId);
            } else {
                recibido.asociaciones.push(c);
                v.contacto = { tipo: "cliente", clienteId: Number(c?.clienteId) };
                v.titulo = "Comercial El Roble, S.A.";
            }
            v.version = (v.version ?? 1) + 1;
            Object.assign(l, v);
            return (await json(route, v), true);
        }
        if (!configura && !asigna) return (await problema(route, P("prohibido", 403, "Sin permiso")), true);
        if (metodo === "POST" && ruta === `${BASE}/centralitas/probar`) {
            if (!configura) return (await problema(route, P("prohibido", 403, "Sin permiso")), true);
            recibido.pruebas.push(c);
            return (await json(route, prueba()), true);
        }
        if (metodo === "POST" && ruta === `${BASE}/centralitas`) {
            recibido.creadas.push(c);
            centralita = {
                id: 7, nombre: String(c?.nombre), tipo: "issabel", host: String(c?.host), puerto: Number(c?.puerto ?? 5039), usuario: String(c?.usuario),
                secretoGuardadoEn: new Date().toISOString(), huellaTls: String(c?.huellaTls), huellaVista: null, modo: "lectura", estado: "desactivada", errorCodigo: null,
                probadaEn: null, conectadaEn: null, desconectadaDesde: null, ultimoEventoEn: null, activa: false, telefono: { activo: false, wssPuerto: 8089, wssRuta: "/ws", huellaWss: null, sipDominio: null, prefijoSalida: "", llamadasPorUsuarioDia: 60, llamadasSimultaneas: 10, probadoEn: null, restriccionesConfirmadasEn: null }, version: 1,
            };
            return (await json(route, centralita, 201), true);
        }
        const accion = /^\/api\/v1\/mensajeria\/centralitas\/(\d+)\/(actualizar|probar|activar|desactivar|confirmar-huella|extensiones|extensiones\/asignar|extensiones\/(\d+)\/quitar)$/.exec(ruta);
        if (!accion || !centralita) return (await problema(route, P("no_encontrado", 404, "La centralita no existe")), true);
        const tipo = accion[3] ? "quitar" : accion[2];
        if (metodo === "GET" && tipo === "extensiones") return (await json(route, { data: extensiones }), true);
        if (tipo === "extensiones/asignar") {
            recibido.asignaciones.push(c);
            const usuarioId = Number(c?.usuarioId);
            if (extensiones.some((e) => e.extension === c?.extension)) return (await problema(route, P("extension_ocupada", 409, "Esa extensión ya es de otra persona: quítasela primero")), true);
            const x: Extension = { id: siguienteExt++, extension: String(c?.extension), usuarioId, usuarioNombre: PERSONAS_CENTRALITA.find((p) => p.id === usuarioId)?.nombre ?? "Persona",
                tipo: "escritorio", sipUsuario: null, sipGuardadoEn: null, sipProbadoEn: null, sipRechazadoEn: null };
            extensiones.push(x);
            return (await json(route, x), true);
        }
        if (tipo === "quitar") {
            const i = extensiones.findIndex((e) => e.id === Number(accion[3]));
            if (i < 0) return (await problema(route, P("no_encontrado", 404, "La extensión no existe")), true);
            recibido.quitadas++;
            const [x] = extensiones.splice(i, 1);
            return (await json(route, x), true);
        }
        if (!configura) return (await problema(route, P("prohibido", 403, "Sin permiso")), true);
        if (tipo === "probar") {
            const p = prueba();
            if (p.estado === "ok") centralita = { ...centralita, probadaEn: new Date().toISOString(), version: centralita.version + 1 };
            return (await json(route, { ...p, centralita }), true);
        }
        if (tipo === "activar") {
            if (!centralita.probadaEn) return (await problema(route, P("sin_probar", 422, "Prueba la conexión antes de encenderla")), true);
            recibido.activaciones++;
            centralita = { ...centralita, activa: true, estado: "conectada", conectadaEn: new Date().toISOString(), version: centralita.version + 1 };
            return (await json(route, centralita), true);
        }
        if (tipo === "desactivar") {
            centralita = { ...centralita, activa: false, estado: "desactivada", version: centralita.version + 1 };
            return (await json(route, centralita), true);
        }
        if (tipo === "confirmar-huella") {
            if (c?.huella !== centralita.huellaVista) return (await problema(route, P("huella_no_coincide", 422, "La huella no es la que presentó la centralita")), true);
            centralita = { ...centralita, huellaTls: centralita.huellaVista, huellaVista: null, errorCodigo: null, estado: "conectada", version: centralita.version + 1 };
            return (await json(route, centralita), true);
        }
        if (tipo === "actualizar") {
            recibido.actualizadas.push(c);
            centralita = { ...centralita, nombre: String(c?.nombre), host: String(c?.host), usuario: String(c?.usuario), huellaTls: String(c?.huellaTls), version: centralita.version + 1 };
            return (await json(route, centralita), true);
        }
        return false;
    }

    /** El teléfono de Iris de la centralita (E4c, e2e/api-telefono.ts). */
    function telefono(cambio: Partial<NonNullable<Centralita["telefono"]>>) {
        if (!centralita?.telefono) return;
        centralita = { ...centralita, telefono: { ...centralita.telefono, ...cambio }, version: centralita.version + 1 };
    }

    return { manejar, sonar, actualizar, recibido, llamadas, extensiones, telefono, centralita: () => centralita };
}

export type ApiCentralita = ReturnType<typeof apiCentralita>;

/** Una llamada de la centralita como la manda el servidor. */
export function llamadaCentralita(extra: Partial<Llamada> = {}): Llamada {
    return {
        id: "0193f0aa-0000-7000-8000-000000000001",
        empresaId: 1,
        origen: "centralita",
        direccion: "entrante",
        numero: "+50255123480",
        titulo: "María Fernanda Pérez López",
        contacto: { tipo: "cliente", clienteId: 4101, servicioId: 41010 },
        senales: {
            servicio: { id: 41010, codigo: "S-1", plan: "Fibra 50 Mbps", estado: "activo", zona: "Zona Norte", precioMensual: "250.00", moneda: "GTQ" },
            ticketAbierto: { id: 4812, numero: "#4812", asunto: "Sin internet desde ayer" },
        },
        estado: "sonando",
        tramos: [],
        sonoEn: new Date().toISOString(),
        puede: { contestar: false, rechazar: false, colgar: false, tomar: false, transferir: false, anotar: true, escucharGrabacion: false },
        centralita: { id: 7, tuExtension: "204", receptores: [{ usuarioId: 1, nombre: "Ana", extension: "204" }], coincidencias: 1, candidatos: [], asociacion: "automatica", asociar: true },
        version: 1,
        ...extra,
    };
}
