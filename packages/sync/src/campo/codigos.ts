// Lo que lee el escáner (serie, MAC o QR con ambos) convertido a los campos del equipo.

/** MAC normalizada `AA:BB:CC:DD:EE:FF` si el texto es una MAC (con o sin separadores). */
export function normalizarMac(texto: string): string | null {
    const hex = texto.trim().replace(/[\s:.-]/g, "").toUpperCase();
    if (!/^[0-9A-F]{12}$/.test(hex)) {
        return null;
    }
    return hex.match(/.{2}/g)?.join(":") ?? null;
}

/**
 * Interpreta un código leído: MAC sola, QR con pares (`SN:…;MAC:…`, `S/N=…`, JSON) o serie.
 */
export function interpretarCodigo(texto: string): { serie?: string; mac?: string } {
    const limpio = texto.trim();
    try {
        const json: unknown = JSON.parse(limpio);
        if (json && typeof json === "object") {
            const o = json as Record<string, unknown>;
            const serie = [o.sn, o.serie, o.serial, o.SN].find((v): v is string => typeof v === "string");
            const mac = [o.mac, o.MAC].find((v): v is string => typeof v === "string");
            return { ...(serie ? { serie } : {}), ...(mac ? { mac: normalizarMac(mac) ?? mac } : {}) };
        }
    } catch {
        // No es JSON.
    }
    const pares = [...limpio.matchAll(/(S\/?N|SERIE|SERIAL|MAC)\s*[:=]\s*([^;,\s]+)/gi)];
    if (pares.length > 0) {
        const resultado: { serie?: string; mac?: string } = {};
        for (const [, clave = "", valor = ""] of pares) {
            if (clave.toUpperCase() === "MAC") {
                resultado.mac = normalizarMac(valor) ?? valor;
            } else {
                resultado.serie = valor;
            }
        }
        return resultado;
    }
    const mac = normalizarMac(limpio);
    return mac ? { mac } : { serie: limpio.slice(0, 100) };
}
