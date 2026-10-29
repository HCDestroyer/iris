import { BlockList, isIP } from "node:net";

/**
 * Proxy del panel hacia la API (`/api/*` → `IRIS_API_URL/api/*`). Así el navegador habla solo con
 * el origen del panel y las cookies `iris_sesion` / `iris_csrf` son del mismo origen.
 * Se resuelve en cada petición (no en el build): la misma imagen sirve en cualquier entorno.
 */

// Cabeceras por salto (RFC 9110 §7.6.1) y las que fetch recalcula.
const NO_REENVIAR_PETICION = new Set([
    "connection",
    "keep-alive",
    "proxy-authenticate",
    "proxy-authorization",
    "te",
    "trailer",
    "transfer-encoding",
    "upgrade",
    "host",
    "content-length",
    "accept-encoding",
    // IP y protocolo del cliente: se arman aquí (ver `cabecerasCliente`), nunca se copian tal cual.
    "x-forwarded-for",
    "x-forwarded-proto",
    "x-real-ip",
    "forwarded",
    // Anotaciones de la precarga (ip-cliente.mjs): solo para el panel, no salen hacia la API.
    "x-iris-socket-ip",
    "x-iris-socket-proto",
    "x-iris-xff-entrante",
    "x-iris-xfp-entrante",
]);

// fetch de Node ya descomprime el cuerpo: content-encoding/length ya no describen lo que se reenvía.
const NO_REENVIAR_RESPUESTA = new Set([
    "connection",
    "keep-alive",
    "transfer-encoding",
    "content-encoding",
    "content-length",
    "set-cookie",
]);

/** Normaliza la IP del socket (`::ffff:10.0.0.5` → `10.0.0.5`). */
function ipSocket(valor: string | null): string | undefined {
    const v = valor?.trim();
    if (!v) return undefined;
    return v.toLowerCase().startsWith("::ffff:") && v.includes(".") ? v.slice(7) : v;
}

/** Redes (CIDR o IP, separadas por coma) de `IRIS_WEB_PROXIES_CONFIABLES`. */
export function redesConfiables(valor: string | undefined = process.env.IRIS_WEB_PROXIES_CONFIABLES): BlockList {
    const lista = new BlockList();
    for (const crudo of (valor ?? "").split(",")) {
        const r = crudo.trim();
        if (!r) continue;
        const [ip = "", bits] = r.split("/");
        const tipo = isIP(ip) === 6 ? "ipv6" : isIP(ip) === 4 ? "ipv4" : undefined;
        if (!tipo) continue;
        if (bits === undefined) lista.addAddress(ip, tipo);
        else lista.addSubnet(ip, Number(bits), tipo);
    }
    return lista;
}

function esConfiable(ip: string, redes: BlockList): boolean {
    const tipo = isIP(ip) === 6 ? "ipv6" : isIP(ip) === 4 ? "ipv4" : undefined;
    return tipo !== undefined && redes.check(ip, tipo);
}

/**
 * `X-Forwarded-For` y `X-Forwarded-Proto` hacia la API, con lo que anotó la precarga del servidor
 * (ip-cliente.mjs). X-Forwarded-For encadena lo que ya venía y agrega al final la IP del socket:
 * la API (servidor/ip.go) lo recorre de derecha a izquierda desde sus proxies confiables, así que lo
 * que escribió el navegador nunca gana. X-Forwarded-Proto se cree solo si quien conectó es un
 * proxy de `IRIS_WEB_PROXIES_CONFIABLES`; si no, es el protocolo del socket. Sin la precarga no
 * hay IP fiable: no se manda X-Forwarded-For (la API verá la IP del panel).
 */
export function cabecerasCliente(h: Headers, redes: BlockList = redesConfiables()): { xff?: string; xfp: string } {
    const socket = ipSocket(h.get("x-iris-socket-ip"));
    const protoSocket = h.get("x-iris-socket-proto") === "https" ? "https" : "http";
    if (!socket) return { xfp: protoSocket };
    const previa = (h.get("x-iris-xff-entrante") ?? "")
        .split(",")
        .map((x) => x.trim())
        .filter(Boolean)
        // Una cadena larga no aporta: la API solo mira los últimos saltos.
        .slice(-10);
    const xfpEntrante = (h.get("x-iris-xfp-entrante") ?? "").trim().toLowerCase();
    const xfp = esConfiable(socket, redes) && (xfpEntrante === "https" || xfpEntrante === "http") ? xfpEntrante : protoSocket;
    return { xff: [...previa, socket].join(", "), xfp };
}

export function urlApi(): string {
    return process.env.IRIS_API_URL ?? "http://localhost:8080";
}

export function problemaProxy(estado: number, codigo: string, titulo: string, detalle: string): Response {
    return Response.json(
        { type: `urn:iris:problema:${codigo}`, title: titulo, status: estado, detail: detalle },
        { status: estado, headers: { "Content-Type": "application/problem+json", "Cache-Control": "no-store" } },
    );
}

export async function reenviar(peticion: Request, fetchApi: typeof fetch = fetch, base: string = urlApi()): Promise<Response> {
    const entrante = new URL(peticion.url);
    const destino = `${base.replace(/\/+$/, "")}${entrante.pathname}${entrante.search}`;

    const cabeceras = new Headers();
    peticion.headers.forEach((valor, clave) => {
        if (!NO_REENVIAR_PETICION.has(clave.toLowerCase())) {
            cabeceras.set(clave, valor);
        }
    });
    // Origen público tal como lo vio el navegador (o el proxy delante del panel).
    const h = peticion.headers;
    cabeceras.set("X-Forwarded-Host", h.get("x-forwarded-host") ?? h.get("host") ?? entrante.host);
    const cliente = cabecerasCliente(h);
    if (cliente.xff) cabeceras.set("X-Forwarded-For", cliente.xff);
    cabeceras.set("X-Forwarded-Proto", cliente.xfp);

    const conCuerpo = !["GET", "HEAD"].includes(peticion.method.toUpperCase());
    let respuesta: Response;
    try {
        respuesta = await fetchApi(destino, {
            method: peticion.method,
            headers: cabeceras,
            body: conCuerpo ? await peticion.arrayBuffer() : undefined,
            redirect: "manual",
            cache: "no-store",
        });
    } catch {
        return problemaProxy(502, "api_no_disponible", "La API no responde", "No se pudo contactar a la API de Iris. Inténtalo de nuevo en un momento.");
    }

    const salida = new Headers();
    respuesta.headers.forEach((valor, clave) => {
        if (!NO_REENVIAR_RESPUESTA.has(clave.toLowerCase())) {
            salida.set(clave, valor);
        }
    });
    // Varias Set-Cookie (sesión y CSRF) deben llegar separadas.
    for (const cookie of respuesta.headers.getSetCookie()) {
        salida.append("Set-Cookie", cookie);
    }
    return new Response(respuesta.body, { status: respuesta.status, statusText: respuesta.statusText, headers: salida });
}
