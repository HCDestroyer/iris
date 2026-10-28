import type { Schema } from "@iris/contratos";
import type { BrowserContext, Page, Route } from "@playwright/test";
import { CATEGORIAS, CLIENTE, CSRF_PORTAL, escenarioPortal, TICKET_LIMITE, TICKET_RED, TICKET_VALIDO, TICKET_VENCIDO } from "./datos-portal";

/*
 * API falsa del portal (/api/v1/portal/*) como la describe i1.md / i2.md: cookie HttpOnly
 * `iris_portal`, CSRF propio `iris_portal_csrf` → X-CSRF-Token (403 si falta), sin
 * X-Iris-Empresa, 401 sin sesión, cursor, version → 409 y topes 429. Cualquier otra ruta de la
 * API responde 401 (el portal no debe llamar al panel).
 */

type Problema = Schema<"Problema">;

export type PeticionPortal = {
    metodo: string;
    ruta: string;
    url: string;
    csrf: string | undefined;
    empresa: string | undefined;
    idempotencia: string | undefined;
    cuerpo: unknown;
};

export type ApiPortal = {
    peticiones: PeticionPortal[];
    /** Responde `p` en las próximas `n` peticiones `metodo` cuya ruta empiece con `prefijo`. */
    responderProblema: (metodo: string, prefijo: string, p: Problema, n?: number, cabeceras?: Record<string, string>) => void;
    /** Alguien del equipo cambió el ticket: sube su versión. */
    cambiarVersion: (id: number) => void;
    /** La sesión vence: toda petición con sesión responde 401. */
    vencerSesion: () => void;
};

const PORTAL = "/api/v1/portal";

function problema(route: Route, p: Problema, cabeceras: Record<string, string> = {}) {
    return route.fulfill({ status: p.status, contentType: "application/problem+json", headers: cabeceras, body: JSON.stringify(p) });
}

const json = (route: Route, cuerpo: unknown, status = 200) => route.fulfill({ status, contentType: "application/json", body: JSON.stringify(cuerpo) });

/** Con sesión del portal ya abierta (cookies como las deja el canje). */
export async function conSesionPortal(context: BrowserContext, baseURL: string) {
    await context.addCookies([
        { name: "iris_portal", value: `c:${CLIENTE.clienteId}`, url: baseURL, httpOnly: true, sameSite: "Lax" },
        { name: "iris_portal_csrf", value: CSRF_PORTAL, url: baseURL, sameSite: "Lax" },
    ]);
}

export async function simularApiPortal(page: Page): Promise<ApiPortal> {
    const esc = escenarioPortal();
    const peticiones: PeticionPortal[] = [];
    const forzados: { metodo: string; prefijo: string; p: Problema; n: number; cabeceras: Record<string, string> }[] = [];
    let vencida = false;
    let fallosRed = 1;

    await page.route("**/api/v1/**", async (route) => {
        const req = route.request();
        const url = new URL(req.url());
        const ruta = url.pathname;
        const metodo = req.method();
        const h = await req.allHeaders();
        let cuerpo: unknown;
        try {
            cuerpo = req.postDataJSON();
        } catch {
            cuerpo = undefined;
        }
        peticiones.push({ metodo, ruta, url: req.url(), csrf: h["x-csrf-token"], empresa: h["x-iris-empresa"], idempotencia: h["idempotency-key"], cuerpo });
        const sesion = /(?:^|;\s*)iris_portal=([^;]+)/.exec(h.cookie ?? "")?.[1];
        const noAutenticado = () => problema(route, { type: "urn:iris:problema:no_autenticado", title: "No autenticado", status: 401 });

        if (!ruta.startsWith(`${PORTAL}/`)) {
            return noAutenticado();
        }
        const forzado = forzados.find((f) => f.n > 0 && f.metodo === metodo && ruta.startsWith(f.prefijo));
        if (forzado) {
            forzado.n--;
            return problema(route, forzado.p, forzado.cabeceras);
        }

        if (metodo === "POST" && ruta === `${PORTAL}/sesion`) {
            const { ticket } = cuerpo as Schema<"EntradaEntrarPortal">;
            if (ticket === TICKET_RED && fallosRed > 0) {
                fallosRed--;
                return route.abort("connectionrefused");
            }
            if (ticket === TICKET_LIMITE) {
                return problema(route, { type: "urn:iris:problema:demasiados_intentos", title: "Demasiados intentos", status: 429 }, { "Retry-After": "120" });
            }
            if (ticket !== TICKET_VALIDO && ticket !== TICKET_RED) {
                return problema(route, { type: "urn:iris:problema:ticket_invalido", title: "Enlace inválido", status: 401, detail: "El enlace venció o ya se usó" });
            }
            const respuesta: Schema<"SesionPortal"> = { csrfToken: CSRF_PORTAL, expiraEn: "2026-10-28T17:00:00Z", cliente: CLIENTE };
            return route.fulfill({
                status: 200,
                contentType: "application/json",
                headers: {
                    "Set-Cookie": [`iris_portal=c:${CLIENTE.clienteId}; Path=/; HttpOnly; SameSite=Lax`, `iris_portal_csrf=${CSRF_PORTAL}; Path=/; SameSite=Lax`].join("\n"),
                },
                body: JSON.stringify(respuesta),
            });
        }

        if (!sesion || vencida) {
            return noAutenticado();
        }
        if (metodo !== "GET" && h["x-csrf-token"] !== CSRF_PORTAL) {
            return problema(route, { type: "urn:iris:problema:csrf_invalido", title: "CSRF inválido", status: 403 });
        }
        if (h["x-iris-empresa"] !== undefined) {
            return problema(route, { type: "urn:iris:problema:empresa_no_permitida", title: "Empresa no permitida", status: 403 });
        }

        if (ruta === `${PORTAL}/sesion`) {
            if (metodo === "GET") {
                const r: Schema<"SesionPortalActual"> = { cliente: CLIENTE, expiraEn: "2026-10-28T17:00:00Z" };
                return json(route, r);
            }
            if (metodo === "DELETE") {
                return route.fulfill({
                    status: 204,
                    headers: { "Set-Cookie": ["iris_portal=; Path=/; Max-Age=0; HttpOnly", "iris_portal_csrf=; Path=/; Max-Age=0"].join("\n") },
                });
            }
        }

        if (metodo === "GET" && ruta === `${PORTAL}/categorias`) {
            const r: Schema<"PaginaCategoriaPortal"> = { data: CATEGORIAS, nextCursor: null };
            return json(route, r);
        }

        if (ruta === `${PORTAL}/tickets`) {
            if (metodo === "GET") {
                const estados = url.searchParams.get("estado")?.split(",");
                const limite = Number(url.searchParams.get("limit") ?? 20);
                const desde = Number(url.searchParams.get("cursor") ?? 0);
                const lista = esc.tickets.filter((t) => !estados || estados.includes(t.estado)).sort((a, b) => b.id - a.id);
                const pagina = lista.slice(desde, desde + limite);
                const r: Schema<"PaginaTicketPortal"> = { data: pagina, nextCursor: desde + limite < lista.length ? String(desde + limite) : null };
                return json(route, r);
            }
            if (metodo === "POST") {
                const entrada = cuerpo as Schema<"EntradaCrearTicketPortal">;
                if (!h["idempotency-key"]) {
                    return problema(route, { type: "urn:iris:problema:validacion", title: "Datos inválidos", status: 422 });
                }
                const id = esc.siguienteTicket++;
                const categoria = CATEGORIAS.find((c) => c.id === entrada.categoriaId);
                const nuevo: Schema<"TicketPortal"> = {
                    id,
                    numero: `TK-000${id}`,
                    asunto: entrada.asunto,
                    estado: "nuevo",
                    categoriaId: categoria?.id ?? null,
                    categoria: categoria?.nombre ?? null,
                    servicioId: null,
                    origen: "portal",
                    afectadoPorIncidente: false,
                    puedeConfirmar: false,
                    resueltoEn: null,
                    cerradoEn: null,
                    version: 1,
                    creadoEn: "2026-10-28T15:00:00Z",
                    actualizadoEn: "2026-10-28T15:00:00Z",
                };
                esc.tickets.push(nuevo);
                esc.mensajes.set(id, [
                    { id: esc.siguienteMensaje++, autorTipo: "cliente", autorNombre: CLIENTE.nombre, cuerpo: entrada.descripcion, adjuntos: [], creadoEn: "2026-10-28T15:00:00Z" },
                ]);
                return json(route, nuevo, 201);
            }
        }

        const m = new RegExp(`^${PORTAL}/tickets/(\\d+)(/[a-z-]+)?$`).exec(ruta);
        if (m) {
            const t = esc.tickets.find((x) => x.id === Number(m[1]));
            if (!t) {
                return problema(route, { type: "urn:iris:problema:no_encontrado", title: "No encontrado", status: 404 });
            }
            const accion = m[2];
            if (metodo === "GET" && !accion) {
                return json(route, t);
            }
            if (metodo === "GET" && accion === "/mensajes") {
                const r: Schema<"PaginaMensajePortal"> = { data: esc.mensajes.get(t.id) ?? [], nextCursor: null };
                return json(route, r);
            }
            const conflicto = () => problema(route, { type: "urn:iris:problema:conflicto_version", title: "Conflicto", status: 409, detail: "El ticket cambió" });
            if (metodo === "POST" && accion === "/responder") {
                const entrada = cuerpo as Schema<"EntradaResponderTicketPortal">;
                if (entrada.version !== undefined && entrada.version !== t.version) {
                    return conflicto();
                }
                if (t.estado === "cerrado") {
                    return problema(route, { type: "urn:iris:problema:transicion_invalida", title: "Transición inválida", status: 422 });
                }
                const mensaje: Schema<"MensajePortal"> = {
                    id: esc.siguienteMensaje++,
                    autorTipo: "cliente",
                    autorNombre: CLIENTE.nombre,
                    cuerpo: entrada.cuerpo,
                    adjuntos: [],
                    creadoEn: "2026-10-28T15:05:00Z",
                };
                esc.mensajes.get(t.id)?.push(mensaje);
                if (t.estado === "resuelto" || t.estado === "en_espera") {
                    Object.assign(t, { estado: "abierto", puedeConfirmar: false, resueltoEn: null, version: t.version + 1 });
                }
                const r: Schema<"TicketPortalConMensaje"> = { ticket: t, mensaje };
                return json(route, r);
            }
            if (metodo === "POST" && accion === "/confirmar-solucion") {
                const entrada = cuerpo as Schema<"EntradaConfirmarSolucion">;
                if (entrada.version !== t.version) {
                    return conflicto();
                }
                if (t.estado !== "resuelto") {
                    return problema(route, { type: "urn:iris:problema:transicion_invalida", title: "Transición inválida", status: 422 });
                }
                Object.assign(t, { estado: "cerrado", puedeConfirmar: false, cerradoEn: "2026-10-28T15:10:00Z", version: t.version + 1 });
                return json(route, t);
            }
        }
        return problema(route, { type: "urn:iris:problema:no_encontrado", title: "No encontrado", status: 404 });
    });

    return {
        peticiones,
        responderProblema: (metodo, prefijo, p, n = 1, cabeceras = {}) => forzados.push({ metodo, prefijo, p, n, cabeceras }),
        cambiarVersion: (id) => {
            const t = esc.tickets.find((x) => x.id === id);
            if (t) t.version++;
        },
        vencerSesion: () => {
            vencida = true;
        },
    };
}

export { TICKET_LIMITE, TICKET_RED, TICKET_VALIDO, TICKET_VENCIDO };
