import type { Schema } from "@iris/contratos";
import type { Route } from "@playwright/test";
import type {
    Agente,
    AjustesBandeja,
    BotNumero,
    Cola,
    Destinatario,
    Disponibilidad,
    Numero,
    PlantillaDisponible,
} from "../lib/conversaciones/tipos";
import { detalle } from "./datos-conversaciones";

/*
 * Bandeja 1.1 en la API falsa (contratos/conversaciones.md §14): disponibilidad y latido, ajustes,
 * supervisores por cola, «Pasar a…», iniciar una conversación (idempotente por `id`, como el
 * servidor) con sus destinatarios y plantillas por número, y el bot por número.
 */

type Problema = Schema<"Problema">;
type Conversacion = Schema<"ConversacionCliente">;
type Mensaje = Schema<"MensajeConversacion">;
type Permiso = Schema<"Permiso">;

const json = (route: Route, cuerpo: unknown, status = 200) => route.fulfill({ status, contentType: "application/json", body: JSON.stringify(cuerpo) });
const problema = (route: Route, p: Problema) => route.fulfill({ status: p.status, contentType: "application/problem+json", body: JSON.stringify(p) });
const P = (codigo: string, status: number, title: string, extra: Partial<Problema> = {}): Problema => ({ type: `urn:iris:problema:${codigo}`, title, status, ...extra });

export const PERMISOS_BANDEJA11: Permiso[] = ["conversaciones.iniciar", "conversaciones.pasar"];

export const BOT_INICIAL: BotNumero = {
    activo: true,
    atiende: "bot",
    minutosSinConfirmacion: 30,
    minutosSinDisponibles: 3,
    saludo: "Gracias por escribir. En este momento todos nuestros agentes están ocupados; te respondemos en cuanto se libere uno.",
    menu: [],
};

/** Personas de la bandeja con su disponibilidad (Ana es la admin de las pruebas). */
export function agentesBandeja11(): Agente[] {
    const disp = (estado: "disponible" | "no_disponible", origen: Agente["disponibilidad"]["origen"] = "manual") => ({ estado, origen, desde: "2026-10-28T14:00:00Z" });
    return [
        { id: 1, nombre: "Ana López", supervisa: true, colas: [{ id: 2, nombre: "Soporte" }, { id: 3, nombre: "Caja" }], colasSupervisadas: [], disponibilidad: disp("disponible"), abiertas: 1 },
        { id: 9, nombre: "Rosa Sánchez", supervisa: false, colas: [{ id: 2, nombre: "Soporte" }], colasSupervisadas: [], disponibilidad: disp("disponible"), abiertas: 3 },
        { id: 12, nombre: "Laura Méndez", supervisa: false, colas: [], colasSupervisadas: [{ id: 2, nombre: "Soporte" }, { id: 4, nombre: "Ventas" }], disponibilidad: disp("disponible"), abiertas: 5 },
        { id: 14, nombre: "Rosa Pop", supervisa: false, colas: [{ id: 2, nombre: "Soporte" }], colasSupervisadas: [], disponibilidad: disp("disponible"), abiertas: 0 },
        { id: 15, nombre: "Mario Ixcot", supervisa: false, colas: [{ id: 2, nombre: "Soporte" }], colasSupervisadas: [], disponibilidad: disp("no_disponible", "horario"), abiertas: 1 },
        { id: 16, nombre: "Karla Tun", supervisa: false, colas: [{ id: 3, nombre: "Caja" }], colasSupervisadas: [], disponibilidad: disp("no_disponible", "inactividad"), abiertas: 0 },
    ];
}

const PLANTILLAS: PlantillaDisponible[] = [
    {
        codigo: "confirmar_visita",
        nombre: "Confirmar visita técnica",
        categoria: "servicio",
        variables: [
            { nombre: "1", formato: "texto" },
            { nombre: "2", formato: "texto" },
        ],
        vistaPrevia: "Hola {{1}}, te escribimos de Soporte. Queremos confirmar tu visita técnica de {{2}}. ¿Te queda bien?",
    },
    { codigo: "seguimiento", nombre: "Seguimiento de soporte", categoria: "servicio", variables: [{ nombre: "nombre", formato: "texto" }], vistaPrevia: "Hola {{nombre}}, ¿pudiste volver a conectarte?" },
];

const DESTINATARIOS: Destinatario[] = [
    { tipo: "cliente", id: 4101, nombre: "Carlos Ruiz", codigo: "C-4101", detalle: null, numero: "+50255120041", puede: true, motivo: null, conversacionId: null },
    { tipo: "contacto_iris", id: 12, nombre: "Carlos Ramírez", codigo: null, detalle: "proveedor", numero: "+50240218830", puede: true, motivo: null, conversacionId: null },
    { tipo: "cliente", id: 3877, nombre: "Carlos Cifuentes", codigo: "C-3877", detalle: null, numero: "+50257301122", puede: false, motivo: "baja", conversacionId: null },
    { tipo: "cliente", id: 2290, nombre: "Carlos Pérez", codigo: "C-2290", detalle: null, numero: null, puede: false, motivo: "sin_contacto", conversacionId: null },
];

export type OpcionesBandeja11 = {
    /** Las primeras N llamadas a `POST /conversaciones/iniciar` responden 503 (para el reintento). */
    iniciarFalla503?: number;
    /** Estado inicial de mi disponibilidad. */
    disponibilidad?: "disponible" | "no_disponible";
};

type Estado = {
    actorId: () => number;
    conversaciones: Conversacion[];
    mensajes: Map<number, Mensaje[]>;
    agentes: Agente[];
    colas: Cola[];
    canales: Numero[];
    emitir: (tipo: "mensaje_nuevo" | "conversacion_actualizada", c: Conversacion, m?: Mensaje) => void;
    enviados: { ruta: string; cuerpo: unknown; idempotencia: string | undefined }[];
};

const BASE = "/api/v1/mensajeria";

export function bandeja11(e: Estado, opciones: OpcionesBandeja11 = {}) {
    const disponibilidades = new Map<number, Disponibilidad>();
    const latidos: number[] = [];
    let ajustes: AjustesBandeja = { minutosInactividad: 15, version: 1 };
    let fallas503 = opciones.iniciarFalla503 ?? 0;
    const iniciadas = new Map<string, { cuerpo: string; respuesta: Schema<"ConversacionIniciada"> }>();
    let siguienteConv = 70;

    function miDisponibilidad(): Disponibilidad {
        const id = e.actorId();
        let d = disponibilidades.get(id);
        if (!d) {
            const estado = opciones.disponibilidad ?? "disponible";
            d = { estado, origen: "manual", desde: "2026-10-28T14:00:00Z", horarioHasta: estado === "disponible" ? "2026-10-29T04:00:00Z" : null, minutosInactividad: ajustes.minutosInactividad, version: 1 };
            disponibilidades.set(id, d);
        }
        return { ...d, minutosInactividad: ajustes.minutosInactividad };
    }

    async function manejar(x: { route: Route; metodo: string; ruta: string; query: URLSearchParams; cuerpo: Record<string, unknown>; idempotencia: string | undefined; puede: (p: Permiso) => boolean }): Promise<boolean> {
        const { route, metodo, ruta, query, cuerpo, puede } = x;
        const prohibido = async () => (await problema(route, P("permiso_denegado", 403, "Sin permiso")), true);
        const noExiste = async () => (await problema(route, P("no_encontrado", 404, "No existe")), true);
        const conflicto = async () => (await problema(route, P("conflicto_version", 409, "Cambió")), true);
        const yo = e.actorId();

        // §14.5 Disponibilidad, latido y ajustes.
        if (ruta === `${BASE}/conversaciones/disponibilidad`) {
            if (metodo === "GET") return (await json(route, miDisponibilidad()), true);
            const d = miDisponibilidad();
            if (cuerpo.version !== d.version) return conflicto();
            const estado = cuerpo.estado === "disponible" ? "disponible" : "no_disponible";
            const nueva: Disponibilidad = { ...d, estado, origen: "manual", desde: new Date().toISOString(), horarioHasta: estado === "disponible" ? "2026-10-29T04:00:00Z" : null, version: d.version + 1 };
            disponibilidades.set(yo, nueva);
            const a = e.agentes.find((v) => v.id === yo);
            if (a) a.disponibilidad = { estado, origen: "manual", desde: nueva.desde };
            e.enviados.push({ ruta, cuerpo, idempotencia: x.idempotencia });
            return (await json(route, nueva), true);
        }
        if (ruta === `${BASE}/conversaciones/disponibilidad/latido` && metodo === "POST") {
            latidos.push(Date.now());
            return (await json(route, miDisponibilidad()), true);
        }
        if (ruta === `${BASE}/conversaciones/ajustes`) {
            if (metodo === "GET") return (await json(route, ajustes), true);
            if (!puede("canales.configurar")) return prohibido();
            if (cuerpo.version !== ajustes.version) return conflicto();
            const min = Number(cuerpo.minutosInactividad);
            if (!Number.isInteger(min) || min < 1 || min > 480) {
                return (await problema(route, P("validacion", 422, "Datos inválidos", { errors: [{ field: "body.minutosInactividad", message: "Entre 1 y 480" }] })), true);
            }
            ajustes = { minutosInactividad: min, version: ajustes.version + 1 };
            e.enviados.push({ ruta, cuerpo, idempotencia: x.idempotencia });
            return (await json(route, ajustes), true);
        }

        // §14.3 Destinatarios, plantillas por número e iniciar.
        if (ruta === `${BASE}/conversaciones/destinatarios` && metodo === "GET") {
            if (!puede("conversaciones.iniciar")) return noExiste();
            const q = (query.get("q") ?? "").toLowerCase();
            const canalId = Number(query.get("canalId"));
            const data = DESTINATARIOS.filter((d) => d.nombre.toLowerCase().includes(q) || (d.codigo ?? "").toLowerCase().includes(q)).map((d) => {
                const existente = canalId && d.numero ? e.conversaciones.find((c) => c.numero === d.numero && c.canal.id === canalId) : undefined;
                return { ...d, conversacionId: existente?.id ?? null };
            });
            return (await json(route, { data, nextCursor: null }), true);
        }
        const pl = /^\/api\/v1\/mensajeria\/conversaciones\/canales\/(\d+)\/plantillas$/.exec(ruta);
        if (pl && metodo === "GET") {
            const canal = e.canales.find((c) => c.id === Number(pl[1]));
            if (!canal || !canal.iniciar) return noExiste();
            return (await json(route, { data: canal.id === 4 ? [] : PLANTILLAS, nextCursor: null }), true);
        }
        if (ruta === `${BASE}/conversaciones/iniciar` && metodo === "POST") {
            if (!puede("conversaciones.iniciar")) return noExiste();
            e.enviados.push({ ruta, cuerpo, idempotencia: x.idempotencia });
            const id = String(cuerpo.id);
            const firma = JSON.stringify({ ...cuerpo, id: undefined });
            const previa = iniciadas.get(id);
            if (previa) {
                if (previa.cuerpo !== firma) return (await problema(route, P("mensaje_id_reutilizado", 409, "Ese id ya se usó con otro contenido")), true);
                await route.fulfill({ status: 200, contentType: "application/json", headers: { "Idempotent-Replayed": "true" }, body: JSON.stringify(previa.respuesta) });
                return true;
            }
            if (fallas503 > 0) {
                fallas503 -= 1;
                return (await problema(route, P("no_disponible", 503, "El servicio no respondió")), true);
            }
            const canal = e.canales.find((c) => c.id === cuerpo.canalId);
            if (!canal?.iniciar) return (await problema(route, P("numero_no_permitido", 422, "Ese número no es de tus colas")), true);
            const dest = DESTINATARIOS.find((d) => (cuerpo.clienteId ? d.tipo === "cliente" && d.id === cuerpo.clienteId : d.tipo === "contacto_iris" && d.id === cuerpo.contactoIrisId));
            if (!dest) return noExiste();
            if (!dest.puede) return (await problema(route, P(dest.motivo ?? "sin_permiso", 422, "No se le puede escribir")), true);
            const plantilla = cuerpo.plantilla as { codigo: string; variables: Record<string, string> };
            const ahora = new Date().toISOString();
            const existente = e.conversaciones.find((c) => c.numero === dest.numero && c.canal.id === canal.id);
            const yoAgente = e.agentes.find((a) => a.id === yo);
            const c: Conversacion = existente ?? {
                id: siguienteConv++,
                empresaId: 1,
                version: 1,
                canal: { id: canal.id, nombre: canal.nombre, numero: canal.numero, usos: canal.usos },
                ...(canal.colaId ? { cola: { id: canal.colaId, nombre: e.colas.find((q) => q.id === canal.colaId)?.nombre ?? "" } } : {}),
                numero: dest.numero ?? "",
                perfilNombre: null,
                titulo: dest.nombre,
                contacto: {
                    tipo: dest.tipo,
                    clienteId: dest.tipo === "cliente" ? dest.id : null,
                    servicioId: null,
                    contactoIrisId: dest.tipo === "contacto_iris" ? dest.id : null,
                    prospectoId: null,
                    asociacion: "manual",
                    verificado: dest.tipo === "cliente",
                },
                estado: "esperando_cliente",
                reabiertaEn: null,
                etiquetas: [],
                ventana: { abierta: false, cierraEn: null },
                sla: { estado: "sin", venceEn: null, esperaDesde: null },
                noLeidos: 0,
                ultimaActividad: ahora,
                creadaEn: ahora,
            };
            c.asignado = { id: yo, nombre: yoAgente?.nombre ?? "Ana López" };
            c.estado = "esperando_cliente";
            const texto = PLANTILLAS.find((p) => p.codigo === plantilla.codigo)?.vistaPrevia.replace(/\{\{\s*([\w.]+)\s*\}\}/g, (_, n: string) => plantilla.variables[n] ?? "") ?? null;
            const m: Mensaje = {
                id,
                conversacionId: c.id,
                seq: 1,
                direccion: "saliente",
                tipo: "plantilla",
                texto,
                plantilla: { codigo: plantilla.codigo, variables: plantilla.variables },
                autor: { id: yo, nombre: c.asignado.nombre },
                estadoEntrega: "en_cola",
                motivoFalla: null,
                menciones: [],
                creadoEn: ahora,
                enviadoEn: null,
                entregadoEn: null,
                leidoEn: null,
                automatico: null,
            };
            c.ultimoMensaje = { direccion: "saliente", tipo: "plantilla", texto, evento: null, creadoEn: ahora };
            if (!existente) {
                e.conversaciones.push(c);
                e.mensajes.set(c.id, []);
            }
            e.mensajes.get(c.id)?.push(
                {
                    id: `${id}-s`,
                    conversacionId: c.id,
                    seq: 0,
                    direccion: "sistema",
                    tipo: "sistema",
                    texto: null,
                    evento: { tipo: "iniciada", actorNombre: c.asignado.nombre.split(" ")[0], plantilla: plantilla.codigo },
                    estadoEntrega: null,
                    motivoFalla: null,
                    menciones: [],
                    creadoEn: ahora,
                    enviadoEn: null,
                    entregadoEn: null,
                    leidoEn: null,
                    automatico: null,
                },
                m,
            );
            const respuesta: Schema<"ConversacionIniciada"> = { conversacion: detalle(c), mensaje: m, nueva: !existente };
            iniciadas.set(id, { cuerpo: firma, respuesta });
            e.emitir("mensaje_nuevo", c, m);
            return (await json(route, respuesta, 201), true);
        }

        // §14.4 Pasar a…
        const pasar = /^\/api\/v1\/mensajeria\/conversaciones\/(\d+)\/pasar$/.exec(ruta);
        if (pasar && metodo === "POST") {
            if (!puede("conversaciones.pasar")) return prohibido();
            e.enviados.push({ ruta, cuerpo, idempotencia: x.idempotencia });
            const c = e.conversaciones.find((v) => v.id === Number(pasar[1]));
            if (!c) return noExiste();
            if (cuerpo.version !== c.version) return conflicto();
            const destino = e.agentes.find((a) => a.id === cuerpo.usuarioId);
            if (!destino) return (await problema(route, P("usuario_no_disponible", 422, "No es de la cola")), true);
            if (c.asignado?.id === destino.id) return (await problema(route, P("mismo_agente", 422, "Ya la tiene")), true);
            const disponible = destino.disponibilidad.estado === "disponible";
            const yoAgente = e.agentes.find((a) => a.id === yo);
            c.asignado = { id: destino.id, nombre: destino.nombre };
            delete (c as Conversacion).espera;
            c.version += 1;
            const ahora = new Date().toISOString();
            const nota = typeof cuerpo.nota === "string" ? cuerpo.nota : "";
            const base = { conversacionId: c.id, seq: 0, tipo: "texto" as const, estadoEntrega: null, motivoFalla: null, menciones: [], creadoEn: ahora, enviadoEn: null, entregadoEn: null, leidoEn: null, automatico: null };
            const sistema: Mensaje = {
                ...base,
                id: `pasada-${c.version}`,
                direccion: "sistema",
                tipo: "sistema",
                texto: null,
                evento: { tipo: "pasada", actorNombre: (yoAgente?.nombre ?? "Ana").split(" ")[0], asignadoNombre: destino.nombre, ...(disponible ? {} : { detalle: "no_disponible" }) },
            };
            e.mensajes.get(c.id)?.push(sistema);
            if (nota) e.mensajes.get(c.id)?.push({ ...base, id: `nota-${c.version}`, direccion: "nota", texto: nota, autor: { id: yo, nombre: yoAgente?.nombre ?? "Ana López" } });
            e.emitir("mensaje_nuevo", c, sistema);
            const r: Schema<"PaseConversacion"> = { conversacion: detalle(c), destino: { id: destino.id, nombre: destino.nombre }, destinoDisponible: disponible };
            return (await json(route, r), true);
        }

        // §14.1 Colas: crear, actualizar, miembros y supervisores.
        const cola = /^\/api\/v1\/mensajeria\/colas(?:\/(\d+)\/(actualizar|miembros|supervisores|activar|desactivar))?$/.exec(ruta);
        if (cola && metodo === "POST") {
            if (!puede("canales.configurar")) return prohibido();
            e.enviados.push({ ruta, cuerpo, idempotencia: x.idempotencia });
            const persona = (id: number) => {
                const a = e.agentes.find((v) => v.id === id);
                return a ? { id: a.id, nombre: a.nombre } : null;
            };
            if (!cola[1]) {
                if (e.colas.some((q) => q.nombre.toLowerCase() === String(cuerpo.nombre).toLowerCase())) {
                    return (await problema(route, P("cola_existente", 422, "Ya hay una cola con ese nombre", { errors: [{ field: "body.nombre", message: "Ya hay una cola con ese nombre" }] })), true);
                }
                const nueva: Cola = {
                    id: e.colas.length + 20,
                    empresaId: 1,
                    nombre: String(cuerpo.nombre),
                    descripcion: String(cuerpo.descripcion ?? ""),
                    slaRespuestaMin: (cuerpo.slaRespuestaMin as number | null) ?? null,
                    slaPorVencerMin: null,
                    horarioId: null,
                    miembros: [],
                    activa: true,
                    version: 1,
                    supervisores: [],
                    disponibles: [],
                    botSinConfirmacionMin: (cuerpo.botSinConfirmacionMin as number | undefined) ?? null,
                    botSinDisponiblesMin: (cuerpo.botSinDisponiblesMin as number | undefined) ?? null,
                };
                e.colas.push(nueva);
                return (await json(route, nueva, 201), true);
            }
            const q = e.colas.find((v) => v.id === Number(cola[1]));
            if (!q) return noExiste();
            if (cuerpo.version !== q.version) return conflicto();
            if (cola[2] === "miembros") q.miembros = ((cuerpo.usuarioIds as number[]) ?? []).map(persona).filter((p): p is { id: number; nombre: string } => p !== null);
            else if (cola[2] === "supervisores") {
                if (cuerpo.asistenteIa) return (await problema(route, P("no_disponible_aun", 422, "Llega en la fase 3")), true);
                q.supervisores = ((cuerpo.usuarioIds as number[]) ?? []).map(persona).filter((p): p is { id: number; nombre: string } => p !== null);
            } else if (cola[2] === "activar") q.activa = true;
            else if (cola[2] === "desactivar") q.activa = false;
            else {
                q.nombre = String(cuerpo.nombre);
                q.descripcion = String(cuerpo.descripcion ?? "");
                q.slaRespuestaMin = (cuerpo.slaRespuestaMin as number | null) ?? null;
                q.botSinConfirmacionMin = (cuerpo.botSinConfirmacionMin as number | undefined) ?? null;
                q.botSinDisponiblesMin = (cuerpo.botSinDisponiblesMin as number | undefined) ?? null;
            }
            q.version += 1;
            return (await json(route, q), true);
        }

        // Validación del bot al configurar un número (§14.6), antes del resto de `configurar`.
        const conf = /^\/api\/v1\/mensajeria\/canales\/(\d+)\/configurar$/.exec(ruta);
        if (conf && metodo === "POST" && cuerpo.bot) {
            const b = cuerpo.bot as BotNumero;
            if (b.atiende === "agente_autonomo") return (await problema(route, P("no_disponible_aun", 422, "Llega en la fase 3")), true);
            if ((b.menu ?? []).length > 9) {
                return (await problema(route, P("validacion", 422, "Datos inválidos", { errors: [{ field: "body.bot.menu", message: "Hasta 9 opciones" }] })), true);
            }
        }
        return false;
    }

    return { manejar, latidos, disponibilidades, ajustes: () => ajustes };
}

/** Los catálogos de la API falsa con los campos de la fase 1.1. */
export type ColaBase = Omit<Cola, "supervisores" | "disponibles" | "botSinConfirmacionMin" | "botSinDisponiblesMin">;
export type NumeroBase = Omit<Numero, "bot" | "iniciar">;

export function colasBandeja11(colas: ColaBase[], agentes: Agente[]): Cola[] {
    return colas.map((c) => {
        const supervisores = c.id === 2 ? [{ id: 12, nombre: "Laura Méndez" }, { id: 1, nombre: "Ana López" }] : [];
        const miembros = c.id === 2 ? [...c.miembros, { id: 15, nombre: "Mario Ixcot" }] : c.id === 3 ? [...c.miembros, { id: 16, nombre: "Karla Tun" }] : c.miembros;
        const ids = [...miembros, ...supervisores].map((p) => p.id);
        const disponibles = agentes.filter((a) => ids.includes(a.id) && a.disponibilidad.estado === "disponible").map((a) => a.id);
        return { ...c, miembros, supervisores, disponibles, botSinConfirmacionMin: c.id === 2 ? 30 : null, botSinDisponiblesMin: null };
    });
}

export function canalesBandeja11(canales: NumeroBase[]): Numero[] {
    return canales.map((c) => ({ ...c, iniciar: c.id !== 5, bot: { ...BOT_INICIAL, activo: c.id === 3, menu: c.id === 3 ? [{ texto: "Soporte técnico", colaId: 2, etiquetaIds: [] }, { texto: "Pagos", colaId: 3, etiquetaIds: [5] }] : [] } }));
}
