import type { Adjunto, EntradaRegistrarAdjunto } from "../contrato";
import { ErrorTransporte } from "../errores";
import type { Subidor } from "../transporte";

interface Subida {
    entrada: EntradaRegistrarAdjunto;
    recibido: Uint8Array;
}

/**
 * Servidor de adjuntos (alta + tus) en memoria. `cortarTrasPartes` simula perder la señal a mitad
 * de la subida; `rechazarAlta` responde un problema definitivo al alta.
 */
export class SubidorFalso implements Subidor {
    readonly subidas = new Map<string, Subida>();
    readonly llamadas: string[] = [];
    /** Tras esta cantidad de PATCH correctos, el siguiente falla con error de red (una vez). */
    cortarTrasPartes: number | null = null;
    private partes = 0;
    sinRed = false;
    /** Error para la próxima alta (p. ej. 422 `mime_no_permitido`). */
    rechazarAlta: ErrorTransporte | null = null;

    private revisar(): void {
        if (this.sinRed) {
            throw new ErrorTransporte("red", "Sin señal");
        }
    }

    crear(entrada: EntradaRegistrarAdjunto): Promise<Adjunto> {
        this.llamadas.push(`POST ${entrada.id}`);
        this.revisar();
        if (this.rechazarAlta) {
            const e = this.rechazarAlta;
            this.rechazarAlta = null;
            return Promise.reject(e);
        }
        if (!this.subidas.has(entrada.id)) {
            this.subidas.set(entrada.id, { entrada: structuredClone(entrada), recibido: new Uint8Array(0) });
        }
        return Promise.resolve(this.adjunto(entrada.id));
    }

    adjunto(id: string): Adjunto {
        const s = this.subida(id);
        const completo = s.recibido.byteLength === s.entrada.tamano;
        return {
            id,
            entidad: s.entrada.entidad,
            entidadId: s.entrada.entidadId,
            tipo: s.entrada.tipo,
            claveChecklist: s.entrada.claveChecklist ?? null,
            mime: s.entrada.mime,
            tamano: s.entrada.tamano,
            sha256: s.entrada.sha256,
            recibido: s.recibido.byteLength,
            estado: completo ? "completo" : s.recibido.byteLength > 0 ? "subiendo" : "pendiente",
            motivoRechazo: null,
            lat: s.entrada.lat ?? null,
            lon: s.entrada.lon ?? null,
            tomadoEn: s.entrada.tomadoEn ?? null,
            subidoPor: 5,
            creadoEn: "2026-10-28T15:00:00Z",
            completadoEn: completo ? "2026-10-28T15:00:00Z" : null,
            urlSubida: `/api/v1/adjuntos/${id}/subida`,
            urlDescarga: `/api/v1/adjuntos/${id}`,
            version: 1,
        };
    }

    private subida(id: string): Subida {
        const s = this.subidas.get(id);
        if (!s) {
            throw new ErrorTransporte("http", "No existe", 404);
        }
        return s;
    }

    consultarOffset(id: string): Promise<number> {
        this.llamadas.push(`HEAD ${id}`);
        this.revisar();
        return Promise.resolve(this.subida(id).recibido.byteLength);
    }

    enviarParte(id: string, offset: number, parte: Uint8Array): Promise<number> {
        this.llamadas.push(`PATCH ${id} ${offset}`);
        this.revisar();
        if (this.cortarTrasPartes !== null && this.partes >= this.cortarTrasPartes) {
            this.cortarTrasPartes = null;
            return Promise.reject(new ErrorTransporte("red", "Se cortó la señal"));
        }
        const s = this.subida(id);
        if (offset !== s.recibido.byteLength) {
            const problema = { type: "urn:iris:problema:offset_distinto", title: "Offset distinto", status: 409 };
            const cabeceras = new Headers({ "Upload-Offset": String(s.recibido.byteLength) });
            return Promise.reject(new ErrorTransporte("http", "Offset distinto", 409, problema, cabeceras));
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
