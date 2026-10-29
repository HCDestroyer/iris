// Tipos de la precarga ip-cliente.mjs (JavaScript para cargarse con `node --import` sin compilar).
import type { IncomingMessage } from "node:http";

export declare const CABECERA_SOCKET_IP = "x-iris-socket-ip";
export declare const CABECERA_SOCKET_PROTO = "x-iris-socket-proto";
export declare const CABECERA_XFF_ENTRANTE = "x-iris-xff-entrante";
export declare const CABECERA_XFP_ENTRANTE = "x-iris-xfp-entrante";
export declare function anotar(mensaje: { request: IncomingMessage }): void;
