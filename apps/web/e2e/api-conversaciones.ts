import type { Schema } from "@iris/contratos";
import type { Route } from "@playwright/test";
import { ANDREA, CANAL_AVISOS, CANAL_COBROS, CANAL_SOPORTE, CANAL_VENTAS, CONVERSACIONES, detalle, mensajesDe } from "./datos-conversaciones";

/*
 * Bandeja de conversaciones en la API falsa (e2e/api-falsa.ts), tipada con packages/contratos y
 * fiel a contratos/conversaciones.md: vistas y filtros, `version` (409 `conflicto_version`), tomar
 * (409 `ya_asignada`), ventana de 24 h (422 `fuera_de_ventana`), envío idempotente por `id` y el
 * feed (`/cambios` y `/eventos`, que entrega lo encolado y cierra: el panel reconecta).
 */

type Problema = Schema<"Problema">;
type Permiso = Schema<"Permiso">;
type Conversacion = Schema<"ConversacionCliente">;
type Mensaje = Schema<"MensajeConversacion">;
type Evento = Schema<"EventoConversacion">;

const json = (route: Route, cuerpo: unknown, status = 200) => route.fulfill({ status, contentType: "application/json", body: JSON.stringify(cuerpo) });
const problema = (route: Route, p: Problema) => route.fulfill({ status: p.status, contentType: "application/problem+json", body: JSON.stringify(p) });
const P = (codigo: string, status: number, title: string, extra: Partial<Problema> = {}): Problema => ({ type: `urn:iris:problema:${codigo}`, title, status, ...extra });

export const PERMISOS_CONVERSACIONES: Permiso[] = [
    "conversaciones.ver",
    "conversaciones.supervisar",
    "conversaciones.responder",
    "conversaciones.tomar",
    "conversaciones.asignar",
    "conversaciones.transferir",
    "conversaciones.etiquetar",
    "conversaciones.resolver",
    "conversaciones.asociar",
    "conversaciones.registrar_boleta",
    "conversaciones.registrar_promesa",
    "conversaciones.configurar",
    "canales.configurar",
    "manuales.ver",
    "manuales.editar",
];

export type OpcionesConversaciones = {
    /** `/eventos` responde 503: el panel trabaja con el sondeo. */
    sinFlujo?: boolean;
};

const BASE = "/api/v1/mensajeria";

export function apiConversaciones(actorId: () => number, opciones: OpcionesConversaciones = {}) {
    const conversaciones: Conversacion[] = CONVERSACIONES.map((c) => structuredClone(c));
    const mensajes = new Map<number, Mensaje[]>(conversaciones.map((c) => [c.id, mensajesDe(c.id)]));
    const eventos: Evento[] = [];
    let cursor = 100;
    let seq = 1000;
    const enviados: { ruta: string; cuerpo: unknown; idempotencia: string | undefined }[] = [];

    const colas: Schema<"ColaConversaciones">[] = [
        { id: 2, empresaId: 1, nombre: "Soporte", descripcion: "", slaRespuestaMin: 10, slaPorVencerMin: null, miembros: [ANDREA, { id: 9, nombre: "Rosa Sánchez" }], activa: true, version: 1 },
        { id: 3, empresaId: 1, nombre: "Caja", descripcion: "", slaRespuestaMin: 30, slaPorVencerMin: null, miembros: [ANDREA], activa: true, version: 1 },
        { id: 4, empresaId: 1, nombre: "Ventas", descripcion: "", slaRespuestaMin: 60, slaPorVencerMin: null, miembros: [], activa: true, version: 1 },
    ];
    const etiquetas: Schema<"EtiquetaConversacion">[] = [
        { id: 3, nombre: "Sin servicio", color: "peligro", activa: true, version: 1 },
        { id: 5, nombre: "Empresarial", color: "info", activa: true, version: 1 },
    ];
    const agentes: Schema<"AgenteConversaciones">[] = [
        { id: ANDREA.id, nombre: ANDREA.nombre, supervisa: true, colas: [{ id: 2, nombre: "Soporte" }] },
        { id: 9, nombre: "Rosa Sánchez", supervisa: false, colas: [{ id: 2, nombre: "Soporte" }] },
        { id: 12, nombre: "Laura Méndez", supervisa: true, colas: [] },
    ];
    const respuestas: Schema<"RespuestaRapida">[] = [
        { id: 1, colaId: null, atajo: "/visita", titulo: "Visita hoy 13:30–15:00", texto: "Hola {{nombre}}, te enviamos un técnico hoy entre 13:30 y 15:00.", activa: true, version: 1 },
        { id: 2, colaId: 2, atajo: "/reinicio", titulo: "Pedir reinicio del módem", texto: "¿Puedes apagar el módem 30 segundos y volver a encenderlo?", activa: true, version: 1 },
        { id: 3, colaId: 2, atajo: "/falla", titulo: "Falla en la zona", texto: "Hay una falla en tu zona y ya la estamos atendiendo.", activa: true, version: 1 },
    ];
    const horarios: Schema<"Horario">[] = [
        {
            id: 1,
            nombre: "Soporte · extendido",
            zona: "America/Guatemala",
            version: 3,
            franjas: [{ dias: [1, 2, 3, 4, 5, 6, 7], inicio: "06:00", fin: "22:00", perfil: null }],
            feriados: [{ fecha: "2026-12-25", nombre: "Navidad", franjas: [] }],
        },
        {
            id: 2,
            nombre: "Caja",
            zona: "America/Guatemala",
            version: 1,
            franjas: [
                { dias: [1, 2, 3, 4, 5], inicio: "08:00", fin: "17:00", perfil: null },
                { dias: [6], inicio: "08:00", fin: "12:00", perfil: null },
            ],
            feriados: [],
        },
    ];
    const canales: Schema<"NumeroConversaciones">[] = [CANAL_AVISOS, CANAL_SOPORTE, CANAL_COBROS, CANAL_VENTAS].map((c) => ({
        ...c,
        avisosEmpresa: c.id === 2,
        colaId: c.id === 4 ? 3 : c.id === 5 ? 4 : 2,
        activo: true,
        version: 1,
        horarioId: c.id === 4 ? 2 : 1,
        fueraDeHorario: {
            responder: true,
            mensaje: "Gracias por escribir. Atendemos de 6:00 a 22:00; te respondemos al abrir.",
            guardia: { activa: c.id === 3, usuarioId: c.id === 3 ? 12 : null, etiquetaIds: c.id === 3 ? [5] : [], fallaMasiva: c.id === 3 },
            asistente: { activo: false },
        },
    }));
    const manuales: Schema<"Manual">[] = [
        {
            id: 1,
            titulo: "Sin servicio en fibra: diagnóstico",
            resumen: "Revisar luces de la ONU, falla en la zona, potencia y reinicio guiado antes de agendar visita.",
            cuerpo: "## Pasos\n\n1. Revisa si hay **falla en la zona**.\n2. Pide la luz PON y LOS.\n3. Si LOS está roja, agenda visita.\n\n<b>no es HTML</b>",
            etiquetas: ["sin servicio"],
            equipo: "",
            visibilidad: "interna",
            activo: true,
            version: 1,
        },
        {
            id: 2,
            titulo: "Reiniciar la ONU ZTE F670L",
            resumen: "Guía con fotos para el cliente: apagar 30 segundos, revisar la luz PON y volver a probar.",
            cuerpo: "Apaga el equipo **30 segundos** y vuelve a encenderlo. [Video](https://iris.test/video)",
            etiquetas: ["onu", "reinicio"],
            equipo: "ZTE F670L",
            visibilidad: "cliente",
            activo: true,
            version: 1,
        },
    ];
    const configurados: unknown[] = [];

    /** Como el servidor: `mensaje_*` trae el mensaje y luego `conversacion_actualizada` la conversación. */
    function emitir(tipo: Evento["tipo"], c: Conversacion, m?: Mensaje) {
        const base = { conversacionId: c.id, ocurridoEn: new Date().toISOString(), noLeidos: c.noLeidos };
        if (m) {
            cursor += 1;
            eventos.push({ id: String(cursor), tipo, ...base, mensaje: m });
        }
        cursor += 1;
        eventos.push({ id: String(cursor), tipo: "conversacion_actualizada", ...base, conversacion: structuredClone(c) });
    }

    function filtrar(q: URLSearchParams): Conversacion[] {
        const yo = actorId();
        const vista = q.get("vista") ?? "todas";
        const estado = q.get("estado") ?? "activas";
        const texto = (q.get("q") ?? "").toLowerCase();
        return conversaciones
            .filter((c) => (vista === "mias" ? c.asignado?.id === yo : vista === "sin_asignar" ? !c.asignado : true))
            .filter((c) => (estado === "activas" ? c.estado !== "resuelta" : c.estado === estado))
            .filter((c) => !q.get("canalId") || c.canal.id === Number(q.get("canalId")))
            .filter((c) => !q.get("colaId") || c.cola?.id === Number(q.get("colaId")))
            .filter((c) => !q.get("etiquetaId") || c.etiquetas.some((e) => e.id === Number(q.get("etiquetaId"))))
            .filter((c) => !q.get("sla") || c.sla.estado === q.get("sla"))
            .filter((c) => !texto || c.titulo.toLowerCase().includes(texto) || c.numero.includes(texto.replace(/\D/g, "") || "x"))
            .sort((a, b) => Date.parse(b.ultimaActividad) - Date.parse(a.ultimaActividad));
    }

    function contadores(): Schema<"ContadoresConversaciones"> {
        const yo = actorId();
        const activas = conversaciones.filter((c) => c.estado !== "resuelta");
        const grupo = (clave: (c: Conversacion) => number[]) => {
            const m = new Map<number, number>();
            for (const c of activas) for (const id of clave(c)) m.set(id, (m.get(id) ?? 0) + 1);
            return [...m].map(([id, total]) => ({ id, total }));
        };
        return {
            mias: activas.filter((c) => c.asignado?.id === yo).length,
            sinAsignar: activas.filter((c) => !c.asignado).length,
            todas: activas.length,
            noLeidosMias: activas.filter((c) => c.asignado?.id === yo).reduce((n, c) => n + c.noLeidos, 0),
            porVencer: activas.filter((c) => c.sla.estado === "por_vencer").length,
            vencidas: activas.filter((c) => c.sla.estado === "vencido").length,
            porEstado: { abierta: activas.filter((c) => c.estado === "abierta").length, esperando_cliente: activas.filter((c) => c.estado === "esperando_cliente").length },
            porCanal: grupo((c) => [c.canal.id]),
            porCola: grupo((c) => (c.cola ? [c.cola.id] : [])),
            porEtiqueta: grupo((c) => c.etiquetas.map((e) => e.id)),
        };
    }

    /** Agrega un mensaje del cliente (como el webhook de Meta) y lo publica en el feed. */
    function mensajeDelCliente(id: number, texto: string) {
        const c = conversaciones.find((x) => x.id === id);
        if (!c) return;
        seq += 1;
        const m: Mensaje = {
            id: `0193b000-0000-7000-8000-${String(seq).padStart(12, "0")}`,
            conversacionId: id,
            seq,
            direccion: "entrante",
            tipo: "texto",
            texto,
            autor: null,
            estadoEntrega: null,
            motivoFalla: null,
            menciones: [],
            creadoEn: new Date().toISOString(),
            enviadoEn: null,
            entregadoEn: null,
            leidoEn: null,
        };
        mensajes.get(id)?.push(m);
        c.version += 1;
        c.noLeidos += 1;
        c.estado = "abierta";
        c.ultimoMensaje = { direccion: "entrante", tipo: "texto", texto, evento: null, creadoEn: m.creadoEn };
        c.ultimaActividad = m.creadoEn;
        emitir("mensaje_nuevo", c, m);
    }

    async function manejar(x: { route: Route; metodo: string; ruta: string; query: URLSearchParams; cuerpo: unknown; idempotencia: string | undefined; puede: (p: Permiso) => boolean }): Promise<boolean> {
        const { route, metodo, ruta, query, idempotencia, puede } = x;
        const cuerpo = (x.cuerpo ?? {}) as Record<string, unknown>;
        const prohibido = () => problema(route, P("permiso_denegado", 403, "Sin permiso"));
        if (await administrar(x.route, metodo, ruta, query, cuerpo, puede)) return true;
        if (!ruta.startsWith(`${BASE}/conversaciones`) && ![`${BASE}/colas`, `${BASE}/etiquetas`, `${BASE}/respuestas-rapidas`].includes(ruta)) return false;
        if (!puede("conversaciones.ver")) return (await prohibido(), true);

        if (metodo === "GET") {
            if (ruta === `${BASE}/colas`) return (await json(route, { data: colas, nextCursor: null }), true);
            if (ruta === `${BASE}/etiquetas`) return (await json(route, { data: etiquetas, nextCursor: null }), true);
            if (ruta === `${BASE}/respuestas-rapidas`) return (await json(route, { data: query.get("incluirInactivas") ? respuestas : respuestas.filter((r) => r.activa), nextCursor: null }), true);
            if (ruta === `${BASE}/conversaciones/cuentas-bancarias`) return (await json(route, { data: [{ id: 1, nombre: "Monetaria", banco: "Banco Industrial", numero: "000-123456-7", moneda: "GTQ" }], nextCursor: null }), true);
            if (ruta === `${BASE}/conversaciones/bancos`) return (await json(route, { data: [{ id: 1, nombre: "Banrural" }], nextCursor: null }), true);
            if (ruta === `${BASE}/conversaciones`) return (await json(route, { data: filtrar(query), nextCursor: null }), true);
            if (ruta === `${BASE}/conversaciones/contadores`) return (await json(route, contadores()), true);
            if (ruta === `${BASE}/conversaciones/canales`) return (await json(route, { data: canales, nextCursor: null }), true);
            if (ruta === `${BASE}/conversaciones/agentes`) return (await json(route, { data: agentes, nextCursor: null }), true);
            if (ruta === `${BASE}/conversaciones/cambios`) {
                const desde = Number(query.get("cursor") ?? cursor);
                const data = query.get("cursor") ? eventos.filter((e) => Number(e.id) > desde) : [];
                return (await json(route, { data, nextCursor: String(data.at(-1)?.id ?? (query.get("cursor") ? desde : cursor)), hayMas: false }), true);
            }
            if (ruta === `${BASE}/conversaciones/eventos`) {
                if (opciones.sinFlujo) return (await problema(route, P("flujos_agotados", 503, "Sin flujos")), true);
                const desde = Number((await route.request().headerValue("last-event-id")) ?? cursor);
                let pendientes = eventos.filter((e) => Number(e.id) > desde);
                if (pendientes.length === 0) {
                    // Espera breve (como un latido) y cierra: el panel reconecta con su cursor.
                    await new Promise((r) => setTimeout(r, 400));
                    pendientes = eventos.filter((e) => Number(e.id) > desde);
                }
                const cuerpoSSE = pendientes.map((e) => `id: ${e.id}\nevent: ${e.tipo}\ndata: ${JSON.stringify(e)}\n\n`).join("") || ": latido\n\n";
                await route.fulfill({ status: 200, headers: { "Content-Type": "text/event-stream", "Cache-Control": "no-store" }, body: cuerpoSSE });
                return true;
            }
        }

        const m = /^\/api\/v1\/mensajeria\/conversaciones\/(\d+)(?:\/(.+))?$/.exec(ruta);
        if (!m) return false;
        const c = conversaciones.find((v) => v.id === Number(m[1]));
        if (!c) return (await problema(route, P("no_encontrado", 404, "No existe")), true);
        const accion = m[2];

        if (metodo === "GET") {
            if (!accion) return (await json(route, detalle(c)), true);
            if (accion === "mensajes") return (await json(route, { data: mensajes.get(c.id) ?? [], nextCursor: null }), true);
            if (accion === "plantillas") {
                return (
                    await json(route, {
                        data: [
                            { codigo: "seguimiento", nombre: "Seguimiento de soporte", variables: [{ nombre: "nombre", formato: "texto", obligatoria: true }], vistaPrevia: "Hola {{nombre}}, ¿pudiste volver a conectarte?" },
                        ],
                        nextCursor: null,
                    }),
                    true
                );
            }
            if (accion === "manuales-sugeridos") {
                return (await json(route, { data: [{ manual: manuales[0], motivo: "etiqueta" }, { manual: manuales[1], motivo: "equipo" }], nextCursor: null }), true);
            }
            if (accion === "coincidencias") {
                return (
                    await json(route, {
                        clientes: [
                            { clienteId: 5101, nombre: "Luis Herrera", codigo: "C-5101", coincideEn: ["movil"], origen: "argos" },
                            { clienteId: 5102, nombre: "Lucía Herrera", codigo: "C-5102", coincideEn: ["contacto_servicio"], origen: "argos" },
                        ],
                        contactosIris: [],
                        argosConsultado: true,
                    }),
                    true
                );
            }
            return false;
        }

        if (metodo !== "POST") return false;
        enviados.push({ ruta, cuerpo, idempotencia });
        const version = cuerpo.version as number | undefined;
        const conflicto = () => problema(route, P("conflicto_version", 409, "Alguien más cambió la conversación"));
        const yo = actorId();
        const responder = async (cambio: () => void) => {
            if (version !== undefined && version !== c.version) return (await conflicto(), true);
            cambio();
            c.version += 1;
            emitir("conversacion_actualizada", c);
            return (await json(route, c), true);
        };
        switch (accion) {
            case "tomar":
                if (!puede("conversaciones.tomar")) return (await prohibido(), true);
                if (c.asignado && c.asignado.id !== yo) return (await problema(route, P("ya_asignada", 409, "Ya está asignada", { errors: [{ field: "asignado", message: "ya asignada", value: c.asignado.nombre }] })), true);
                return responder(() => (c.asignado = agentes.find((a) => a.id === yo) ?? { id: yo, nombre: "Yo" }));
            case "asignar": {
                if (!puede("conversaciones.asignar")) return (await prohibido(), true);
                const id = cuerpo.usuarioId as number | null;
                return responder(() => (c.asignado = id === null ? null : (agentes.find((a) => a.id === id) ?? null)));
            }
            case "transferir": {
                const cola = colas.find((x) => x.id === cuerpo.colaId);
                return responder(() => {
                    c.cola = cola ? { id: cola.id, nombre: cola.nombre } : null;
                    if (!cuerpo.conservarAsignacion) c.asignado = null;
                });
            }
            case "etiquetar":
                return responder(() => (c.etiquetas = etiquetas.filter((e) => (cuerpo.etiquetaIds as number[]).includes(e.id)).map(({ id, nombre, color }) => ({ id, nombre, color }))));
            case "resolver":
                return responder(() => (c.estado = "resuelta"));
            case "reabrir":
                return responder(() => (c.estado = "abierta"));
            case "desasociar":
                return responder(() => (c.contacto = { tipo: "desconocido", clienteId: null, servicioId: null, contactoIrisId: null, prospectoId: null, asociacion: null }));
            case "asociar-cliente":
                return responder(() => {
                    c.contacto = { tipo: "cliente", clienteId: cuerpo.clienteId as number, servicioId: (cuerpo.servicioId as number | undefined) ?? null, contactoIrisId: null, prospectoId: null, asociacion: "manual" };
                    c.titulo = cuerpo.clienteId === 5101 ? "Luis Herrera" : "Lucía Herrera";
                });
            case "contacto-iris":
                return responder(() => {
                    c.contacto = { tipo: "contacto_iris", clienteId: null, servicioId: null, contactoIrisId: 77, prospectoId: null, asociacion: "manual" };
                    c.titulo = String(cuerpo.nombre);
                });
            case "leer":
                c.noLeidos = 0;
                return (await json(route, { conversacionId: c.id, noLeidos: 0 }), true);
            case "enviar-manual": {
                const manual = manuales.find((v) => v.id === cuerpo.manualId);
                if (manual?.visibilidad !== "cliente") return (await problema(route, P("manual_interno", 422, "Es un manual interno")), true);
                return (await json(route, { id: cuerpo.id }, 201), true);
            }
            case "crear-ticket":
                return (await json(route, { vinculo: { tipo: "ticket", id: "4900", referencia: "TK-004900", creadoEn: new Date().toISOString() }, conversacion: detalle(c) }, 201), true);
            case "mensajes":
            case "notas": {
                if (!puede("conversaciones.responder")) return (await prohibido(), true);
                const id = String(cuerpo.id);
                const previo = mensajes.get(c.id)?.find((x) => x.id === id);
                if (previo) return (await json(route, previo), true);
                if (accion === "mensajes" && !cuerpo.plantilla && !c.ventana.abierta) return (await problema(route, P("fuera_de_ventana", 422, "La ventana de 24 h está cerrada")), true);
                seq += 1;
                const nuevo: Mensaje = {
                    id,
                    conversacionId: c.id,
                    seq,
                    direccion: accion === "notas" ? "nota" : "saliente",
                    tipo: cuerpo.plantilla ? "plantilla" : "texto",
                    texto: (cuerpo.texto as string | undefined) ?? null,
                    autor: agentes.find((a) => a.id === yo) ?? { id: yo, nombre: "Yo" },
                    estadoEntrega: accion === "notas" ? null : "en_cola",
                    motivoFalla: null,
                    menciones: ((cuerpo.menciones as number[] | undefined) ?? []).map((u) => ({ id: u, nombre: agentes.find((a) => a.id === u)?.nombre ?? "" })),
                    creadoEn: new Date().toISOString(),
                    enviadoEn: null,
                    entregadoEn: null,
                    leidoEn: null,
                    ...(cuerpo.plantilla ? { plantilla: cuerpo.plantilla as Schema<"PlantillaMensaje"> } : {}),
                };
                mensajes.get(c.id)?.push(nuevo);
                if (accion === "mensajes") {
                    c.estado = "esperando_cliente";
                    c.ultimoMensaje = { direccion: "saliente", tipo: nuevo.tipo, texto: nuevo.texto, evento: null, creadoEn: nuevo.creadoEn };
                    c.ultimaActividad = nuevo.creadoEn;
                    c.version += 1;
                }
                emitir("mensaje_nuevo", c, nuevo);
                return (await json(route, nuevo, 201), true);
            }
            default:
                return false;
        }
    }

    /** Administración: horarios, manuales, configurar números y respuestas rápidas. */
    async function administrar(route: Route, metodo: string, ruta: string, query: URLSearchParams, cuerpo: Record<string, unknown>, puede: (p: Permiso) => boolean): Promise<boolean> {
        const prohibido = async () => (await problema(route, P("permiso_denegado", 403, "Sin permiso")), true);
        const noExiste = async () => (await problema(route, P("no_encontrado", 404, "No existe")), true);
        const conflicto = async () => (await problema(route, P("conflicto_version", 409, "Cambió")), true);
        if (ruta === `${BASE}/horarios` || ruta.startsWith(`${BASE}/horarios/`)) {
            const id = Number(ruta.split("/")[5]);
            if (metodo === "GET" && ruta === `${BASE}/horarios`) return (await json(route, { data: horarios, nextCursor: null }), true);
            const h = horarios.find((v) => v.id === id);
            if (metodo === "GET") return h ? (await json(route, h), true) : noExiste();
            if (!puede("canales.configurar")) return prohibido();
            if (ruta === `${BASE}/horarios`) {
                const nuevo = { ...(cuerpo as unknown as Schema<"Horario">), id: horarios.length + 10, version: 1 };
                horarios.push(nuevo);
                return (await json(route, nuevo, 201), true);
            }
            if (!h) return noExiste();
            if (cuerpo.version !== h.version) return conflicto();
            Object.assign(h, cuerpo, { version: h.version + 1 });
            return (await json(route, h), true);
        }
        if (ruta === `${BASE}/manuales` || ruta.startsWith(`${BASE}/manuales/`)) {
            if (!puede("manuales.ver")) return prohibido();
            const partes = ruta.split("/");
            const id = Number(partes[5]);
            if (metodo === "GET" && ruta === `${BASE}/manuales`) {
                const q = (query.get("q") ?? "").toLowerCase();
                const vis = query.get("visibilidad");
                const data = manuales.filter(
                    (m) => (!q || `${m.titulo} ${m.resumen} ${m.cuerpo} ${m.etiquetas.join(" ")} ${m.equipo}`.toLowerCase().includes(q)) && (!vis || m.visibilidad === vis) && (m.activo || query.get("incluirInactivos")),
                );
                return (await json(route, { data, nextCursor: null }), true);
            }
            const m = manuales.find((v) => v.id === id);
            if (metodo === "GET") return m ? (await json(route, m), true) : noExiste();
            if (!puede("manuales.editar")) return prohibido();
            if (ruta === `${BASE}/manuales`) {
                const nuevo = { ...(cuerpo as unknown as Schema<"Manual">), id: manuales.length + 10, activo: true, version: 1 };
                manuales.push(nuevo);
                return (await json(route, nuevo, 201), true);
            }
            if (!m) return noExiste();
            if (cuerpo.version !== m.version) return conflicto();
            const accion = partes[6];
            if (accion === "activar") m.activo = true;
            else if (accion === "desactivar") m.activo = false;
            else Object.assign(m, cuerpo);
            m.version += 1;
            return (await json(route, m), true);
        }
        const conf = /^\/api\/v1\/mensajeria\/canales\/(\d+)\/configurar$/.exec(ruta);
        if (conf && metodo === "POST") {
            if (!puede("canales.configurar")) return prohibido();
            const c = canales.find((v) => v.id === Number(conf[1]));
            if (!c) return noExiste();
            if (cuerpo.version !== c.version) return conflicto();
            configurados.push(cuerpo);
            if (cuerpo.avisosEmpresa) for (const otro of canales) otro.avisosEmpresa = false;
            Object.assign(c, cuerpo, { version: c.version + 1 });
            return (await json(route, c), true);
        }
        const rr = /^\/api\/v1\/mensajeria\/respuestas-rapidas(?:\/(\d+)\/(actualizar|activar|desactivar))?$/.exec(ruta);
        if (rr && metodo === "POST") {
            if (!puede("conversaciones.configurar")) return prohibido();
            if (!rr[1]) {
                if (respuestas.some((v) => v.atajo === cuerpo.atajo)) {
                    return (await problema(route, P("atajo_existente", 422, "Atajo repetido", { errors: [{ field: "body.atajo", message: "Ya hay una respuesta con ese atajo" }] })), true);
                }
                const nueva = { ...(cuerpo as unknown as Schema<"RespuestaRapida">), id: respuestas.length + 10, activa: true, version: 1 };
                respuestas.push(nueva);
                return (await json(route, nueva, 201), true);
            }
            const rq = respuestas.find((v) => v.id === Number(rr[1]));
            if (!rq) return noExiste();
            if (cuerpo.version !== rq.version) return conflicto();
            if (rr[2] === "activar") rq.activa = true;
            else if (rr[2] === "desactivar") rq.activa = false;
            else Object.assign(rq, cuerpo);
            rq.version += 1;
            return (await json(route, rq), true);
        }
        return false;
    }

    return { manejar, conversaciones, mensajes, enviados, mensajeDelCliente, horarios, manuales, canales, configurados, respuestas };
}

export type ApiConversaciones = ReturnType<typeof apiConversaciones>;
