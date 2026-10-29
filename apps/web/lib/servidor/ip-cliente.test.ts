import { createServer, request as pedir, type IncomingHttpHeaders } from "node:http";
import type { AddressInfo } from "node:net";
import { describe, expect, it } from "vitest";
import { CABECERA_SOCKET_IP, CABECERA_SOCKET_PROTO, CABECERA_XFF_ENTRANTE, CABECERA_XFP_ENTRANTE } from "../../ip-cliente.mjs";

describe("precarga ip-cliente.mjs", () => {
    it("anota la IP real del socket y lo que llegó, aunque el cliente mande esas cabeceras", async () => {
        let vistas: IncomingHttpHeaders = {};
        const servidor = createServer((req, res) => {
            vistas = req.headers;
            res.end("ok");
        });
        await new Promise<void>((r) => servidor.listen(0, "127.0.0.1", r));
        const { port } = servidor.address() as AddressInfo;
        await new Promise<void>((resolver, rechazar) => {
            const r = pedir(
                {
                    host: "127.0.0.1",
                    port,
                    path: "/api/v1/yo",
                    headers: { "X-Forwarded-For": "1.2.3.4", "X-Forwarded-Proto": "https", [CABECERA_SOCKET_IP]: "6.6.6.6", [CABECERA_XFF_ENTRANTE]: "falsa" },
                },
                (res) => {
                    res.resume();
                    res.on("end", resolver);
                },
            );
            r.on("error", rechazar);
            r.end();
        });
        servidor.close();
        expect(vistas[CABECERA_SOCKET_IP]).toMatch(/^(::ffff:)?127\.0\.0\.1$/);
        expect(vistas[CABECERA_SOCKET_PROTO]).toBe("http");
        expect(vistas[CABECERA_XFF_ENTRANTE]).toBe("1.2.3.4");
        expect(vistas[CABECERA_XFP_ENTRANTE]).toBe("https");
    });
});
