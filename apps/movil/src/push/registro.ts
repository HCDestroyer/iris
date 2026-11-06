// Registro del teléfono para recibir push (`contratos/push.md` §1): `POST /dispositivos` al entrar,
// al cambiar de empresa, cuando FCM rota el token y una vez al día con la app abierta (es idempotente
// por token). Al cerrar sesión se quitan los de este usuario (el servidor ya los quita con
// `DELETE /sesion`: esto es una capa más). El id se guarda solo para poder quitarlo; el token nunca.
import type { AlmacenClaves } from "../entorno";
import type { ApiPush } from "./api";

/** Un registro por día con la app abierta basta (el servidor renueva versión y sesión). */
export const DIA_MS = 24 * 60 * 60 * 1000;

const clave = (usuarioId: number) => `iris-push-dispositivos-${usuarioId}`;

/** Lo guardado por usuario: id del dispositivo y cuándo se registró, por empresa (`"0"` = sin empresa). */
type Guardado = Record<string, { id: number; registradoEn: number }>;

export interface OpcionesRegistro {
    api: ApiPush;
    claves: AlmacenClaves;
    /** Token nativo de FCM (`getDevicePushTokenAsync`); `null` si este teléfono no puede (sin Firebase). */
    obtenerToken: () => Promise<string | null>;
    appVersion: string;
    generarClave: () => string;
    ahora?: () => number;
}

export class RegistroDispositivo {
    private readonly o: OpcionesRegistro;
    private readonly ahora: () => number;

    constructor(o: OpcionesRegistro) {
        this.o = o;
        this.ahora = o.ahora ?? Date.now;
    }

    private leer(usuarioId: number): Guardado {
        try {
            const v: unknown = JSON.parse(this.o.claves.leer(clave(usuarioId)) ?? "{}");
            return typeof v === "object" && v !== null ? (v as Guardado) : {};
        } catch {
            return {};
        }
    }

    private escribir(usuarioId: number, g: Guardado): void {
        if (Object.keys(g).length === 0) {
            this.o.claves.borrar(clave(usuarioId));
        } else {
            this.o.claves.escribir(clave(usuarioId), JSON.stringify(g));
        }
    }

    /**
     * Registra este teléfono para el usuario en la empresa. `forzar`: aunque se haya registrado hoy
     * (al entrar, al cambiar de empresa o con un token nuevo). Devuelve `false` si no hay token.
     */
    async registrar(usuarioId: number, empresa: number | null, opciones: { forzar?: boolean; token?: string } = {}): Promise<boolean> {
        const g = this.leer(usuarioId);
        const k = String(empresa ?? 0);
        const previo = g[k];
        if (!opciones.forzar && !opciones.token && previo && this.ahora() - previo.registradoEn < DIA_MS) {
            return true;
        }
        const token = opciones.token ?? (await this.o.obtenerToken());
        if (!token) {
            return false;
        }
        const d = await this.o.api.registrarDispositivo(empresa, { token, plataforma: "android", appVersion: this.o.appVersion }, this.o.generarClave());
        this.escribir(usuarioId, { ...this.leer(usuarioId), [k]: { id: d.id, registradoEn: this.ahora() } });
        return true;
    }

    /**
     * Cerrar sesión: quita los dispositivos de este usuario que este teléfono registró (en cada
     * empresa). Un error (sin señal, 404 porque el servidor ya lo quitó) no detiene la salida.
     */
    async quitarTodos(usuarioId: number): Promise<void> {
        const g = this.leer(usuarioId);
        for (const [k, d] of Object.entries(g)) {
            try {
                await this.o.api.quitarDispositivo(k === "0" ? null : Number(k), d.id);
            } catch {
                // El servidor también los quita con `DELETE /sesion` y en su purga.
            }
        }
        this.escribir(usuarioId, {});
    }
}
