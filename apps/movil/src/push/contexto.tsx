// Notificaciones push con la sesión abierta (`contratos/push.md`): registra el teléfono (al entrar,
// al cambiar de empresa —este proveedor se vuelve a montar—, con un token nuevo y una vez al día),
// lee y guarda las preferencias de la cuenta, muestra el primer uso antes del diálogo de Android,
// arma las notificaciones que llegan con la app al frente (menos la del chat que se está viendo) y
// abre el destino al tocar una (también la que abrió la app cerrada: tras entrar, si era para esta
// persona). Sin `servicio` (pruebas, otra plataforma) no hace nada.
import type { MotorChat } from "@iris/sync/chat";
import type { Vista } from "@iris/sync/campo";
import { createContext, useCallback, useContext, useEffect, useMemo, useRef, useState, type ReactNode } from "react";
import { Linking } from "react-native";
import { ErrorApi } from "../api/errores";
import { useCampo } from "../campo/contexto";
import type { AlmacenClaves } from "../entorno";
import { enlaceComoLlegar } from "../pantallas/orden/DetalleOrden";
import { ejecutarRespuesta, vaciarPendientes, type DepsAccion, type RespuestaPush } from "./acciones";
import type { ApiPush, EntradaPreferenciasPush, PreferenciasPush } from "./api";
import { leerMensajePush, seEstaViendo } from "./mensaje";
import type { RegistroDispositivo } from "./registro";
import type { PermisoPush, ServicioPush } from "./servicio";

export type LecturaPreferencias = { estado: "cargando" } | { estado: "listo"; preferencias: PreferenciasPush } | { estado: "error"; error: unknown };

interface ContextoPush {
    /** Este teléfono puede recibir push (hay servicio). */
    disponible: boolean;
    permiso: PermisoPush | null;
    preferencias: LecturaPreferencias;
    recargar: () => void;
    /** Guarda todo con la `version` leída; 409 → vuelve a leer y lanza. */
    guardar: (p: EntradaPreferenciasPush) => Promise<void>;
    activar: () => Promise<PermisoPush>;
    abrirAjustes: () => Promise<void>;
}

const Contexto = createContext<ContextoPush | null>(null);

export function usePush(): ContextoPush | null {
    return useContext(Contexto);
}

const clavePrimerUso = (usuarioId: number) => `iris-push-primer-uso-${usuarioId}`;


export interface PropsProveedorPush {
    servicio: ServicioPush | null;
    api: ApiPush;
    registro: RegistroDispositivo | null;
    claves: AlmacenClaves;
    empresa: number | null;
    /**
     * Origen del panel web: la bandeja de clientes se abre en `${baseUrl}/conversaciones/{id}`. Es el
     * de la API (`EXPO_PUBLIC_IRIS_API_URL`): en el despliegue estándar (install.sh) el panel y la API
     * comparten origen.
     */
    baseUrl: string;
    generarId: () => string;
    chat: MotorChat | null;
    alVolverAlFrente: (fn: () => void) => () => void;
    /** Pantalla de primer uso (antes del diálogo de Android). */
    primerUso: (p: { activar: () => void; ahoraNo: () => void }) => ReactNode;
    children: ReactNode;
}

export function ProveedorPush({ servicio, api, registro, claves, empresa, baseUrl, generarId, chat, alVolverAlFrente, primerUso, children }: PropsProveedorPush) {
    const { sesion, vista, navegar, motor, notificar } = useCampo();
    const usuarioId = sesion.usuarioId;
    const [permiso, setPermiso] = useState<PermisoPush | null>(null);
    const [preferencias, setPreferencias] = useState<LecturaPreferencias>({ estado: "cargando" });
    const [vez, setVez] = useState(0);
    const [primerUsoVisto, setPrimerUsoVisto] = useState(() => claves.leer(clavePrimerUso(usuarioId)) !== null);
    const vistaActual = useRef<Vista>(vista);
    useEffect(() => {
        vistaActual.current = vista;
    }, [vista]);

    const registrar = useCallback(
        async (opciones: { forzar?: boolean; token?: string } = {}) => {
            if (!registro) {
                return;
            }
            try {
                await registro.registrar(usuarioId, empresa, opciones);
            } catch {
                // Sin señal o la API no respondió: se reintenta al volver al frente.
            }
        },
        [registro, usuarioId, empresa],
    );

    // Preferencias de la cuenta.
    useEffect(() => {
        let vivo = true;
        api.leerPreferencias()
            .then((p) => vivo && setPreferencias({ estado: "listo", preferencias: p }))
            .catch((error: unknown) => vivo && setPreferencias({ estado: "error", error }));
        return () => {
            vivo = false;
        };
    }, [api, vez]);

    const deps = useMemo<DepsAccion>(
        () => ({
            api,
            claves,
            generarId,
            usuarioId,
            ...(chat ? { escribirChat: async (id: number, texto: string) => void (await chat.escribir(id, { texto })) } : {}),
            quitarGrupo: (g) => servicio?.quitarGrupo(g) ?? Promise.resolve(),
        }),
        [api, claves, generarId, usuarioId, chat, servicio],
    );

    // Abrir lo que se tocó: una pantalla de la app, la navegación o la bandeja en el panel.
    const responder = useCallback(
        async (r: RespuestaPush) => {
            const res = await ejecutarRespuesta(r, deps);
            if (!res) {
                return;
            }
            if (res.destino.tipo === "panel") {
                await Linking.openURL(`${baseUrl}${res.destino.ruta}`);
                return;
            }
            const destino = res.destino.vista;
            if (destino.tipo === "conversacion" && !chat) {
                // Sin chat en esta sesión (sin `chat.ver`): no hay a dónde ir.
                return;
            }
            navegar(destino);
            if (res.comoLlegar && destino.tipo === "orden") {
                const o = motor.obtenerOrden(destino.id);
                const url = o ? enlaceComoLlegar(o.lat ?? o.cliente?.lat ?? null, o.lon ?? o.cliente?.lon ?? null, o.direccion) : null;
                if (url) {
                    await Linking.openURL(url);
                }
            }
        },
        [deps, baseUrl, navegar, motor, chat],
    );

    // Lo último de cada render, para que las suscripciones no se rehagan (ni se vuelva a registrar).
    const ultimos = useRef({ deps, responder });
    useEffect(() => {
        ultimos.current = { deps, responder };
    }, [deps, responder]);

    useEffect(() => {
        if (!servicio) {
            return;
        }
        let vivo = true;
        const revisar = async () => {
            const p = await servicio.permiso();
            if (!vivo) {
                return;
            }
            setPermiso(p);
            if (p === "concedido") {
                await registrar();
            }
            await vaciarPendientes(ultimos.current.deps);
        };
        void servicio
            .preparar()
            .catch(() => undefined)
            .then(async () => {
                const p = await servicio.permiso();
                if (!vivo) {
                    return;
                }
                setPermiso(p);
                // Al entrar (o al cambiar de empresa) siempre: renueva la sesión del dispositivo.
                if (p === "concedido") {
                    await registrar({ forzar: true });
                }
                await vaciarPendientes(ultimos.current.deps);
            });
        const quitarToken = servicio.alCambiarToken((token) => void registrar({ token }));
        const quitarFrente = alVolverAlFrente(() => void revisar());
        const quitarRecibir = servicio.alRecibir((data) => {
            const m = leerMensajePush(data);
            if (m && m.usuarioId === usuarioId && !seEstaViendo(m, vistaActual.current)) {
                void servicio.presentar(m);
            }
        });
        const quitarResponder = servicio.alResponder((r) => void ultimos.current.responder(r));

        return () => {
            vivo = false;
            quitarToken();
            quitarFrente();
            quitarRecibir();
            quitarResponder();
        };
    }, [servicio, registrar, alVolverAlFrente, usuarioId]);

    const activar = useCallback(async (): Promise<PermisoPush> => {
        if (!servicio) {
            return "denegado";
        }
        const p = await servicio.pedirPermiso();
        setPermiso(p);
        if (p === "concedido") {
            await registrar({ forzar: true });
        }
        return p;
    }, [servicio, registrar]);

    const marcarPrimerUso = useCallback(() => {
        claves.escribir(clavePrimerUso(usuarioId), String(Date.now()));
        setPrimerUsoVisto(true);
    }, [claves, usuarioId]);

    const guardar = useCallback(
        async (p: EntradaPreferenciasPush) => {
            try {
                const nuevas = await api.guardarPreferencias(p, generarId());
                setPreferencias({ estado: "listo", preferencias: nuevas });
            } catch (e) {
                if (e instanceof ErrorApi && e.estado === 409) {
                    // Se cambiaron en otro teléfono: se muestran las de ahora.
                    setVez((v) => v + 1);
                }
                throw e;
            }
        },
        [api, generarId],
    );

    const valor = useMemo<ContextoPush>(
        () => ({
            disponible: servicio !== null,
            permiso,
            preferencias,
            recargar: () => {
                setPreferencias({ estado: "cargando" });
                setVez((v) => v + 1);
            },
            guardar,
            activar,
            abrirAjustes: () => servicio?.abrirAjustes() ?? Promise.resolve(),
        }),
        [servicio, permiso, preferencias, guardar, activar],
    );

    // Primer uso: antes del diálogo de Android, una sola vez («Ahora no» no vuelve a preguntar solo).
    // Con el push del servidor sin configurar no se pide el permiso.
    const configurado = preferencias.estado === "listo" && preferencias.preferencias.push.estado === "configurado";
    if (servicio && permiso === "sin_preguntar" && !primerUsoVisto && configurado) {
        return (
            <Contexto.Provider value={valor}>
                {primerUso({
                    activar: () => {
                        marcarPrimerUso();
                        void activar().then((p) => {
                            if (p === "denegado") {
                                notificar({ tono: "warning", texto: "Las notificaciones quedaron bloqueadas. Puedes activarlas en «Yo» → «Notificaciones»." });
                            }
                        });
                    },
                    ahoraNo: marcarPrimerUso,
                })}
            </Contexto.Provider>
        );
    }
    return <Contexto.Provider value={valor}>{children}</Contexto.Provider>;
}
