/**
 * Lector incremental de `text/event-stream` (WHATWG «Server-sent events», §9.2.6) para leer el
 * flujo de la bandeja con `fetch`: así la petición lleva `X-Iris-Empresa` y `Last-Event-ID` y el
 * panel ve el estado HTTP (401, 403, 410, 429, 503), cosas que `EventSource` no permite.
 */
export type EventoSSE = {
    /** `id:` del bloque; `undefined` si no vino. */
    id: string | undefined;
    /** `event:`; `message` si no vino. */
    tipo: string;
    datos: string;
};

export type LectorSSE = {
    /** Pasa un trozo de texto; devuelve los eventos completos que cerró. */
    agregar: (trozo: string) => EventoSSE[];
};

/** `alComentario` recibe cada comentario (`: latido`): el flujo sigue vivo. */
export function crearLectorSSE(alComentario?: (texto: string) => void): LectorSSE {
    let resto = "";
    let id: string | undefined;
    let tipo = "";
    let datos: string[] = [];
    let hayDatos = false;

    function linea(texto: string, salida: EventoSSE[]) {
        if (texto === "") {
            if (hayDatos) {
                salida.push({ id, tipo: tipo || "message", datos: datos.join("\n") });
            }
            tipo = "";
            datos = [];
            hayDatos = false;
            id = undefined;
            return;
        }
        if (texto.startsWith(":")) {
            alComentario?.(texto.slice(1).trimStart());
            return;
        }
        const dos = texto.indexOf(":");
        const campo = dos === -1 ? texto : texto.slice(0, dos);
        let valor = dos === -1 ? "" : texto.slice(dos + 1);
        if (valor.startsWith(" ")) {
            valor = valor.slice(1);
        }
        if (campo === "data") {
            datos.push(valor);
            hayDatos = true;
        } else if (campo === "event") {
            tipo = valor;
        } else if (campo === "id" && !valor.includes("\0")) {
            id = valor;
        }
        // `retry:` y campos desconocidos se ignoran: los plazos de reconexión los decide el panel.
    }

    return {
        agregar(trozo) {
            const salida: EventoSSE[] = [];
            let texto = resto + trozo;
            // Un CR al final puede ser la mitad de un CRLF: se guarda para el siguiente trozo.
            const guardaCR = texto.endsWith("\r");
            if (guardaCR) {
                texto = texto.slice(0, -1);
            }
            const partes = texto.split(/\r\n|\n|\r/);
            resto = (partes.pop() ?? "") + (guardaCR ? "\r" : "");
            for (const p of partes) {
                linea(p, salida);
            }
            return salida;
        },
    };
}
