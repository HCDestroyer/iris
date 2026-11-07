import type { Schema } from "@iris/contratos";
import type { Page, Route } from "@playwright/test";

/*
 * Chat del equipo para las pruebas del panel (`contratos/chat.md`, con lo del panel: menciones,
 * miembros, solo lectura, resumen, personas y cuadrillas). Tipado con `@iris/contratos`. Se registra
 * desde `simularApi(page, { chat: true })` después de la ruta general, así que atiende primero
 * `/api/v1/mensajeria/chat/*`. `/eventos` entrega lo pendiente desde `?desde` y se cierra (el motor
 * vuelve a abrirlo o sondea `/cambios` con el mismo cursor).
 */

type Conversacion = Schema<"ConversacionChat">;
type Mensaje = Schema<"MensajeChat">;
type Evento = Schema<"EventoChat">;
type Miembro = Schema<"MiembroChat">;
type Lectura = Schema<"LecturaMiembroChat">;
type Problema = Schema<"Problema">;

export const YO_CHAT = { id: 1, nombre: "Ana López" };
export const MARIO = { id: 50, nombre: "Mario Ixcot" };
export const LAURA = { id: 51, nombre: "Laura Méndez" };
export const PEDRO = { id: 52, nombre: "Pedro Gómez" };
export const ANDREA = { id: 53, nombre: "Andrea Castillo" };
const PERSONAS = [MARIO, LAURA, PEDRO, ANDREA];

/** Conversaciones del escenario. */
export const CONV = { ordenViva: 21, directaMario: 22, cuadrillaNorte: 23, directaLaura: 24, ordenCerrada: 25, cuadrillaSur: 26 } as const;
/** Órdenes del escenario de órdenes (`datos-ordenes.ts`) con hilo: la 5 en sitio y la 8 cerrada. */
export const ORDEN_VIVA = 5;
export const ORDEN_CERRADA = 8;
export const ORDEN_SIN_CHAT = 2;

export type ApiChatPanel = {
    conversaciones: Map<number, Conversacion>;
    mensajes: Map<number, Mensaje[]>;
    /** Peticiones al chat: método, ruta (sin la base) y cuerpo. */
    peticiones: { metodo: string; ruta: string; cuerpo: unknown }[];
    /** Mientras sea true, `/eventos` y `/cambios` responden 503 (el tiempo real se cae). */
    caido: boolean;
    /** Llega un mensaje de otra persona (al feed). */
    recibir: (conversacionId: number, autor: { id: number; nombre: string }, texto: string, menciones?: { id: number; nombre: string }[]) => Mensaje;
    /** Eliminaciones que llegaron al servidor (ids). */
    eliminados: string[];
};

const BASE = "/api/v1/mensajeria/chat";
const ahoraIso = () => new Date().toISOString();
const haceMin = (min: number) => new Date(Date.now() - min * 60_000).toISOString();

let siguienteMensaje = 1;
function idMensaje(): string {
    const n = String(siguienteMensaje++).padStart(12, "0");
    return `0193a000-0000-7000-8000-${n}`;
}

export async function simularChatPanel(page: Page): Promise<ApiChatPanel> {
    const conversaciones = new Map<number, Conversacion>();
    const mensajes = new Map<number, Mensaje[]>();
    const lecturas = new Map<number, Lectura[]>();
    const agregados = new Map<number, Miembro[]>();
    const eventos: Evento[] = [];
    const api: ApiChatPanel = { conversaciones, mensajes, peticiones: [], caido: false, eliminados: [], recibir: () => ({}) as Mensaje };

    const conv = (c: Partial<Conversacion> & Pick<Conversacion, "id" | "tipo" | "titulo">): Conversacion => {
        const nueva: Conversacion = {
            empresa_id: null,
            cuadrilla_id: null,
            orden_id: null,
            orden_estado: null,
            orden_tipo: null,
            solo_lectura: false,
            no_leidos: 0,
            menciones: 0,
            leido_hasta_mensaje_id: null,
            ultima_actividad: ahoraIso(),
            creada_en: haceMin(600),
            ...c,
        };
        conversaciones.set(nueva.id, nueva);
        mensajes.set(nueva.id, mensajes.get(nueva.id) ?? []);
        return nueva;
    };

    const previa = (m: Mensaje): Schema<"VistaPreviaChat"> => ({
        id: m.id,
        autor_id: m.autor_id,
        autor_nombre: m.autor_nombre,
        contenido: m.eliminado ? "eliminado" : m.texto ? "texto" : m.ubicacion ? "ubicacion" : "texto",
        texto: m.eliminado ? null : (m.texto?.slice(0, 140) ?? null),
        enviado_en: m.enviado_en,
    });

    const poner = (conversacionId: number, autor: { id: number; nombre: string }, d: Partial<Mensaje>, hace: number): Mensaje => {
        const lista = mensajes.get(conversacionId) ?? [];
        const m: Mensaje = {
            id: idMensaje(),
            conversacion_id: conversacionId,
            seq: lista.length + 1,
            autor_id: autor.id,
            autor_nombre: autor.nombre,
            texto: null,
            creado_en: haceMin(hace),
            enviado_en: haceMin(hace),
            eliminado: false,
            eliminado_en: null,
            menciones: [],
            ...d,
        };
        lista.push(m);
        mensajes.set(conversacionId, lista);
        const c = conversaciones.get(conversacionId);
        if (c) {
            c.ultimo_mensaje = previa(m);
            c.ultima_actividad = m.enviado_en;
        }
        return m;
    };

    const emitir = (e: Omit<Evento, "id" | "ocurrido_en">) => {
        eventos.push({ ...e, id: String(eventos.length + 1), ocurrido_en: ahoraIso() } as Evento);
    };

    // --- Escenario ---------------------------------------------------------------------------
    conv({ id: CONV.ordenViva, tipo: "orden", titulo: "OT-000005", orden_id: ORDEN_VIVA, orden_estado: "en_sitio", orden_tipo: "Mantenimiento" });
    poner(CONV.ordenViva, MARIO, { texto: "Llegué. La caja NAP 14 tiene el puerto 3 con el conector quemado." }, 40);
    poner(CONV.ordenViva, MARIO, { ubicacion: { lat: 14.5279, lon: -90.5878 } }, 38);
    const propio = poner(CONV.ordenViva, YO_CHAT, { texto: "@Mario Ixcot la bodega de Villa Nueva ya tiene conectores SC/APC.", menciones: [{ ...MARIO }] }, 4);
    lecturas.set(CONV.ordenViva, [{ usuario_id: MARIO.id, nombre: MARIO.nombre, hasta_mensaje_id: propio.id, hasta_seq: propio.seq, leido_en: haceMin(3) }]);
    const ultimo = poner(CONV.ordenViva, MARIO, { texto: "Ya quedó, −19.8 dBm. @Ana López ¿cierro la orden en la app?", menciones: [{ ...YO_CHAT }] }, 2);
    Object.assign(conversaciones.get(CONV.ordenViva) ?? {}, { no_leidos: 1, menciones: 1, leido_hasta_mensaje_id: propio.id });
    void ultimo;

    conv({ id: CONV.directaMario, tipo: "directa", titulo: MARIO.nombre, otro_usuario: { ...MARIO } });
    poner(CONV.directaMario, MARIO, { texto: "Buenos días, ¿me confirma la dirección de la siguiente?" }, 30);
    poner(CONV.directaMario, MARIO, { texto: "La de San Lucas, la del cliente nuevo." }, 29);
    Object.assign(conversaciones.get(CONV.directaMario) ?? {}, { no_leidos: 2 });

    conv({ id: CONV.cuadrillaNorte, tipo: "cuadrilla", titulo: "Cuadrilla Norte", cuadrilla_id: 1 });
    poner(CONV.cuadrillaNorte, ANDREA, { texto: "La caja NAP 14 tiene la potencia baja en todos los puertos." }, 60);
    poner(CONV.cuadrillaNorte, PEDRO, { texto: "Voy para allá después de la de las 11." }, 55);
    poner(CONV.cuadrillaNorte, ANDREA, { texto: "Gracias, Pedro." }, 50);
    Object.assign(conversaciones.get(CONV.cuadrillaNorte) ?? {}, { no_leidos: 3 });

    conv({ id: CONV.directaLaura, tipo: "directa", titulo: LAURA.nombre, otro_usuario: { ...LAURA } });
    poner(CONV.directaLaura, LAURA, { texto: "Revisa la ruta de Pedro para la tarde." }, 60 * 20);

    conv({ id: CONV.ordenCerrada, tipo: "orden", titulo: "OT-000008", orden_id: ORDEN_CERRADA, orden_estado: "cerrada", orden_tipo: "Retiro", solo_lectura: true });
    poner(CONV.ordenCerrada, PEDRO, { texto: "Equipo retirado y en bodega." }, 60 * 22);

    conv({ id: CONV.cuadrillaSur, tipo: "cuadrilla", titulo: "Cuadrilla Sur", cuadrilla_id: 2 });
    poner(CONV.cuadrillaSur, PEDRO, { texto: "Sin novedad en San Lucas." }, 60 * 23);

    for (const [id, lista] of mensajes) {
        const c = conversaciones.get(id);
        if (c && c.no_leidos === 0) c.leido_hasta_mensaje_id = lista.at(-1)?.id ?? null;
    }

    api.recibir = (conversacionId, autor, texto, menciones = []) => {
        const m = poner(conversacionId, autor, { texto, menciones, creado_en: ahoraIso(), enviado_en: ahoraIso() }, 0);
        const c = conversaciones.get(conversacionId);
        if (c) {
            c.no_leidos += 1;
            if (menciones.some((x) => x.id === YO_CHAT.id)) c.menciones += 1;
        }
        emitir({ tipo: "mensaje_nuevo", conversacion_id: conversacionId, no_leidos: c?.no_leidos ?? 0, menciones: c?.menciones ?? 0, mensaje: structuredClone(m) });
        return m;
    };

    // --- Lo que se deriva --------------------------------------------------------------------
    const miembrosDe = (c: Conversacion): Miembro[] => {
        const yo: Miembro = { usuario_id: YO_CHAT.id, nombre: YO_CHAT.nombre, motivo: c.tipo === "directa" ? "directa" : c.tipo === "orden" ? "despacho" : "supervision" };
        const base: Miembro[] =
            c.tipo === "directa"
                ? [yo, { usuario_id: c.otro_usuario?.id ?? 0, nombre: c.otro_usuario?.nombre ?? "", motivo: "directa" }]
                : c.tipo === "orden"
                  ? [{ usuario_id: c.orden_id === ORDEN_VIVA ? MARIO.id : PEDRO.id, nombre: c.orden_id === ORDEN_VIVA ? MARIO.nombre : PEDRO.nombre, motivo: "tecnico" }, yo, { usuario_id: LAURA.id, nombre: LAURA.nombre, motivo: "supervision" }]
                  : [
                        { usuario_id: ANDREA.id, nombre: ANDREA.nombre, motivo: "tecnico" },
                        { usuario_id: PEDRO.id, nombre: PEDRO.nombre, motivo: "tecnico" },
                        yo,
                    ];
        return [...base, ...(agregados.get(c.id) ?? [])];
    };

    const contadores = (lista: Conversacion[]): Schema<"ContadoresChat"> => ({
        no_leidos: lista.reduce((n, c) => n + c.no_leidos, 0),
        menciones: lista.reduce((n, c) => n + c.menciones, 0),
        conversaciones_no_leidas: lista.filter((c) => c.no_leidos > 0).length,
        requiere_accion: lista.reduce((n, c) => n + (c.tipo === "directa" ? c.no_leidos : c.menciones), 0),
    });

    const detalle = (c: Conversacion): Schema<"ConversacionChatDetalle"> => ({ ...structuredClone(c), lecturas: structuredClone(lecturas.get(c.id) ?? []) });

    const json = (route: Route, cuerpo: unknown, status = 200) => route.fulfill({ status, contentType: "application/json", body: JSON.stringify(cuerpo) });
    const problema = (route: Route, p: Problema) => route.fulfill({ status: p.status, contentType: "application/problem+json", body: JSON.stringify(p) });
    const p = (status: number, codigo: string, title: string): Problema => ({ type: `urn:iris:problema:${codigo}`, title, status });

    await page.route(
        (url) => url.pathname.startsWith(`${BASE}/`),
        async (route) => {
            const req = route.request();
            const url = new URL(req.url());
            const ruta = url.pathname.slice(BASE.length);
            const metodo = req.method();
            let cuerpo: unknown = undefined;
            try {
                cuerpo = req.postDataJSON();
            } catch {
                cuerpo = undefined;
            }
            api.peticiones.push({ metodo, ruta, cuerpo });
            const q = url.searchParams;
            const num = (re: RegExp) => Number(re.exec(ruta)?.[1]);

            if (metodo === "GET" && ruta === "/resumen") {
                const todas = [...conversaciones.values()];
                const de = (t: Conversacion["tipo"]) => contadores(todas.filter((c) => c.tipo === t));
                return json(route, { ...contadores(todas), por_tipo: { directa: de("directa"), cuadrilla: de("cuadrilla"), orden: de("orden") } } satisfies Schema<"ResumenChat">);
            }
            if (metodo === "GET" && ruta === "/conversaciones") {
                const buscado = (q.get("q") ?? "").toLowerCase();
                const data = [...conversaciones.values()]
                    .filter((c) => !buscado || c.titulo.toLowerCase().includes(buscado))
                    .sort((a, b) => Date.parse(b.ultima_actividad) - Date.parse(a.ultima_actividad))
                    .map((c) => structuredClone(c));
                return json(route, { data, nextCursor: null } satisfies Schema<"PaginaConversacionChat">);
            }
            if (metodo === "GET" && /^\/conversaciones\/\d+$/.test(ruta)) {
                const c = conversaciones.get(num(/(\d+)/));
                return c ? json(route, detalle(c)) : problema(route, p(404, "no_encontrado", "No encontrada"));
            }
            if (/^\/conversaciones\/\d+\/miembros$/.test(ruta)) {
                const c = conversaciones.get(num(/(\d+)/));
                if (!c) return problema(route, p(404, "no_encontrado", "No encontrada"));
                if (metodo === "GET") return json(route, { data: miembrosDe(c) } satisfies Schema<"MiembrosChat">);
                if (c.tipo !== "orden") return problema(route, p(422, "tipo_no_admite_miembros", "Solo hilos de orden"));
                if (c.solo_lectura) return problema(route, p(422, "conversacion_cerrada", "La orden está cerrada"));
                const id = (cuerpo as Schema<"CuerpoAgregarMiembro">).usuario_id;
                const persona = PERSONAS.find((x) => x.id === id);
                if (!persona) return problema(route, p(422, "usuario_no_disponible", "No disponible"));
                if (miembrosDe(c).some((m) => m.usuario_id === id)) return json(route, { data: miembrosDe(c) });
                agregados.set(c.id, [...(agregados.get(c.id) ?? []), { usuario_id: persona.id, nombre: persona.nombre, motivo: "agregado" }]);
                return json(route, { data: miembrosDe(c) } satisfies Schema<"MiembrosChat">, 201);
            }
            if (metodo === "GET" && /^\/conversaciones\/\d+\/mensajes$/.test(ruta)) {
                const lista = mensajes.get(num(/(\d+)/));
                if (!lista) return problema(route, p(404, "no_encontrado", "No encontrada"));
                const desde = Number(q.get("desde_seq") ?? 0);
                const data = q.get("direccion") === "siguientes" ? lista.filter((m) => m.seq > desde) : lista.slice(-50);
                return json(route, { data: structuredClone(data), nextCursor: null } satisfies Schema<"PaginaMensajeChat">);
            }
            if (metodo === "POST" && /^\/conversaciones\/\d+\/mensajes$/.test(ruta)) {
                const id = num(/(\d+)/);
                const c = conversaciones.get(id);
                const lista = mensajes.get(id);
                if (!c || !lista) return problema(route, p(404, "no_encontrado", "No encontrada"));
                const e = cuerpo as Schema<"CuerpoEnviarMensaje">;
                const previo = lista.find((m) => m.id === e.id);
                if (previo) return json(route, previo, 200);
                if (c.solo_lectura) return problema(route, p(422, "conversacion_cerrada", "La orden está cerrada"));
                const visibles = new Set(miembrosDe(c).map((m) => m.usuario_id));
                const ids = [...new Set(e.menciones ?? [])];
                if (ids.some((m) => !visibles.has(m) || m === YO_CHAT.id)) return problema(route, p(422, "mencion_invalida", "Mención inválida"));
                const m = poner(
                    id,
                    YO_CHAT,
                    {
                        id: e.id,
                        texto: e.texto ?? null,
                        creado_en: e.creado_en ?? null,
                        enviado_en: ahoraIso(),
                        menciones: ids.map((x) => ({ id: x, nombre: PERSONAS.find((y) => y.id === x)?.nombre ?? "" })),
                        ...(e.ubicacion ? { ubicacion: e.ubicacion } : {}),
                    },
                    0,
                );
                emitir({ tipo: "mensaje_nuevo", conversacion_id: id, no_leidos: c.no_leidos, menciones: c.menciones, mensaje: structuredClone(m) });
                return json(route, m, 201);
            }
            if (metodo === "POST" && /^\/conversaciones\/\d+\/leer$/.test(ruta)) {
                const id = num(/(\d+)/);
                const c = conversaciones.get(id);
                if (!c) return problema(route, p(404, "no_encontrado", "No encontrada"));
                c.no_leidos = 0;
                c.menciones = 0;
                c.leido_hasta_mensaje_id = (cuerpo as { hasta_mensaje_id: string }).hasta_mensaje_id;
                return json(route, { conversacion_id: id, no_leidos: 0, leido_hasta_mensaje_id: c.leido_hasta_mensaje_id } satisfies Schema<"LecturaPropia">);
            }
            if (metodo === "POST" && /^\/mensajes\/[^/]+\/eliminar$/.test(ruta)) {
                const id = decodeURIComponent(/^\/mensajes\/([^/]+)\//.exec(ruta)?.[1] ?? "");
                for (const lista of mensajes.values()) {
                    const m = lista.find((x) => x.id === id);
                    if (!m) continue;
                    if (m.autor_id !== YO_CHAT.id) return problema(route, p(403, "no_es_autor", "No es tuyo"));
                    Object.assign(m, { eliminado: true, eliminado_en: ahoraIso(), texto: null, menciones: [] });
                    api.eliminados.push(id);
                    const c = conversaciones.get(m.conversacion_id);
                    if (c && c.ultimo_mensaje?.id === m.id) c.ultimo_mensaje = previa(m);
                    emitir({ tipo: "mensaje_eliminado", conversacion_id: m.conversacion_id, no_leidos: c?.no_leidos ?? 0, menciones: c?.menciones ?? 0, mensaje: structuredClone(m) });
                    return json(route, m);
                }
                return problema(route, p(404, "no_encontrado", "No encontrado"));
            }
            if (metodo === "GET" && /^\/ordenes\/\d+\/conversacion$/.test(ruta)) {
                const ordenId = num(/(\d+)/);
                const c = [...conversaciones.values()].find((x) => x.tipo === "orden" && x.orden_id === ordenId);
                return c ? json(route, detalle(c)) : problema(route, p(404, "no_encontrado", "Sin chat"));
            }
            if (metodo === "POST" && /^\/ordenes\/\d+\/abrir$/.test(ruta)) {
                const ordenId = num(/(\d+)/);
                const existe = [...conversaciones.values()].find((x) => x.tipo === "orden" && x.orden_id === ordenId);
                const c = existe ?? conv({ id: 100 + ordenId, tipo: "orden", titulo: `OT-${String(ordenId).padStart(6, "0")}`, orden_id: ordenId, orden_estado: "pendiente", orden_tipo: "Visita técnica" });
                return json(route, detalle(c), existe ? 200 : 201);
            }
            if (metodo === "POST" && ruta === "/directas") {
                const id = (cuerpo as { usuario_id: number }).usuario_id;
                const persona = PERSONAS.find((x) => x.id === id);
                if (!persona) return problema(route, p(422, "usuario_no_disponible", "No disponible"));
                const existe = [...conversaciones.values()].find((x) => x.tipo === "directa" && x.otro_usuario?.id === id);
                const c = existe ?? conv({ id: 200 + id, tipo: "directa", titulo: persona.nombre, otro_usuario: { ...persona } });
                return json(route, detalle(c), existe ? 200 : 201);
            }
            if (metodo === "POST" && /^\/cuadrillas\/\d+\/abrir$/.test(ruta)) {
                const id = num(/(\d+)/);
                const existe = [...conversaciones.values()].find((x) => x.tipo === "cuadrilla" && x.cuadrilla_id === id);
                const c = existe ?? conv({ id: 300 + id, tipo: "cuadrilla", titulo: id === 3 ? "Cuadrilla Centro" : `Cuadrilla ${id}`, cuadrilla_id: id });
                return json(route, detalle(c), existe ? 200 : 201);
            }
            if (metodo === "GET" && ruta === "/personas") {
                const buscado = (q.get("q") ?? "").toLowerCase();
                const data = PERSONAS.filter((x) => !buscado || x.nombre.toLowerCase().includes(buscado)).map((x) => ({
                    ...x,
                    conversacion_id: [...conversaciones.values()].find((c) => c.tipo === "directa" && c.otro_usuario?.id === x.id)?.id ?? null,
                }));
                return json(route, { data, nextCursor: null } satisfies Schema<"PaginaPersonaChat">);
            }
            if (metodo === "GET" && ruta === "/cuadrillas") {
                const buscado = (q.get("q") ?? "").toLowerCase();
                const data: Schema<"CuadrillaChat">[] = [
                    { id: 1, nombre: "Cuadrilla Norte", tecnicos: 5, zonas: ["Villa Nueva", "Mixco"], conversacion_id: CONV.cuadrillaNorte },
                    { id: 2, nombre: "Cuadrilla Sur", tecnicos: 4, zonas: ["San Lucas", "Amatitlán"], conversacion_id: CONV.cuadrillaSur },
                    { id: 3, nombre: "Cuadrilla Centro", tecnicos: 3, zonas: ["Zona 1"], conversacion_id: [...conversaciones.values()].find((c) => c.cuadrilla_id === 3)?.id ?? null },
                ].filter((c) => !buscado || c.nombre.toLowerCase().includes(buscado));
                return json(route, { data } satisfies Schema<"CuadrillasChat">);
            }
            if (metodo === "GET" && ruta === "/cambios") {
                if (api.caido) return problema(route, p(503, "no_disponible", "No disponible"));
                const cursor = q.get("cursor");
                if (cursor === null) return json(route, { data: [], nextCursor: String(eventos.length), hayMas: false } satisfies Schema<"CambiosChat">);
                const data = eventos.filter((e) => Number(e.id) > Number(cursor));
                return json(route, { data, nextCursor: data.at(-1)?.id ?? cursor, hayMas: false } satisfies Schema<"CambiosChat">);
            }
            if (metodo === "GET" && ruta === "/eventos") {
                if (api.caido) return problema(route, p(503, "flujos_agotados", "No disponible"));
                const desde = q.get("desde") ?? (await req.headerValue("last-event-id"));
                const pendientes = desde === null ? [] : eventos.filter((e) => Number(e.id) > Number(desde));
                const texto = ["retry: 3000", "", ...pendientes.flatMap((e) => [`id: ${e.id}`, `event: ${e.tipo}`, `data: ${JSON.stringify(e)}`, ""]), ": latido", "", ""].join("\n");
                return route.fulfill({ status: 200, headers: { "Content-Type": "text/event-stream", "Cache-Control": "no-store" }, body: texto });
            }
            return problema(route, p(501, "no_simulado", `Sin simular: ${metodo} ${ruta}`));
        },
    );
    return api;
}
