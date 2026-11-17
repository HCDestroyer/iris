import { describe, expect, it } from "vitest";
import { respuestaSinRedInterna } from "./sdp";

const RESPUESTA = [
    "v=0",
    "o=- 4611731400430051336 2 IN IP4 127.0.0.1",
    "s=-",
    "t=0 0",
    "a=group:BUNDLE 0",
    "a=msid-semantic: WMS",
    "m=audio 54321 UDP/TLS/RTP/SAVPF 111 126",
    "c=IN IP4 192.168.1.23",
    "a=rtcp:9 IN IP4 192.168.1.23",
    "a=candidate:1 1 udp 2122260223 192.168.1.23 54321 typ host generation 0",
    "a=candidate:2 1 udp 2122194687 4d3f1a52-7c1e-4b0a-9f1e-0d2b6c1a7e55.local 54322 typ host generation 0",
    "a=candidate:3 1 udp 1686052607 190.56.10.20 54321 typ srflx raddr 192.168.1.23 rport 54321 generation 0",
    "a=candidate:4 1 udp 2122260223 fd00::1234 54323 typ host generation 0",
    "a=end-of-candidates",
    "a=ice-ufrag:abcd",
    "a=ice-pwd:0123456789abcdefghijklmn",
    "a=fingerprint:sha-256 AA:BB:CC",
    "a=setup:active",
    "a=mid:0",
    "a=sendrecv",
    "a=rtcp-mux",
    "a=rtpmap:111 opus/48000/2",
    "a=rtpmap:126 telephone-event/8000",
    "",
].join("\r\n");

describe("respuesta SDP sin direcciones de la red interna (voz.md §6.2-11)", () => {
    const limpia = respuestaSinRedInterna(RESPUESTA);

    it("quita los candidatos con IP (privada, pública con raddr interno, IPv6 local) y deja el mDNS", () => {
        const candidatos = limpia.split("\r\n").filter((l) => l.startsWith("a=candidate:"));
        expect(candidatos).toEqual(["a=candidate:2 1 udp 2122194687 4d3f1a52-7c1e-4b0a-9f1e-0d2b6c1a7e55.local 54322 typ host generation 0"]);
        expect(limpia).not.toContain("192.168.1.23");
        expect(limpia).not.toContain("fd00::1234");
        expect(limpia).not.toContain("a=end-of-candidates");
    });

    it("`c=` y `a=rtcp` quedan en 0.0.0.0 («la dirá ICE»); lo demás no se toca", () => {
        expect(limpia).toContain("c=IN IP4 0.0.0.0\r\n");
        expect(limpia).toContain("a=rtcp:9 IN IP4 0.0.0.0\r\n");
        for (const linea of ["m=audio 54321 UDP/TLS/RTP/SAVPF 111 126", "a=ice-ufrag:abcd", "a=fingerprint:sha-256 AA:BB:CC", "a=setup:active", "a=rtpmap:111 opus/48000/2", "o=- 4611731400430051336 2 IN IP4 127.0.0.1"]) {
            expect(limpia).toContain(`${linea}\r\n`);
        }
        expect(limpia.endsWith("\r\n")).toBe(true);
        // IPv6 y saltos de línea simples.
        expect(respuestaSinRedInterna("c=IN IP6 fe80::1\na=rtcp:9 IN IP6 fe80::1\na=mid:0\n")).toBe("c=IN IP6 ::\na=rtcp:9 IN IP6 ::\na=mid:0\n");
    });

    it("un nombre que parece mDNS pero trae otra cosa no pasa", () => {
        expect(respuestaSinRedInterna("a=candidate:1 1 udp 1 interno.empresa.local 9 typ host")).toBe("");
        expect(respuestaSinRedInterna("a=candidate:1 1 udp 1 x.local 9 typ srflx raddr 10.0.0.1 rport 9")).toBe("");
    });
});
