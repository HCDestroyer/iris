// Lo nativo de las notificaciones, inyectable: la app usa `servicioPushNativo` (expo-notifications)
// y las pruebas uno falso. Las pantallas y el registro solo conocen esta interfaz.
import type { RespuestaPush } from "./acciones";
import type { MensajePush } from "./mensaje";

export type PermisoPush = "concedido" | "denegado" | "sin_preguntar";

export interface ServicioPush {
    /** Canales de Android y categorías de botones (idempotente, al abrir la app). */
    preparar(): Promise<void>;
    permiso(): Promise<PermisoPush>;
    /** El diálogo del sistema (Android 13+). Si ya se negó para siempre, no aparece: devuelve `denegado`. */
    pedirPermiso(): Promise<PermisoPush>;
    /** Token nativo de FCM; `null` si este APK no trae Firebase o no hay permiso. */
    token(): Promise<string | null>;
    /** FCM rotó el token. */
    alCambiarToken(fn: (token: string) => void): () => void;
    /** Un push llegó con la app al frente (la app decide si lo muestra). */
    alRecibir(fn: (data: Record<string, unknown>) => void): () => void;
    /** Tocó la notificación o un botón (también la que abrió la app cerrada). */
    alResponder(fn: (r: RespuestaPush) => void): () => void;
    presentar(m: MensajePush): Promise<void>;
    quitarGrupo(grupo: string): Promise<void>;
    /** Quita todas las notificaciones de la app (al cerrar sesión). */
    quitarTodas(): Promise<void>;
    /** Ajustes de notificaciones de la app en Android (canales: sonido, pantalla bloqueada). */
    abrirAjustes(): Promise<void>;
}
