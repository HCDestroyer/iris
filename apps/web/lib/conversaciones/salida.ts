/*
 * Salir de la conversación abierta pasando por quien tenga algo que proteger. El compositor, con
 * archivos por enviar o subiendo, registra aquí su «salir preguntando»; la bandeja lo usa cuando
 * cambia de conversación por código (el botón «La atiendo» de la lista, «Ver» de un aviso), que no
 * pasa por un enlace. Solo hay un compositor a la vez.
 */

type Salir = (ruta: string) => void;

let protegida: Salir | null = null;

/** El compositor registra su salida protegida; devuelve cómo quitarla (al desmontarse). */
export function registrarSalidaProtegida(salir: Salir): () => void {
    protegida = salir;
    return () => {
        if (protegida === salir) protegida = null;
    };
}

/** Va a `ruta`: por la salida protegida si hay una (pregunta si hace falta); si no, directo. */
export function salirDeConversacion(ruta: string, directo: Salir): void {
    if (protegida) protegida(ruta);
    else directo(ruta);
}
