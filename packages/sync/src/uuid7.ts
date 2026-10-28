// UUID v7 (RFC 9562) generado en el cliente: 48 bits de milisegundos, 12 bits de contador
// (método 1 del §6.2, para que dos ids del mismo milisegundo sigan ordenados) y 62 aleatorios.
// Implementación propia para no agregar una dependencia; el azar viene de `crypto.getRandomValues`
// (navegador, Node 22; en Expo, con el polyfill `react-native-get-random-values`).

export type FuenteAzar = (bytes: Uint8Array) => void;

const azarSistema: FuenteAzar = (bytes) => {
    globalThis.crypto.getRandomValues(bytes);
};

export interface GeneradorUuid7 {
    (): string;
}

/** Crea un generador monótono: cada id es mayor (en texto y en bytes) que el anterior. */
export function crearGeneradorUuid7(ahora: () => number = Date.now, azar: FuenteAzar = azarSistema): GeneradorUuid7 {
    let ultimoMs = -1;
    let contador = 0;
    return () => {
        let ms = Math.floor(ahora());
        const aleatorio = new Uint8Array(10);
        azar(aleatorio);
        if (ms > ultimoMs) {
            ultimoMs = ms;
            // Arranca el contador en la mitad baja para dejar margen dentro del mismo milisegundo.
            contador = ((aleatorio[0] ?? 0) << 3) | ((aleatorio[1] ?? 0) & 0x07);
        } else {
            ms = ultimoMs;
            contador += 1;
            if (contador > 0xfff) {
                ultimoMs += 1;
                ms = ultimoMs;
                contador = 0;
            }
        }
        const b = new Uint8Array(16);
        // 48 bits de tiempo, big endian (2^48 cabe sin pérdida en un double).
        let t = ms;
        for (let i = 5; i >= 0; i -= 1) {
            b[i] = t % 256;
            t = Math.floor(t / 256);
        }
        b[6] = 0x70 | ((contador >> 8) & 0x0f);
        b[7] = contador & 0xff;
        b[8] = 0x80 | ((aleatorio[2] ?? 0) & 0x3f);
        for (let i = 9; i < 16; i += 1) {
            b[i] = aleatorio[i - 6] ?? 0;
        }
        const hex = Array.from(b, (x) => x.toString(16).padStart(2, "0")).join("");
        return `${hex.slice(0, 8)}-${hex.slice(8, 12)}-${hex.slice(12, 16)}-${hex.slice(16, 20)}-${hex.slice(20)}`;
    };
}

/** Milisegundos codificados en un UUID v7. */
export function msDeUuid7(id: string): number {
    return Number.parseInt(id.replaceAll("-", "").slice(0, 12), 16);
}

export const RE_UUID7 = /^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/;
