// Precarga del servidor del panel (`node --import ./ip-cliente.mjs …`): anota en cada petición,
// antes de que Next la vea, la IP del socket, el X-Forwarded-For y el X-Forwarded-Proto tal como
// llegaron y el protocolo del socket. Next solo rellena X-Forwarded-For si no viene (`??=`) y los
// Route Handlers no exponen el socket, así que sin esto el proxy de /api (lib/proxy.ts) no tendría
// una IP fiable. Siempre se sobrescriben: lo que el navegador mande con estos nombres no cuenta.
// Usa el canal oficial de Node `http.server.request.start`, que se publica antes del evento
// `request` (servidor HTTP; con TLS delante, el proxy que termina TLS es quien conecta).
import diagnosticsChannel from "node:diagnostics_channel";

export const CABECERA_SOCKET_IP = "x-iris-socket-ip";
export const CABECERA_SOCKET_PROTO = "x-iris-socket-proto";
export const CABECERA_XFF_ENTRANTE = "x-iris-xff-entrante";
export const CABECERA_XFP_ENTRANTE = "x-iris-xfp-entrante";

/** @param {{ request: import("node:http").IncomingMessage }} mensaje */
export function anotar({ request }) {
    const h = request.headers;
    const xff = h["x-forwarded-for"];
    const xfp = h["x-forwarded-proto"];
    h[CABECERA_SOCKET_IP] = request.socket?.remoteAddress ?? "";
    h[CABECERA_SOCKET_PROTO] = /** @type {{ encrypted?: boolean }} */ (request.socket)?.encrypted ? "https" : "http";
    h[CABECERA_XFF_ENTRANTE] = Array.isArray(xff) ? xff.join(", ") : (xff ?? "");
    h[CABECERA_XFP_ENTRANTE] = Array.isArray(xfp) ? (xfp[0] ?? "") : (xfp ?? "");
}

diagnosticsChannel.subscribe("http.server.request.start", (mensaje) => anotar(/** @type {{ request: import("node:http").IncomingMessage }} */ (mensaje)));
