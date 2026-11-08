import type { Page, Route } from "@playwright/test";
import { ErrorTransporte } from "@iris/sync";
import { ServidorChatFalso } from "@iris/sync/pruebas";
import { TECNICO } from "./datos-campo";

/*
 * Chat interno para Playwright (`contratos/chat.md`): las rutas `/api/v1/mensajeria/chat/*` sobre
 * el mismo servidor falso de las pruebas de `@iris/sync` (seq, idempotencia por id, eliminar con
 * plazo, lecturas y feed con cursor). `/eventos` responde un SSE que entrega lo pendiente desde
 * `?desde` (o `Last-Event-ID`) y se cierra (EventSource reconecta). Se registra después de
 * `simularApiCampo`, que sigue atendiendo `/adjuntos` (también las fotos del chat).
 */

export const ANDREA = { id: 61, nombre: "Andrea Castillo" };
export const LUIS = { id: 62, nombre: "Luis Pérez" };

/** PNG de 1×1 (la foto del chat que se descarga). */
const PNG = Buffer.from("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNkYAAAAAYAAjCB0C8AAAAASUVORK5CYII=", "base64");

export interface ApiChat {
    servidor: ServidorChatFalso;
    /** Peticiones al chat (método y ruta), en orden. */
    peticiones: { metodo: string; ruta: string }[];
    /** Las próximas N peticiones de envío fallan como sin red (la cola reintenta). */
    cortarEnvios: number;
}

export async function simularChat(page: Page, preparar?: (s: ServidorChatFalso) => void): Promise<ApiChat> {
    const servidor = new ServidorChatFalso(TECNICO.id);
    servidor.nombreYo = TECNICO.nombre;
    servidor.ahora = () => Date.now();
    servidor.nombres.set(ANDREA.id, ANDREA.nombre);
    servidor.nombres.set(LUIS.id, LUIS.nombre);
    preparar?.(servidor);
    const api: ApiChat = { servidor, peticiones: [], cortarEnvios: 0 };

    const json = (route: Route, cuerpo: unknown, status = 200) => route.fulfill({ status, contentType: "application/json", body: JSON.stringify(cuerpo) });
    const fallo = (route: Route, e: unknown) => {
        if (e instanceof ErrorTransporte && e.status !== null) {
            const cabeceras: Record<string, string> = {};
            e.cabeceras?.forEach((v, k) => {
                cabeceras[k] = v;
            });
            return route.fulfill({ status: e.status, contentType: "application/problem+json", headers: cabeceras, body: JSON.stringify(e.problema ?? { type: "urn:iris:problema:error", title: e.message, status: e.status }) });
        }
        return route.abort("internetdisconnected");
    };

    await page.route(
        (url) => url.pathname.startsWith("/api/v1/mensajeria/chat/"),
        async (route) => {
            const req = route.request();
            const url = new URL(req.url());
            const ruta = url.pathname.slice("/api/v1/mensajeria/chat".length);
            const metodo = req.method();
            api.peticiones.push({ metodo, ruta });
            const q = url.searchParams;
            const id = (re: RegExp) => Number(re.exec(ruta)?.[1]);
            try {
                if (metodo === "GET" && ruta === "/conversaciones") {
                    return json(route, await servidor.listarConversaciones(q.get("cursor") ?? undefined));
                }
                if (metodo === "GET" && /^\/conversaciones\/\d+$/.test(ruta)) {
                    return json(route, await servidor.verConversacion(id(/(\d+)/)));
                }
                if (metodo === "GET" && /^\/conversaciones\/\d+\/mensajes$/.test(ruta)) {
                    const direccion = q.get("direccion");
                    const desde = q.get("desde_seq");
                    const pagina = await servidor.listarMensajes(id(/(\d+)/), {
                        ...(direccion === "siguientes" || direccion === "anteriores" ? { direccion } : {}),
                        ...(q.get("cursor") ? { cursor: q.get("cursor") ?? "" } : {}),
                        ...(desde !== null ? { desdeSeq: Number(desde) } : {}),
                        limit: Number(q.get("limit") ?? 30),
                    });
                    return json(route, pagina);
                }
                if (metodo === "POST" && /^\/conversaciones\/\d+\/mensajes$/.test(ruta)) {
                    if (api.cortarEnvios > 0) {
                        api.cortarEnvios -= 1;
                        return route.abort("connectionreset");
                    }
                    const r = await servidor.enviarMensaje(id(/(\d+)/), req.postDataJSON());
                    return json(route, r.mensaje, r.reenvio ? 200 : 201);
                }
                if (metodo === "POST" && /^\/conversaciones\/\d+\/leer$/.test(ruta)) {
                    const c = id(/(\d+)/);
                    const r = await servidor.leer(c, (req.postDataJSON() as { hasta_mensaje_id: string }).hasta_mensaje_id);
                    return json(route, { conversacion_id: c, ...r });
                }
                if (metodo === "POST" && /^\/mensajes\/[^/]+\/eliminar$/.test(ruta)) {
                    return json(route, await servidor.eliminarMensaje(/^\/mensajes\/([^/]+)\//.exec(ruta)?.[1] ?? ""));
                }
                if (metodo === "GET" && ruta === "/personas") {
                    return json(route, await servidor.personas(q.get("q") ?? "", q.get("cursor") ?? undefined));
                }
                if (metodo === "POST" && ruta === "/directas") {
                    return json(route, await servidor.abrirDirecta((req.postDataJSON() as { usuario_id: number }).usuario_id));
                }
                if (metodo === "POST" && /^\/ordenes\/\d+\/abrir$/.test(ruta)) {
                    return json(route, await servidor.abrirOrden(id(/(\d+)/)));
                }
                if (metodo === "POST" && /^\/cuadrillas\/\d+\/abrir$/.test(ruta)) {
                    return json(route, await servidor.abrirCuadrilla(id(/(\d+)/)));
                }
                if (metodo === "GET" && ruta === "/cambios") {
                    return json(route, (await servidor.cambios(q.get("cursor"))).datos);
                }
                if (metodo === "GET" && ruta === "/eventos") {
                    const desde = q.get("desde") ?? (await req.headerValue("last-event-id"));
                    const eventos = desde === null ? [] : servidor.eventos.filter((e) => Number(e.id) > Number(desde));
                    const cuerpo = ["retry: 1000", "", ...eventos.flatMap((e) => [`id: ${e.id}`, `event: ${e.tipo}`, `data: ${JSON.stringify(e)}`, ""]), ": latido", "", ""].join("\n");
                    return route.fulfill({ status: 200, headers: { "Content-Type": "text/event-stream", "Cache-Control": "no-store" }, body: cuerpo });
                }
                return json(route, { type: "urn:iris:problema:no_simulado", title: `Sin simular: ${metodo} ${ruta}`, status: 501 }, 501);
            } catch (e) {
                return fallo(route, e);
            }
        },
    );
    // Descarga de las fotos del chat (el resto de /adjuntos lo atiende la API de campo).
    await page.route(
        (url) => /^\/api\/v1\/adjuntos\/[^/]+$/.test(url.pathname),
        (route) => (route.request().method() === "GET" ? route.fulfill({ status: 200, contentType: "image/png", body: PNG }) : route.fallback()),
    );
    return api;
}
