"use client";

import type { OrdenVista } from "@iris/sync";
import { esErrorDeApi, esErrorMicrofono, MotorTelefono, type ConfigUa, type EstadoTelefono, type LlamadaTelefono, type TelefonoMio, type UaSip } from "@iris/telefono";
import { cargarJssip } from "@iris/telefono/jssip";
import { Aviso, Boton, ControlesLlamada, ControlLlamada, Tarjeta, TarjetaContenido, TecladoTelefono, cn } from "@iris/ui";
import { ClipboardList, CloudUpload, Grid3x3, Mic, MicOff, Phone, PhoneMissed, PhoneOff, PhoneOutgoing, RotateCw, Signal, Smartphone, TriangleAlert, X } from "lucide-react";
import { createContext, use, useEffect, useState, useSyncExternalStore, type ReactNode } from "react";
import { listarLlamadasCentralita } from "@/lib/centralita/api";
import { duracionLlana, enlaceTelefono, filasLlamadas, finDeLlamada, formaEmpresa, horaLlamada, numerosDeOrden, senalDebil, textoSinEmpresa, type FinLlamada, type Llamada, type NumeroOrden } from "@/lib/campo/telefono";
import { nuevaClaveIdempotencia } from "@/lib/api/cliente";
import { abrirTelefono, leerTelefono, llamarTelefono } from "@/lib/telefono/api";
import { ErrorMicrofono, pedirMicrofono, permisoMicrofono, soltarMicrofono } from "@/lib/voz/medios";
import { useCampo } from "./contexto";
import { HojaCampo } from "./hoja";

/*
 * Llamar al cliente desde la orden (E4c fase 3, contratos/voz.md §3.12; maqueta aprobada por el PM:
 * pantallas 23 a 36). «Llamar por la empresa» usa la extensión del técnico por el puente de Iris
 * (`@iris/telefono`, el mismo motor del panel, con `dispositivo: app`: solo marca), solo con señal y
 * con el teléfono disponible; el micrófono se pide al tocarlo, con una explicación antes del
 * permiso del navegador. «Llamar desde mi celular» abre `tel:` con el número de la orden guardada
 * (también sin señal) y encola `llamada.directa` (se anota en la orden con la hora real). La conexión
 * con la centralita se abre solo para llamar y se cierra al colgar.
 */

/** Lo que ve el técnico en la orden después de intentar llamar por la empresa (pantallas 28–30 y 36). */
export type AvisoLlamada = "iris" | "senal" | "microfono" | "no_contesto" | "ocupado" | "limite";

type Activa = { orden: OrdenVista; numero: NumeroOrden; id: string | null };

type ContextoTelefonoCampo = {
    /**
     * Puede llamar con su extensión y anotar la llamada directa (`centralita.llamar`). Sin él, «Llamar»
     * es el `tel:` de siempre (no hay dónde anotarla).
     */
    puede: boolean;
    mio: TelefonoMio | null;
    estado: EstadoTelefono;
    /** Abre la hoja «Llamar a…» de la orden. */
    abrirHoja: (orden: OrdenVista) => void;
    aviso: { ordenId: number; tipo: AvisoLlamada; numero: NumeroOrden } | null;
    cerrarAviso: () => void;
    llamarEmpresa: (orden: OrdenVista, numero: NumeroOrden) => Promise<void>;
    llamarCelular: (orden: OrdenVista, numero: NumeroOrden) => Promise<void>;
    /** Sube cuando termina una llamada (la tarjeta «Llamadas» se vuelve a leer). */
    version: number;
};

const Contexto = createContext<ContextoTelefonoCampo | null>(null);

export function useTelefonoCampo(): ContextoTelefonoCampo | null {
    return use(Contexto);
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

// El agente SIP (JsSIP) se carga una vez, al llamar por primera vez (import dinámico).
let fabricaUa: ((c: ConfigUa) => UaSip) | null = null;
function crearUa(c: ConfigUa): UaSip {
    if (!fabricaUa) throw new Error("JsSIP sin cargar");
    return fabricaUa(c);
}

/** Espera a que el motor quede listo (o falle), hasta `ms`. */
function esperarListo(motor: MotorTelefono, ms = 15_000): Promise<EstadoTelefono["conexion"]> {
    return new Promise((resolver) => {
        const fin = (c: EstadoTelefono["conexion"]) => {
            quitar();
            window.clearTimeout(t);
            resolver(c);
        };
        const revisar = () => {
            const c = motor.leer().conexion;
            if (c.fase === "listo" || c.fase === "error" || c.fase === "no_disponible") fin(c);
        };
        const quitar = motor.suscribir(revisar);
        const t = window.setTimeout(() => fin(motor.leer().conexion), ms);
        revisar();
    });
}

type Conexion = { effectiveType?: string; rtt?: number };
const redDelNavegador = (): Conexion | undefined => (navigator as Navigator & { connection?: Conexion }).connection;

/**
 * `registrar`: la app de campo recibe cómo apagar el teléfono (cuelga y suelta el micrófono) para
 * llamarlo al cerrar sesión antes de esperar a nada; `null` al desmontarse.
 */
export function ProveedorTelefonoCampo({ children, registrar }: { children: ReactNode; registrar?: (detener: (() => void) | null) => void }) {
    const { estado: sync, sesion, motor: cola, notificar } = useCampo();
    const puedeLlamar = sesion.permisos?.includes("centralita.llamar") ?? false;
    const [mio, setMio] = useState<TelefonoMio | null>(null);
    const [releer, setReleer] = useState(0);
    const [hoja, setHoja] = useState<OrdenVista | null>(null);
    const [permiso, setPermiso] = useState<{ orden: OrdenVista; numero: NumeroOrden } | null>(null);
    const [aviso, setAviso] = useState<ContextoTelefonoCampo["aviso"]>(null);
    const [activa, setActiva] = useState<Activa | null>(null);
    const [version, setVersion] = useState(0);
    const [motor] = useState(
        () =>
            new MotorTelefono({
                abrir: () => abrirTelefono(undefined, "app"),
                crearUa,
                pedirMicrofono,
                reproducir,
                timbre: () => ({ detener: () => undefined }),
                revisar: () => setReleer((n) => n + 1),
                registrar: false,
            }),
    );
    const estado = useSyncExternalStore(motor.suscribir, motor.leer, motor.leer);

    // Al desmontarse (cerrar sesión, cambiar de empresa, salir) no queda ninguna llamada ni micrófono abiertos.
    useEffect(() => {
        registrar?.(() => motor.detenerUa());
        return () => {
            registrar?.(null);
            motor.detenerUa();
        };
    }, [motor, registrar]);

    // ¿Puedo llamar por la empresa? Solo con señal (y se relee al volver la señal).
    const enLinea = sync.conexion === "en_linea";
    useEffect(() => {
        if (!enLinea || !puedeLlamar) return;
        const control = new AbortController();
        leerTelefono(control.signal).then(
            (r) => !control.signal.aborted && setMio(r),
            () => undefined,
        );
        return () => control.abort();
    }, [enLinea, puedeLlamar, releer]);

    function terminar(a: Activa, visto: { contestada: boolean; causa: string | null; desde: number | null; cortada: boolean }) {
        motor.apagado(null);
        setActiva(null);
        setVersion((v) => v + 1);
        const fin: FinLlamada = visto.cortada && !visto.contestada ? "fallo" : finDeLlamada(visto.causa ?? undefined, visto.contestada);
        if (fin === "terminada") {
            const duracion = visto.desde === null ? "" : ` · ${duracionLlana((Date.now() - visto.desde) / 1000)}`;
            notificar({ tono: "success", texto: `Llamada terminada${duracion} · quedó en la orden` });
        } else if (fin === "no_contesto" || fin === "ocupado") {
            setAviso({ ordenId: a.orden.id, tipo: fin, numero: a.numero });
        } else {
            setAviso({ ordenId: a.orden.id, tipo: "iris", numero: a.numero });
        }
    }

    // La llamada en curso: lo último que dijo el motor de ella. Al terminar, el aviso o el resumen.
    const local = activa?.id ? (estado.llamadas.find((l) => l.id === activa.id) ?? null) : null;
    useEffect(() => {
        if (!activa?.id) return;
        const visto = { contestada: false, causa: null as string | null, hecho: false, desde: null as number | null, cortada: false };
        const quitar = motor.suscribir(() => {
            if (visto.hecho) return;
            const l = motor.leer().llamadas.find((x) => x.id === activa.id);
            if (l) {
                if (l.desde !== null) {
                    visto.contestada = true;
                    visto.desde = l.desde;
                }
                if (l.fase === "terminada") visto.causa = l.causa;
                return;
            }
            // Terminó: se cierra la conexión (la app solo la abre para llamar). Una sola vez: apagar el
            // motor vuelve a avisar a los oyentes.
            visto.hecho = true;
            // ¿Se cayó la conexión con la centralita mientras tanto? Entonces no fue el cliente.
            visto.cortada = motor.leer().conexion.fase === "error";
            quitar();
            terminar(activa, visto);
        });
        return quitar;
        // `terminar` cierra sobre el estado de este render; la llamada activa no cambia mientras dura.
        // eslint-disable-next-line react-hooks/exhaustive-deps
    }, [activa, motor]);

    async function marcar(orden: OrdenVista, numero: NumeroOrden) {
        setAviso(null);
        setActiva({ orden, numero, id: null });
        const fallar = (tipo: AvisoLlamada) => {
            motor.apagado(null);
            setActiva(null);
            setAviso({ ordenId: orden.id, tipo, numero });
        };
        try {
            fabricaUa ??= await cargarJssip();
        } catch {
            return fallar("iris");
        }
        void motor.encender(mio?.extension ?? null);
        const c = await esperarListo(motor);
        if (c.fase !== "listo") {
            return fallar(c.fase === "error" && c.error.tipo === "microfono" ? "microfono" : "iris");
        }
        let marcacion;
        try {
            marcacion = await llamarTelefono({ ordenId: orden.id, telefono: numero.clave }, nuevaClaveIdempotencia());
        } catch (e) {
            return fallar(esErrorDeApi(e) && e.codigo === "limite_llamadas" ? "limite" : "iris");
        }
        const nombre = orden.cliente?.nombre ?? orden.contacto.nombre ?? numero.telefono.mostrar;
        let id: string | null = null;
        try {
            id = await motor.llamar(marcacion.destino, { numero: numero.telefono.mostrar, nombre, llamadaId: marcacion.llamada?.id ?? null, interna: false });
        } catch {
            id = null;
        }
        if (!id) {
            const conexion = motor.leer().conexion;
            return fallar(conexion.fase === "error" && conexion.error.tipo === "microfono" ? "microfono" : "iris");
        }
        setActiva({ orden, numero, id });
    }

    async function llamarEmpresa(orden: OrdenVista, numero: NumeroOrden) {
        if (senalDebil(redDelNavegador())) {
            setHoja(null);
            setAviso({ ordenId: orden.id, tipo: "senal", numero });
            return;
        }
        // El permiso se consulta antes de cerrar la hoja: la de la explicación se abre en el mismo
        // momento en que se cierra esta (así «Atrás» del teléfono queda con una sola entrada).
        const p = await permisoMicrofono();
        setHoja(null);
        if (p === "denied") {
            setAviso({ ordenId: orden.id, tipo: "microfono", numero });
            return;
        }
        if (p !== "granted") {
            // Primero la explicación; el permiso del navegador, al tocar «Permitir el micrófono».
            setPermiso({ orden, numero });
            return;
        }
        await marcar(orden, numero);
    }

    async function permitirMicrofono() {
        const p = permiso;
        setPermiso(null);
        if (!p) return;
        try {
            soltarMicrofono(await pedirMicrofono());
        } catch (e) {
            setAviso({ ordenId: p.orden.id, tipo: e instanceof ErrorMicrofono || esErrorMicrofono(e) ? "microfono" : "iris", numero: p.numero });
            return;
        }
        await marcar(p.orden, p.numero);
    }

    async function llamarCelular(orden: OrdenVista, numero: NumeroOrden) {
        setHoja(null);
        setPermiso(null);
        setAviso(null);
        // Primero en la cola (con la hora de ahora): si se cierra la app, la llamada igual queda anotada.
        try {
            await cola.registrar("llamada.directa", orden.id, { telefono: numero.clave });
        } catch {
            // Sin poder anotarla igual se llama: el técnico la necesita.
        }
        const hora = horaLlamada(new Date().toISOString());
        notificar(
            cola.obtenerEstado().conexion === "sin_conexion"
                ? { tono: "warning", texto: "Llamada directa guardada en el teléfono: se anota en la orden al volver la señal" }
                : { tono: "success", texto: `Llamada directa anotada en la orden (${hora})` },
        );
        window.location.href = enlaceTelefono(numero.telefono);
    }

    const valor: ContextoTelefonoCampo = {
        puede: puedeLlamar,
        mio,
        estado,
        abrirHoja: (o) => {
            setAviso(null);
            setHoja(o);
        },
        aviso,
        cerrarAviso: () => setAviso(null),
        llamarEmpresa,
        llamarCelular,
        version,
    };

    return (
        <Contexto value={valor}>
            {children}
            {hoja ? <HojaLlamar orden={hoja} alCerrar={() => setHoja(null)} /> : null}
            <HojaPermiso
                abierto={permiso !== null}
                alCerrar={() => setPermiso(null)}
                alPermitir={() => void permitirMicrofono()}
                alCelular={() => (permiso ? void llamarCelular(permiso.orden, permiso.numero) : undefined)}
            />
            {activa ? <PantallaLlamada activa={activa} local={local} extension={mio?.extension ?? null} motor={motor} /> : null}
        </Contexto>
    );
}

/** El botón «Llamar» de la orden: abre la hoja (o, sin proveedor, el `tel:` de siempre). */
export function BotonLlamarOrden({ orden, className, etiqueta = "Llamar" }: { orden: OrdenVista; className: string; etiqueta?: string }) {
    const t = useTelefonoCampo();
    const numeros = numerosDeOrden(orden);
    if (numeros.length === 0) return null;
    if (!t?.puede) {
        const primero = numeros[0];
        return primero ? (
            <Boton asChild variante="secundario" className={className}>
                <a href={enlaceTelefono(primero.telefono)} aria-label={etiqueta} title={etiqueta}>
                    <Phone aria-hidden />
                </a>
            </Boton>
        ) : null;
    }
    return (
        <Boton variante="secundario" className={className} aria-label="Llamar al cliente" title="Llamar al cliente" data-slot="llamar-cliente" onClick={() => t.abrirHoja(orden)}>
            <Phone aria-hidden />
        </Boton>
    );
}

/** «Llamar a …» (pantallas 24, 24b y 31): el número, por la empresa o desde mi celular. */
function HojaLlamar({ orden, alCerrar }: { orden: OrdenVista; alCerrar: () => void }) {
    const t = useTelefonoCampo();
    const { estado } = useCampo();
    const numeros = numerosDeOrden(orden);
    const [elegido, setElegido] = useState<NumeroOrden["clave"] | undefined>(numeros[0]?.clave);
    if (!t) return null;
    const numero = numeros.find((n) => n.clave === elegido) ?? numeros[0];
    const forma = formaEmpresa(estado.conexion, t.mio);
    const sinEmpresa = forma.ofrecer ? null : textoSinEmpresa(forma.motivo);
    const sinSenal = !forma.ofrecer && forma.motivo === "sin_senal";
    const nombre = orden.cliente?.nombre ?? orden.contacto.nombre ?? "el cliente";
    return (
        <HojaCampo abierto alCambiar={(a) => (a ? undefined : alCerrar())} titulo={`Llamar a ${nombre}`}>
            <div className="grid gap-3" data-slot="hoja-llamar" data-empresa={forma.ofrecer ? "si" : forma.motivo}>
                <div role="radiogroup" aria-label="Número" className="grid gap-2">
                    {numeros.map((n) => {
                        const on = n.clave === numero?.clave;
                        const Icono = n.telefono.tipo === "movil" || n.clave === "movil" ? Smartphone : Phone;
                        return (
                            <Boton
                                key={n.clave}
                                variante="secundario"
                                role="radio"
                                aria-checked={on}
                                data-slot="numero-llamar"
                                className={cn("h-auto min-h-16 w-full justify-start gap-3 rounded-xl px-3 py-2 text-left font-normal", on && "border-primary ring-1 ring-primary")}
                                onClick={() => setElegido(n.clave)}
                            >
                                <Icono aria-hidden className="size-5 shrink-0" />
                                <span className="grid min-w-0 flex-1">
                                    <b className="font-semibold">{n.etiqueta}</b>
                                    <span className="text-muted-foreground tabular-nums">{n.telefono.mostrar}</span>
                                </span>
                            </Boton>
                        );
                    })}
                </div>
                {sinEmpresa ? (
                    <Aviso tono="warning" titulo={sinEmpresa.titulo} icono={sinSenal ? Signal : PhoneOff} data-slot="sin-empresa">
                        {sinEmpresa.detalle}
                    </Aviso>
                ) : null}
                {numero && forma.ofrecer ? (
                    <Boton tamano="lg" className="h-auto min-h-14 w-full flex-col gap-0 py-2" data-slot="llamar-empresa" onClick={() => void t.llamarEmpresa(orden, numero)}>
                        <span className="flex items-center gap-2">
                            <Phone aria-hidden />
                            Llamar por la empresa
                        </span>
                        <span className="text-xs font-medium opacity-90">El cliente ve el número de la empresa · queda en la orden</span>
                    </Boton>
                ) : null}
                {numero ? (
                    <Boton tamano="lg" variante={forma.ofrecer ? "secundario" : "primario"} className="w-full" data-slot="llamar-celular" onClick={() => void t.llamarCelular(orden, numero)}>
                        <Smartphone aria-hidden />
                        Llamar desde mi celular
                    </Boton>
                ) : null}
                <p className="flex gap-2 text-sm text-muted-foreground">
                    <TriangleAlert aria-hidden className="mt-0.5 size-4 shrink-0 text-warning" />
                    <span>
                        El cliente verá <b className="text-foreground">tu número personal</b>.{" "}
                        {sinSenal ? "La llamada directa se anota en la orden en cuanto vuelva la señal." : "No pasa por Iris: en la orden queda solo «llamada directa», sin duración ni audio."}
                    </span>
                </p>
            </div>
        </HojaCampo>
    );
}

/** El micrófono se pide al usarlo, con esta explicación antes del permiso del navegador (pantalla 32). */
function HojaPermiso({ abierto, alCerrar, alPermitir, alCelular }: { abierto: boolean; alCerrar: () => void; alPermitir: () => void; alCelular: () => void }) {
    return (
        <HojaCampo abierto={abierto} alCambiar={(a) => (a ? undefined : alCerrar())} titulo="Iris Campo necesita el micrófono para esta llamada">
            <div className="grid gap-3" data-slot="hoja-permiso">
                <span aria-hidden className="grid size-16 place-content-center justify-self-center rounded-full bg-[var(--hx-teal-soft,var(--campo-panel-2))] text-primary">
                    <Mic className="size-6" />
                </span>
                <p className="text-center text-sm text-muted-foreground">
                    Solo lo usa mientras hablas con el cliente. Tu teléfono te va a preguntar: toca <b className="text-foreground">Permitir</b>.
                </p>
                <Boton tamano="lg" className="w-full" onClick={alPermitir}>
                    <Mic aria-hidden />
                    Permitir el micrófono
                </Boton>
                <Boton tamano="lg" variante="secundario" className="w-full" onClick={alCelular}>
                    <Smartphone aria-hidden />
                    Mejor desde mi celular
                </Boton>
            </div>
        </HojaCampo>
    );
}

const AVISOS: Record<AvisoLlamada, { tono: "error" | "warning"; icono: typeof Phone; titulo: string; detalle: string; reintentar: string | null }> = {
    iris: {
        tono: "error",
        icono: PhoneOff,
        titulo: "No se pudo llamar",
        detalle: "Iris no llega a la centralita de la oficina en este momento. Vuelve a intentarlo en un rato. Si es urgente, puedes llamar desde tu celular: el cliente verá tu número.",
        reintentar: "Reintentar",
    },
    senal: {
        tono: "warning",
        icono: Signal,
        titulo: "La señal está muy débil para llamar por Iris",
        detalle: "Con esta señal el cliente no te oiría bien. Busca mejor señal o llama desde tu celular.",
        reintentar: "Reintentar",
    },
    microfono: {
        tono: "error",
        icono: MicOff,
        titulo: "La app no tiene permiso para usar el micrófono",
        detalle: "Toca el candado junto a la dirección, arriba › Micrófono › Permitir. Luego toca «Reintentar».",
        reintentar: "Reintentar",
    },
    no_contesto: { tono: "warning", icono: PhoneMissed, titulo: "No contestó", detalle: "La llamada quedó en la orden como «No contestó».", reintentar: "Llamar otra vez" },
    ocupado: { tono: "warning", icono: PhoneOff, titulo: "El número está ocupado", detalle: "Intenta de nuevo en unos minutos.", reintentar: "Llamar otra vez" },
    limite: { tono: "error", icono: PhoneOff, titulo: "Llegaste al límite de llamadas de hoy", detalle: "Lo pone la empresa. Si es urgente, llama desde tu celular.", reintentar: null },
};

/** El aviso de la orden después de intentar llamar por la empresa (pantallas 28–30 y 36). */
export function AvisoLlamadaOrden({ orden }: { orden: OrdenVista }) {
    const t = useTelefonoCampo();
    if (!t?.aviso || t.aviso.ordenId !== orden.id) return null;
    const { tipo, numero } = t.aviso;
    const a = AVISOS[tipo];
    return (
        <Aviso
            tono={a.tono}
            titulo={a.titulo}
            icono={a.icono}
            role={a.tono === "error" ? "alert" : "status"}
            data-slot="aviso-llamada"
            data-aviso={tipo}
            accion={
                <div className="grid w-full grid-cols-1 gap-2 min-[360px]:grid-cols-2">
                    {a.reintentar ? (
                        <Boton tamano="lg" onClick={() => void t.llamarEmpresa(orden, numero)}>
                            {tipo === "no_contesto" || tipo === "ocupado" ? <Phone aria-hidden /> : <RotateCw aria-hidden />}
                            {a.reintentar}
                        </Boton>
                    ) : null}
                    <Boton tamano="lg" variante="secundario" onClick={() => void t.llamarCelular(orden, numero)}>
                        <Smartphone aria-hidden />
                        Desde mi celular
                    </Boton>
                </div>
            }
        >
            {a.detalle}
        </Aviso>
    );
}

const iniciales = (nombre: string) =>
    nombre
        .split(/\s+/)
        .filter(Boolean)
        .slice(0, 2)
        .map((p) => p.charAt(0).toUpperCase())
        .join("");

function useSegundos(desde: number | null): number {
    const [ahora, setAhora] = useState(() => Date.now());
    useEffect(() => {
        if (desde === null) return;
        const t = window.setInterval(() => setAhora(Date.now()), 1000);
        return () => window.clearInterval(t);
    }, [desde]);
    return desde === null ? 0 : Math.max(0, Math.floor((ahora - desde) / 1000));
}

const reloj = (s: number) => `${String(Math.floor(s / 60)).padStart(2, "0")}:${String(s % 60).padStart(2, "0")}`;

/** La llamada por la empresa, a pantalla completa (pantallas 25, 26 y 33). */
function PantallaLlamada({ activa, local, extension, motor }: { activa: Activa; local: LlamadaTelefono | null; extension: string | null; motor: MotorTelefono }) {
    const [teclado, setTeclado] = useState(false);
    const [marcado, setMarcado] = useState("");
    const segundos = useSegundos(local?.desde ?? null);
    const nombre = activa.orden.cliente?.nombre ?? activa.orden.contacto.nombre ?? activa.numero.telefono.mostrar;
    const fase = local?.fase ?? "conectando";
    const enCurso = fase === "en_curso";
    const tipo = activa.numero.telefono.tipo === "movil" ? " · móvil" : "";
    return (
        <div role="dialog" aria-modal="true" aria-label={`Llamada con ${nombre}`} data-slot="pantalla-llamada" data-fase={fase} className="fixed inset-0 z-[60] flex flex-col gap-4 overflow-y-auto bg-background px-5 pt-[calc(env(safe-area-inset-top)+1.5rem)] pb-[calc(env(safe-area-inset-bottom)+1.75rem)]">
            {teclado ? (
                <div className="grid justify-items-center gap-1 text-center">
                    <b className="text-xl font-semibold">{nombre}</b>
                    <span className="font-mono text-muted-foreground tabular-nums">{reloj(segundos)}</span>
                </div>
            ) : (
                <div className="grid justify-items-center gap-1.5 pt-4 text-center">
                    <span aria-hidden className={cn("grid size-20 place-content-center rounded-full bg-primary/12 text-2xl font-bold text-primary", !enCurso && "ring-8 ring-primary/10 motion-safe:animate-pulse")}>
                        {iniciales(nombre)}
                    </span>
                    <b className="mt-3 text-[1.375rem] leading-tight font-bold tracking-[-0.02em]">{nombre}</b>
                    <span className="text-muted-foreground tabular-nums">
                        {activa.numero.telefono.mostrar}
                        {tipo}
                    </span>
                    {enCurso ? (
                        <span className="font-mono text-muted-foreground tabular-nums" data-slot="reloj-llamada">
                            {reloj(segundos)}
                        </span>
                    ) : (
                        <span className="flex items-center gap-1.5 text-muted-foreground" role="status">
                            <PhoneOutgoing aria-hidden className="size-4" />
                            {fase === "llamando" ? "Sonando…" : "Llamando…"}
                        </span>
                    )}
                    <span className="mt-1 inline-flex items-center gap-1 rounded-full bg-[var(--campo-panel-2)] px-2.5 py-0.5 text-xs font-semibold text-muted-foreground">
                        <ClipboardList aria-hidden className="size-3.5" />
                        <span className="font-mono">{activa.orden.numero}</span>
                    </span>
                </div>
            )}
            {teclado ? (
                <div className="grid gap-3">
                    <p className="text-center text-sm text-muted-foreground">Marca lo que te pida la grabación del otro lado.</p>
                    <p className="min-h-8 text-center font-mono text-2xl font-bold tracking-widest" aria-live="polite">
                        {marcado.split("").join(" · ")}
                    </p>
                    <TecladoTelefono
                        etiqueta="Teclado de la llamada"
                        deshabilitado={!enCurso || !local}
                        alTocar={(d) => {
                            if (!local) return;
                            setMarcado((m) => (m + d).slice(-16));
                            motor.tono(local.id, d);
                        }}
                    />
                </div>
            ) : null}
            <span className="flex-1" />
            <ControlesLlamada>
                <ControlLlamada icono={local?.silenciada ? MicOff : Mic} etiqueta={local?.silenciada ? "Activar micrófono" : "Silenciar"} activo={local?.silenciada ?? false} deshabilitado={!local} onClick={() => local && motor.silenciar(local.id, !local.silenciada)} />
                <span aria-hidden />
                <ControlLlamada icono={teclado ? X : Grid3x3} etiqueta={teclado ? "Ocultar" : "Teclado"} activo={teclado} deshabilitado={!enCurso} onClick={() => setTeclado((v) => !v)} />
            </ControlesLlamada>
            <div className="flex justify-center" data-slot="colgar">
                <ControlLlamada
                    icono={PhoneOff}
                    etiqueta="Colgar"
                    peligro
                    onClick={() => {
                        if (local) motor.colgar(local.id);
                        else motor.apagado(null);
                    }}
                />
            </div>
            <p className="text-center text-sm text-muted-foreground">
                Por Iris{extension ? ` con tu extensión ${extension}` : ""} · el cliente ve el número de la empresa
                <br />
                Queda en la orden y en la ficha del cliente
            </p>
        </div>
    );
}

/**
 * El teléfono en el encabezado (pantallas 23 y 27): un punto discreto (verde: se puede llamar por la
 * empresa; gris: no) que abre su hoja de estado. Solo si la persona puede llamar con su extensión.
 */
export function IndicadorTelefono() {
    const t = useTelefonoCampo();
    const { estado } = useCampo();
    const [abierta, setAbierta] = useState(false);
    if (!t?.puede) return null;
    const forma = formaEmpresa(estado.conexion, t.mio);
    const listo = forma.ofrecer;
    const texto = listo ? "Teléfono listo para llamar" : forma.motivo === "sin_senal" ? "Teléfono: sin señal" : forma.motivo === "revisando" ? "Teléfono: revisando" : "Teléfono: solo desde tu celular";
    const sin = forma.ofrecer ? null : textoSinEmpresa(forma.motivo);
    return (
        <>
            <Boton
                variante="fantasma"
                aria-label={texto}
                title={texto}
                data-slot="indicador-telefono"
                data-estado={listo ? "listo" : forma.motivo}
                className="relative grid size-11 place-content-center rounded-full p-0 md:size-11"
                onClick={() => setAbierta(true)}
            >
                <span aria-hidden className="grid size-8 place-content-center rounded-full border border-border">
                    <Phone className="size-[1.125rem]" />
                </span>
                <span aria-hidden className={cn("absolute top-1 right-1 size-2.5 rounded-full ring-2 ring-[var(--campo-lienzo)]", listo ? "bg-success" : "bg-[var(--hx-text-dim)]")} />
            </Boton>
            <HojaCampo abierto={abierta} alCambiar={setAbierta} titulo="Teléfono">
                <ul className="grid divide-y divide-border text-sm" data-slot="hoja-telefono">
                    <li className="flex gap-3 py-3">
                        <Phone aria-hidden className={cn("mt-0.5 size-5 shrink-0", listo ? "text-success" : "text-muted-foreground")} />
                        <span className="grid gap-1">
                            <b className="text-base font-semibold">{listo ? "Teléfono listo para llamar" : (sin?.titulo ?? "Revisando el teléfono…")}</b>
                            <span className="text-muted-foreground">
                                {listo ? `Extensión ${t.mio?.extension ?? ""}, por Iris. El cliente ve el número de la empresa.` : (sin?.detalle ?? "")}
                            </span>
                            <span className="text-muted-foreground">Por ahora solo llamas tú: las llamadas a tu extensión no suenan en la app.</span>
                        </span>
                    </li>
                    <li className="flex gap-3 py-3">
                        <Signal aria-hidden className="mt-0.5 size-5 shrink-0 text-muted-foreground" />
                        <span className="grid gap-1">
                            <b className="text-base font-semibold">Datos móviles</b>
                            <span className="text-muted-foreground">Una llamada usa cerca de 0,5 MB por minuto (unos 5 MB en 10 minutos).</span>
                        </span>
                    </li>
                </ul>
            </HojaCampo>
        </>
    );
}

/** Las llamadas de la orden (pantallas 34 y 35): del servidor (con señal) y las directas que siguen en la cola. */
export function TarjetaLlamadas({ orden }: { orden: OrdenVista }) {
    const t = useTelefonoCampo();
    const { estado } = useCampo();
    const [servidor, setServidor] = useState<{ ordenId: number; lista: Llamada[] } | "error" | null>(null);
    const [reintento, setReintento] = useState(0);
    const enLinea = estado.conexion === "en_linea";
    const pendientes = estado.cola.filter((op) => op.tipo === "llamada.directa" && op.ordenId === orden.id).map((op) => ({ id: op.id, ocurridoEn: op.ocurridoEn }));
    const nPendientes = pendientes.length;
    const version = t?.version ?? 0;
    const puede = t?.puede ?? false;
    useEffect(() => {
        if (!enLinea || !puede) return;
        const control = new AbortController();
        listarLlamadasCentralita({ ordenId: orden.id }, control.signal).then(
            (r) => !control.signal.aborted && setServidor({ ordenId: orden.id, lista: r.data ?? [] }),
            () => !control.signal.aborted && setServidor("error"),
        );
        return () => control.abort();
    }, [enLinea, puede, orden.id, nPendientes, version, reintento]);
    if (!t?.puede) return null;
    const lista = servidor && servidor !== "error" && servidor.ordenId === orden.id ? servidor.lista : [];
    const filas = filasLlamadas(lista, pendientes, t.mio?.extension ?? null);
    if (servidor === "error" && filas.length === 0) {
        return (
            <Aviso
                tono="warning"
                titulo="No se pudieron leer las llamadas de la orden"
                accion={
                    <Boton variante="secundario" onClick={() => setReintento((n) => n + 1)}>
                        <RotateCw aria-hidden />
                        Reintentar
                    </Boton>
                }
            />
        );
    }
    if (filas.length === 0) return null;
    return (
        <Tarjeta data-slot="llamadas-orden">
            <TarjetaContenido className="grid gap-1 p-4 text-sm">
                <h2 className="text-base font-semibold">Llamadas</h2>
                <ul className="divide-y divide-border">
                    {filas.map((f) => {
                        const Icono = f.tipo === "directa" ? Smartphone : f.tono === "warning" ? PhoneMissed : PhoneOutgoing;
                        return (
                            <li key={f.id} className="flex gap-3 py-2.5" data-slot="fila-llamada" data-tipo={f.tipo} data-pendiente={f.pendiente || undefined}>
                                <span aria-hidden className={cn("grid size-9 shrink-0 place-content-center rounded-full bg-[var(--campo-panel-2)]", f.tono === "success" ? "text-success" : f.tono === "warning" ? "text-warning" : "text-muted-foreground")}>
                                    <Icono className="size-[1.125rem]" />
                                </span>
                                <span className="grid min-w-0 flex-1 gap-0.5">
                                    <b className="font-semibold">{f.titulo}</b>
                                    <span className="text-muted-foreground">
                                        {f.detalle}
                                        {f.pendiente ? (
                                            <span className="ml-1 inline-flex items-center gap-1 text-warning">
                                                <CloudUpload aria-hidden className="size-3.5" />
                                                Se anota al volver la señal
                                            </span>
                                        ) : null}
                                    </span>
                                </span>
                            </li>
                        );
                    })}
                </ul>
                {servidor === "error" ? <p className="text-xs text-warning">No se pudieron leer las de la oficina; se muestran las de este teléfono.</p> : null}
            </TarjetaContenido>
        </Tarjeta>
    );
}
