/*
 * Salir de la conversación abierta pasando por quien tenga algo que proteger. El compositor
 * registra aquí su «salir preguntando» y si ahora mismo tiene archivos por enviar o subiendo. Lo
 * usan las navegaciones por código, que no pasan por un enlace: la bandeja al cambiar de
 * conversación («La atiendo», «Ver» de un aviso) y la llamada al contestar. Solo hay un compositor
 * a la vez.
 */

type Salir = (ruta: string) => void;
type Proteccion = { salir: Salir; activa: () => boolean };

let protegida: Proteccion | null = null;

/** El compositor registra su salida protegida; devuelve cómo quitarla (al desmontarse). */
export function registrarSalidaProtegida(salir: Salir, activa: () => boolean): () => void {
    const mia: Proteccion = { salir, activa };
    protegida = mia;
    return () => {
        if (protegida === mia) protegida = null;
    };
}

/** ¿Hay ahora algo que se perdería al salir de la conversación (archivos por enviar o subiendo)? */
export function hayProteccion(): boolean {
    return protegida?.activa() ?? false;
}

/** Va a `ruta`: por la salida protegida si hay una (pregunta si hace falta); si no, directo. */
export function salirDeConversacion(ruta: string, directo: Salir): void {
    if (protegida) protegida.salir(ruta);
    else directo(ruta);
}
