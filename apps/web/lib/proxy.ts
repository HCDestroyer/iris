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
    cabeceras.set("X-Forwarded-Proto", h.get("x-forwarded-proto") ?? entrante.protocol.replace(":", ""));

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
