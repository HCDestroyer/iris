import type { EntradaCrearAdjunto, RespuestaCrearAdjunto } from "../contrato-sync";
import { ErrorTransporte } from "../errores";
import type { Subidor } from "../transporte";

interface Subida {
    entrada: EntradaCrearAdjunto;
    recibido: Uint8Array;
}

/** Servidor tus en memoria. `cortarTrasPartes` simula perder la señal a mitad de la subida. */
export class SubidorFalso implements Subidor {
    readonly subidas = new Map<string, Subida>();
    readonly llamadas: string[] = [];
    /** Tras esta cantidad de PATCH correctos, el siguiente falla con error de red (una vez). */
    cortarTrasPartes: number | null = null;
    private partes = 0;
    sinRed = false;

    private revisar(): void {
        if (this.sinRed) {
            throw new ErrorTransporte("red", "Sin señal");
        }
    }

    crear(entrada: EntradaCrearAdjunto): Promise<RespuestaCrearAdjunto> {
        this.llamadas.push(`POST ${entrada.id}`);
        this.revisar();
        if (!this.subidas.has(entrada.id)) {
            this.subidas.set(entrada.id, { entrada, recibido: new Uint8Array(0) });
        }
        return Promise.resolve({ id: entrada.id, urlSubida: `/tus/${entrada.id}` });
    }

    private subida(url: string): Subida {
        const s = this.subidas.get(url.replace("/tus/", ""));
        if (!s) {
            throw new ErrorTransporte("http", "No existe", 404);
        }
        return s;
    }

    consultarOffset(url: string): Promise<number> {
        this.llamadas.push(`HEAD ${url}`);
        this.revisar();
        return Promise.resolve(this.subida(url).recibido.byteLength);
    }

    enviarParte(url: string, offset: number, parte: Uint8Array): Promise<number> {
        this.llamadas.push(`PATCH ${url} ${offset}`);
        this.revisar();
        if (this.cortarTrasPartes !== null && this.partes >= this.cortarTrasPartes) {
            this.cortarTrasPartes = null;
            return Promise.reject(new ErrorTransporte("red", "Se cortó la señal"));
        }
        const s = this.subida(url);
        if (offset !== s.recibido.byteLength) {
            return Promise.reject(new ErrorTransporte("http", "Offset no coincide", 409));
        }
        const nuevo = new Uint8Array(offset + parte.byteLength);
        nuevo.set(s.recibido);
        nuevo.set(parte, offset);
        s.recibido = nuevo;
        this.partes += 1;
        return Promise.resolve(nuevo.byteLength);
    }

    /** ¿Llegó completo? */
    completo(id: string): boolean {
        const s = this.subidas.get(id);
        return s !== undefined && s.recibido.byteLength === s.entrada.tamano;
    }
}
