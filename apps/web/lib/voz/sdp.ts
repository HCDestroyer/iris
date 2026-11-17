/*
 * La respuesta SDP que el navegador manda a `contestar` (contratos/voz.md §6.2, fila 11). El
 * servidor rechaza (422 `sdp_invalido`) cualquier dirección de una red interna: candidatos con la IP
 * privada del equipo, su `raddr`, o la línea `c=` / `a=rtcp` con esa IP. Un navegador con el
 * micrófono permitido sí las pone, así que aquí se quitan antes de enviar.
 *
 * No se pierde nada: Meta es ICE-lite con IP pública y aprende la dirección del navegador de sus
 * comprobaciones de conectividad (candidato «peer reflexive»); no necesita que viajen en el SDP.
 * Tampoco se revela al servidor ni a Meta la red interna de quien contesta.
 */

const esMdns = (direccion: string) => /^[A-Za-z0-9-]+\.local$/i.test(direccion);

export function respuestaSinRedInterna(sdp: string): string {
    const salto = sdp.includes("\r\n") ? "\r\n" : "\n";
    const lineas = sdp.split(/\r?\n/).flatMap((linea) => {
        if (linea.startsWith("a=candidate:")) {
            // a=candidate:<fundación> <componente> <transporte> <prioridad> <dirección> <puerto> typ <tipo> …
            const direccion = linea.split(/\s+/)[4] ?? "";
            // Solo se conservan los nombres mDNS (no dicen nada de la red); el resto, fuera.
            return esMdns(direccion) && !/\sraddr\s/.test(linea) ? [linea] : [];
        }
        if (linea === "a=end-of-candidates") return [];
        if (linea.startsWith("c=IN IP4 ")) return ["c=IN IP4 0.0.0.0"];
        if (linea.startsWith("c=IN IP6 ")) return ["c=IN IP6 ::"];
        const rtcp = /^a=rtcp:(\d+) IN IP([46]) /.exec(linea);
        if (rtcp) return [`a=rtcp:${rtcp[1]} IN IP${rtcp[2]} ${rtcp[2] === "4" ? "0.0.0.0" : "::"}`];
        return [linea];
    });
    return lineas.join(salto);
}
