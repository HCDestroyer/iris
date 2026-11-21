"use client";

import { avisar } from "@iris/ui";
import { createContext, use, useCallback, useEffect, useMemo, useRef, useState, useSyncExternalStore, type ReactNode } from "react";
import { useClaveIdempotencia } from "@/lib/idempotencia";
import { useSesion } from "@/lib/sesion";
import { abrirTelefono, leerTelefono, llamarTelefono } from "@/lib/telefono/api";
import { cargarJssip } from "@/lib/telefono/jssip";
import { MotorTelefono, type ConfigUa, type UaSip } from "@/lib/telefono/motor";
import { coordinarPestanas, type Pestanas } from "@/lib/telefono/pestanas";
import { textoErrorLlamar } from "@/lib/telefono/presentacion";
import type { EntradaLlamarTelefono, EstadoTelefono, LlamadaTelefono, TelefonoMio } from "@/lib/telefono/tipos";
import { escucharEventos } from "@/lib/voz/canal";
import { ErrorMicrofono, permisoMicrofono, pedirMicrofono, soltarMicrofono } from "@/lib/voz/medios";
import { sonarTimbre } from "@/lib/voz/timbre";
import type { Llamada } from "@/lib/voz/tipos";

/*
 * El teléfono de Iris en el panel (E4c, contratos/voz.md §3.12; maqueta aprobada el 21 nov 2026).
 * Una sola fuente de verdad (`MotorTelefono`) para el punto de la barra superior, la ventana de la
 * entrante (la misma de E4a, con «Contestar» y «Rechazar»), la barra y el panel de la llamada, y
 * «Llamar» en las fichas. Suena en una sola pestaña (`lib/telefono/pestanas`).
 *
 * Encendido por persona y navegador (preferencia en `localStorage`; solo comodidad: el servidor
 * decide si se puede): la primera vez se pide el micrófono con el diálogo «Encender el teléfono».
 */

export type Preferencia = "encendido" | "apagado" | null;

export type OpcionLlamar = {
    /** Para la lista del menú. */
    clave: string;
    etiqueta: string;
    detalle?: string;
    entrada: EntradaLlamarTelefono;
    /** Cómo se muestra mientras suena (el número lo pone el servidor). */
    nombre: string;
    numero: string;
};

export type ContextoTelefono = {
    estado: EstadoTelefono;
    mio: TelefonoMio | null;
    preferencia: Preferencia;
    /** Pide el micrófono, suena un tono de prueba y conecta. */
    encender: () => Promise<boolean>;
    /** «Ahora no» / apagar en este navegador. */
    apagar: () => void;
    reintentar: () => void;
    usarAqui: () => Promise<void>;
    contestar: (id: string) => Promise<void>;
    rechazar: (id: string) => void;
    colgar: (id: string) => void;
    silenciar: (id: string, si: boolean) => void;
    espera: (id: string, si: boolean) => void;
    tono: (id: string, digito: string) => void;
    transferir: (id: string, numero: string) => Promise<void>;
    llamar: (o: OpcionLlamar) => Promise<void>;
    /** La llamada de Iris (la `Llamada` del feed o de `llamar`) de una llamada del teléfono. */
    llamadaIris: (l: LlamadaTelefono) => Llamada | null;
    actualizarLlamadaIris: (l: Llamada) => void;
    /** Esta llamada de la centralita la muestra el teléfono (E4a no saca su ventana). */
    cubre: (l: Llamada) => boolean;
    dialogo: DialogoTelefono;
    abrir: (d: DialogoTelefono) => void;
    panelAbierto: boolean;
    setPanelAbierto: (a: boolean) => void;
};

export type DialogoTelefono = null | "encender" | "marcar" | { tipo: "tonos"; id: string } | { tipo: "transferir"; id: string };

const Contexto = createContext<ContextoTelefono | null>(null);

/** El teléfono de esta pestaña, o `null` si la persona no lo tiene (sin proveedor). */
export function useTelefono(): ContextoTelefono | null {
    return use(Contexto);
}

/** Usable para llamar: disponible, con permiso y en el panel. */
export function puedeLlamar(t: ContextoTelefono | null): t is ContextoTelefono {
    return !!t && t.estado.conexion.fase !== "no_disponible" && !!t.mio?.disponible && t.mio.puede.llamar;
}

const REVISAR_MS = 120_000;
const claveGuardada = (usuarioId: number) => `iris-telefono-${usuarioId}`;

function leerPreferencia(usuarioId: number): Preferencia {
    try {
        const v = window.localStorage.getItem(claveGuardada(usuarioId));
        return v === "encendido" || v === "apagado" ? v : null;
    } catch {
        return null;
    }
}

function guardarPreferencia(usuarioId: number, v: Exclude<Preferencia, null>) {
    try {
        window.localStorage.setItem(claveGuardada(usuarioId), v);
    } catch {
        // Sin almacenamiento: dura lo que la pestaña.
    }
}

function reproducir(stream: MediaStream): () => void {
    const audio = new Audio();
    audio.autoplay = true;
    audio.srcObject = stream;
    void audio.play().catch(() => undefined);
    return () => {
        audio.pause();
        audio.srcObject = null;
    };
}

const digitos = (s: string) => s.replace(/\D/g, "");

export function ProveedorTelefono({ children }: { children: ReactNode }) {
    const { yo } = useSesion();
    const usuarioId = yo.usuario.id;
    const [mio, setMio] = useState<TelefonoMio | null>(null);
    const [releer, setReleer] = useState(0);
    const [preferencia, setPreferencia] = useState<Preferencia>(() => leerPreferencia(usuarioId));
    const [dialogo, abrir] = useState<DialogoTelefono>(null);
    const [panelAbierto, setPanelAbierto] = useState(true);
    const clave = useClaveIdempotencia();

    // El agente SIP se carga al encender (import dinámico).
    const fabrica = useRef<((c: ConfigUa) => UaSip) | null>(null);
    const motor = useMemo(
        () =>
            new MotorTelefono({
                abrir: () => abrirTelefono(),
                crearUa: (c) => {
                    if (!fabrica.current) throw new Error("JsSIP sin cargar");
                    return fabrica.current(c);
                },
                pedirMicrofono,
                reproducir,
                timbre: sonarTimbre,
                revisar: () => setReleer((n) => n + 1),
            }),
        [],
    );
    const estado = useSyncExternalStore(motor.suscribir, motor.leer, motor.leer);

    // Una sola pestaña.
    const pestanas = useRef<Pestanas | null>(null);
    useEffect(() => {
        const p = coordinarPestanas(usuarioId, () => motor.enOtraPestana());
        pestanas.current = p;
        return () => {
            motor.detenerUa();
            p.cerrar();
            pestanas.current = null;
        };
    }, [motor, usuarioId]);

    // ¿Tengo teléfono? (también cuando el servidor cortó con 4003 y al volver a la pestaña).
    useEffect(() => {
        const control = new AbortController();
        const leer = () =>
            leerTelefono(control.signal).then(
                (r) => !control.signal.aborted && setMio(r),
                () => undefined,
            );
        void leer();
        const t = window.setInterval(() => void leer(), REVISAR_MS);
        return () => {
            control.abort();
            window.clearInterval(t);
        };
    }, [releer, yo.empresaActiva?.id]);

    const arrancando = useRef(false);
    const arrancar = useCallback(async () => {
        if (arrancando.current) return;
        arrancando.current = true;
        try {
            const p = pestanas.current;
            if (p && !(await p.tomar())) {
                motor.enOtraPestana();
                return;
            }
            try {
                fabrica.current ??= await cargarJssip();
            } catch {
                motor.noDisponible(null);
                return;
            }
            await motor.encender(mio?.extension ?? null);
        } finally {
            arrancando.current = false;
        }
    }, [motor, mio?.extension]);

    // Conectar o no según lo que dice el servidor y lo que eligió la persona en este navegador.
    useEffect(() => {
        if (!mio) return;
        const panel = (mio.dispositivos ?? []).includes("panel");
        const fase = estado.conexion.fase;
        if (!mio.disponible || !panel) {
            if (mio.motivo === "registro_sin_probar" && preferencia === "encendido") {
                motor.detenerUa();
                if (!(fase === "error" && estado.conexion.error.tipo === "clave")) motor.credencialRechazada(mio.extension ?? null);
            } else if (fase !== "no_disponible") {
                motor.noDisponible(mio.motivo ?? null);
            }
            return;
        }
        if (preferencia !== "encendido") {
            if (fase !== "apagado") motor.apagado(mio.extension ?? null);
            return;
        }
        const volver = fase === "apagado" || fase === "no_disponible" || (fase === "error" && estado.conexion.error.tipo === "clave" && !motor.encendido);
        if (!volver) return;
        if (arrancando.current) return;
        void (async () => {
            if ((await permisoMicrofono()) === "denied") {
                motor.microfonoBloqueado("denegado");
                return;
            }
            await arrancar();
        })();
    }, [mio, preferencia, estado.conexion, motor, arrancar]);

    // «Se abrió muchas veces»: vuelve a intentar sola a la hora que dijo el servidor.
    useEffect(() => {
        const c = estado.conexion;
        if (c.fase !== "error" || c.error.tipo !== "espera") return;
        const t = window.setTimeout(() => void arrancar(), Math.max(1000, c.error.hasta - Date.now()));
        return () => window.clearTimeout(t);
    }, [estado.conexion, arrancar]);

    // Las llamadas de la centralita que llegan por el feed (E4a): cliente, señales, versión para la nota.
    const [iris, setIris] = useState<ReadonlyMap<string, Llamada>>(() => new Map());
    const manejadas = useRef(new Set<string>());
    useEffect(
        () =>
            escucharEventos((evento) => {
                const l = evento.llamada;
                if (!l || l.origen !== "centralita") return;
                setIris((antes) => new Map(antes).set(l.id, l));
            }),
        [],
    );
    const actualizarLlamadaIris = useCallback((l: Llamada) => setIris((antes) => new Map(antes).set(l.id, l)), []);

    const llamadaIris = useCallback(
        (t: LlamadaTelefono): Llamada | null => {
            if (t.llamadaId) return iris.get(t.llamadaId) ?? null;
            if (t.direccion !== "entrante" || t.interna) return null;
            // Sin `X-Iris-Llamada`: la que suena por el AMI con el mismo número (últimos 8 dígitos).
            const fin = digitos(t.numero).slice(-8);
            if (fin.length < 8) return null;
            let mejor: Llamada | null = null;
            for (const l of iris.values()) {
                if (digitos(l.numero).slice(-8) === fin && (l.estado === "sonando" || l.estado === "en_curso") && (!mejor || (l.sonoEn ?? "") > (mejor.sonoEn ?? ""))) mejor = l;
            }
            return mejor;
        },
        [iris],
    );

    // Recordar qué llamadas de Iris mostró el teléfono (para que E4a no saque su ventana ni su nota).
    useEffect(() => {
        for (const t of estado.llamadas) {
            const l = llamadaIris(t);
            if (l) manejadas.current.add(l.id);
        }
    }, [estado.llamadas, llamadaIris]);

    const cubre = useCallback(
        (l: Llamada) => {
            if (manejadas.current.has(l.id)) return true;
            return estado.llamadas.some((t) => llamadaIris(t)?.id === l.id);
        },
        [estado.llamadas, llamadaIris],
    );

    // «Teléfono listo» una vez, cuando la persona lo encendió (no en cada reconexión).
    const avisarAlListo = useRef(false);
    useEffect(() => {
        if (estado.conexion.fase !== "listo" || !avisarAlListo.current) return;
        avisarAlListo.current = false;
        const ext = estado.extension ?? mio?.extension;
        avisar({ titulo: ext ? `Teléfono listo: tu extensión ${ext} suena aquí` : "Teléfono listo" });
    }, [estado.conexion.fase, estado.extension, mio?.extension]);

    const encender = useCallback(async () => {
        try {
            soltarMicrofono(await pedirMicrofono());
        } catch (e) {
            motor.microfonoBloqueado(e instanceof ErrorMicrofono ? e.estado : "no_disponible");
            return false;
        }
        // El timbre corto de prueba: si se oye, el sonido está bien.
        const t = sonarTimbre();
        window.setTimeout(t.detener, 900);
        guardarPreferencia(usuarioId, "encendido");
        setPreferencia("encendido");
        avisarAlListo.current = true;
        await arrancar();
        return true;
    }, [motor, usuarioId, arrancar]);

    const apagar = useCallback(() => {
        guardarPreferencia(usuarioId, "apagado");
        setPreferencia("apagado");
        pestanas.current?.soltar();
        motor.apagado(mio?.extension ?? null);
    }, [motor, usuarioId, mio?.extension]);

    const usarAqui = useCallback(async () => {
        const p = pestanas.current;
        if (p && !(await p.quitar())) {
            avisar({ tono: "error", titulo: "La otra pestaña no soltó el teléfono", descripcion: "Ciérrala e intenta otra vez." });
            return;
        }
        guardarPreferencia(usuarioId, "encendido");
        setPreferencia("encendido");
        avisarAlListo.current = true;
        await arrancar();
    }, [usuarioId, arrancar]);

    const reintentar = useCallback(() => {
        const c = estado.conexion;
        if (c.fase === "error" && c.error.tipo === "clave") {
            setReleer((n) => n + 1);
            if (mio?.disponible) void arrancar();
            return;
        }
        if (c.fase === "error" && c.error.tipo === "microfono" && !motor.encendido) {
            void encender();
            return;
        }
        if (motor.encendido) void motor.reintentar();
        else void arrancar();
    }, [estado.conexion, motor, mio?.disponible, arrancar, encender]);

    const contestar = useCallback(
        async (id: string) => {
            setPanelAbierto(true);
            await motor.contestar(id);
        },
        [motor],
    );

    const llamar = useCallback(
        async (o: OpcionLlamar) => {
            if (estado.conexion.fase !== "listo") {
                avisar({ tono: "error", titulo: "Enciende el teléfono para llamar", descripcion: "Toca el teléfono, arriba, y elige «Encender»." });
                if (estado.conexion.fase === "apagado") abrir("encender");
                return;
            }
            let marcacion;
            try {
                marcacion = await llamarTelefono(o.entrada, clave.actual());
                clave.renovar();
            } catch (e) {
                clave.trasError(e);
                const t = textoErrorLlamar(e);
                avisar({ tono: "error", titulo: t.titulo, descripcion: t.detalle });
                return;
            }
            const l = marcacion.llamada;
            if (l) actualizarLlamadaIris(l);
            setPanelAbierto(true);
            const id = await motor.llamar(marcacion.destino, { numero: o.numero, nombre: o.nombre, llamadaId: l?.id ?? null, interna: !l });
            if (!id) avisar({ tono: "error", titulo: "No se pudo llamar", descripcion: "Revisa el micrófono y la conexión, e intenta otra vez." });
        },
        [estado.conexion.fase, clave, motor, actualizarLlamadaIris],
    );

    const valor = useMemo<ContextoTelefono>(
        () => ({
            estado,
            mio,
            preferencia,
            encender,
            apagar,
            reintentar,
            usarAqui,
            contestar,
            rechazar: (id) => motor.rechazar(id),
            colgar: (id) => motor.colgar(id),
            silenciar: (id, si) => motor.silenciar(id, si),
            espera: (id, si) => motor.espera(id, si),
            tono: (id, d) => motor.tono(id, d),
            transferir: (id, n) => motor.transferir(id, n),
            llamar,
            llamadaIris,
            actualizarLlamadaIris,
            cubre,
            dialogo,
            abrir,
            panelAbierto,
            setPanelAbierto,
        }),
        [estado, mio, preferencia, encender, apagar, reintentar, usarAqui, contestar, motor, llamar, llamadaIris, actualizarLlamadaIris, cubre, dialogo, panelAbierto],
    );

    return <Contexto value={valor}>{children}</Contexto>;
}
