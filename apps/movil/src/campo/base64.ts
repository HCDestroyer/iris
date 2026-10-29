// Base64 → bytes sin depender de `atob` (la firma en PNG llega en base64 desde react-native-svg).
const ALFABETO = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/";
const VALOR = new Map([...ALFABETO].map((c, i) => [c, i]));

export function base64ABytes(texto: string): Uint8Array {
    const limpio = texto.replace(/^data:[^,]*,/, "").replace(/[\s=]/g, "").replace(/-/g, "+").replace(/_/g, "/");
    const salida = new Uint8Array(Math.floor((limpio.length * 3) / 4));
    let bits = 0;
    let acumulado = 0;
    let j = 0;
    for (const c of limpio) {
        const v = VALOR.get(c);
        if (v === undefined) {
            throw new Error("Base64 no válido");
        }
        acumulado = (acumulado << 6) | v;
        bits += 6;
        if (bits >= 8) {
            bits -= 8;
            salida[j++] = (acumulado >> bits) & 0xff;
        }
    }
    return salida.subarray(0, j);
}
