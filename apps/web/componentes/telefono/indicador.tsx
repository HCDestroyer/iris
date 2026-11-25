"use client";

import { Aviso, Boton, cn, Dialogo, DialogoContenido, DialogoPie, PanelEmergente, Desplegable } from "@iris/ui";
import { AppWindow, Grid3x3, KeyRound, Lock, Mic, MicOff, Phone, PhoneCall, PhoneOff, RotateCw, Unplug, Volume2, WifiOff } from "lucide-react";
import { usePathname } from "next/navigation";
import { useState } from "react";
import { avisoError, puntoTelefono, textoMotivo } from "@iris/telefono";
import type { ErrorTelefono } from "@iris/telefono";
import { sonarTimbre } from "@/lib/voz/timbre";
import { useTelefono, type ContextoTelefono } from "./proveedor";

/*
 * El estado del teléfono (maqueta E4c, pantallas 01–03 y 19–22): un punto discreto en la barra
 * superior (verde: listo; gris: apagado en este navegador) que solo crece a un aviso cuando hay que
 * actuar; el aviso en llano con «Reintentar» sobre la pantalla; la primera vez, la invitación a
 * encenderlo y el diálogo que explica el permiso del micrófono.
 */

const ICONO_ERROR: Record<ErrorTelefono["tipo"], typeof Phone> = { sin_conexion: WifiOff, clave: KeyRound, microfono: MicOff, pestana: AppWindow, espera: RotateCw, cortado: Unplug };

/** Botón del teléfono en la barra superior. */
export function BotonTelefono() {
    const t = useTelefono();
    const [abierto, setAbierto] = useState(false);
    if (!t) return null;
    const punto = puntoTelefono(t.estado.conexion);
    if (!punto) return null;
    const c = t.estado.conexion;
    const Icono = c.fase === "error" ? ICONO_ERROR[c.error.tipo] : Phone;
    const disparador =
        punto.forma === "aviso" ? (
            <Boton
                variante="secundario"
                tamano="sm"
                data-slot="boton-telefono"
                data-estado={c.fase === "error" ? c.error.tipo : c.fase}
                aria-label={`Teléfono: ${punto.texto}`}
                className={cn("h-11 max-w-[9.5rem] gap-1.5 rounded-full bg-[var(--iris-tarjeta)] px-3.5 shadow-[var(--iris-sombra-flotante)]", punto.tono === "danger" ? "text-error shadow-[0_0_0_1px_color-mix(in_oklab,var(--hx-error)_45%,transparent)]" : "text-warning shadow-[0_0_0_1px_color-mix(in_oklab,var(--hx-warning)_45%,transparent)]")}
            >
                <Icono />
                <span className="truncate text-xs font-semibold">{punto.corto}</span>
            </Boton>
        ) : (
            <Boton
                variante="secundario"
                tamano="icono-sm"
                data-slot="boton-telefono"
                data-estado={c.fase}
                aria-label={`Teléfono: ${punto.texto}`}
                className="relative size-11 min-w-11 rounded-full border border-border bg-[var(--iris-tarjeta)] p-0 text-muted-foreground shadow-[var(--iris-sombra-flotante)] md:size-11 [&_svg]:size-[1.125rem]"
            >
                <PhoneCall />
                <span aria-hidden className={cn("absolute top-1.5 right-1.5 size-2 rounded-full ring-2 ring-card", punto.tono === "success" ? "bg-success" : "bg-[var(--iris-texto-apagado)]", c.fase === "conectando" && "motion-safe:animate-pulse")} />
            </Boton>
        );
    return (
        <PanelEmergente titulo="Teléfono de Iris" lado="bottom end" abierto={abierto} alCambiar={setAbierto} className="w-[22rem] max-w-[calc(100vw-1rem)]" disparador={disparador}>
            <EstadoTelefono t={t} alCerrar={() => setAbierto(false)} />
        </PanelEmergente>
    );
}

function EstadoTelefono({ t, alCerrar }: { t: ContextoTelefono; alCerrar: () => void }) {
    const c = t.estado.conexion;
    const ext = t.estado.extension ?? t.mio?.extension ?? null;
    return (
        <div className="grid gap-3" data-slot="estado-telefono">
            <div className="flex items-center gap-3">
                <span aria-hidden className="flex size-10 shrink-0 items-center justify-center rounded-full bg-primary/12 text-primary">
                    <PhoneCall className="size-5" />
                </span>
                <div className="grid min-w-0">
                    <p className="text-sm font-semibold">Teléfono de Iris</p>
                    <p className="text-[0.8125rem] text-muted-foreground">{ext ? `Tu extensión ${ext} suena aquí` : "Tu extensión de la centralita"}</p>
                </div>
            </div>
            {c.fase === "listo" ? (
                <>
                    <p className="flex items-center gap-2 text-sm font-medium text-success">
                        <span aria-hidden className="size-2 rounded-full bg-current" />
                        Listo para contestar y llamar
                    </p>
                    <div className="flex flex-wrap gap-2">
                        {t.mio?.puede.llamar ? (
                            <Boton
                                tamano="sm"
                                onClick={() => {
                                    alCerrar();
                                    t.abrir("marcar");
                                }}
                            >
                                <Grid3x3 />
                                Marcar
                            </Boton>
                        ) : null}
                        <Boton
                            tamano="sm"
                            variante="secundario"
                            onClick={() => {
                                const s = sonarTimbre();
                                window.setTimeout(s.detener, 900);
                            }}
                        >
                            <Volume2 />
                            Probar sonido
                        </Boton>
                    </div>
                    {t.mio ? (
                        <p className="text-xs text-muted-foreground">
                            Llamadas a la calle hoy: {t.mio.limite.usadasHoy} de {t.mio.limite.porDia}.
                        </p>
                    ) : null}
                </>
            ) : c.fase === "conectando" ? (
                <p className="text-sm text-muted-foreground">Conectando con la centralita…</p>
            ) : c.fase === "apagado" ? (
                <>
                    <p className="text-sm text-muted-foreground">Apagado en este navegador: tus llamadas no suenan aquí.</p>
                    <Boton
                        tamano="sm"
                        onClick={() => {
                            alCerrar();
                            t.abrir("encender");
                        }}
                    >
                        <Phone />
                        Encender el teléfono
                    </Boton>
                </>
            ) : c.fase === "error" ? (
                <AvisoDeError t={t} error={c.error} />
            ) : (
                <p className="text-sm text-muted-foreground">{textoMotivo(c.motivo)}</p>
            )}
            <Desplegable titulo="¿Qué significa el punto del teléfono?" className="text-xs text-muted-foreground" claseTitulo="text-xs font-medium">
                <ul className="mt-2 grid gap-1.5">
                    <li className="flex items-center gap-2">
                        <span aria-hidden className="size-2 shrink-0 rounded-full bg-success" />
                        Verde: listo, tus llamadas suenan aquí
                    </li>
                    <li className="flex items-center gap-2">
                        <span aria-hidden className="size-2 shrink-0 rounded-full bg-[var(--iris-texto-apagado)]" />
                        Gris: apagado en este navegador
                    </li>
                    <li className="flex items-center gap-2">
                        <span aria-hidden className="size-2 shrink-0 rounded-full bg-error" />
                        Si hay un problema, el punto se vuelve un aviso con lo que tienes que hacer
                    </li>
                </ul>
            </Desplegable>
            {c.fase === "listo" || c.fase === "conectando" || (c.fase === "error" && c.error.tipo !== "pestana") ? (
                <Boton
                    tamano="sm"
                    variante="fantasma"
                    className="justify-self-start text-muted-foreground"
                    onClick={() => {
                        alCerrar();
                        t.apagar();
                    }}
                >
                    <PhoneOff />
                    Apagar en este navegador
                </Boton>
            ) : null}
            <p className="text-xs text-muted-foreground">Mientras Iris esté abierto en esta pestaña, suena aunque estés en otra pantalla.</p>
        </div>
    );
}

/** El aviso de un error, en llano, con lo que hay que hacer. */
function AvisoDeError({ t, error, className }: { t: ContextoTelefono; error: ErrorTelefono; className?: string }) {
    const a = avisoError(error, t.estado.extension ?? t.mio?.extension ?? null);
    const Icono = ICONO_ERROR[error.tipo];
    return (
        <Aviso
            tono={a.tono === "danger" ? "error" : "warning"}
            titulo={a.titulo}
            icono={Icono}
            role="alert"
            className={className}
            data-slot="aviso-telefono"
            data-error={error.tipo}
            accion={
                a.accion === "reintentar" ? (
                    <Boton variante="secundario" tamano="sm" onClick={() => void t.reintentar()}>
                        <RotateCw />
                        Reintentar
                    </Boton>
                ) : a.accion === "usar_aqui" ? (
                    <Boton tamano="sm" onClick={() => void t.usarAqui()}>
                        <Phone />
                        Usar el teléfono aquí
                    </Boton>
                ) : undefined
            }
        >
            <span>{a.detalle}</span>
            {a.pasos ? (
                <ol className="mt-1.5 list-decimal space-y-0.5 pl-5">
                    {a.pasos.map((p, i) => (
                        <li key={p}>
                            {i === 0 ? (
                                <>
                                    Toca el candado <Lock aria-label="candado" className="inline size-3.5 align-[-2px]" /> junto a la dirección, arriba.
                                </>
                            ) : (
                                p
                            )}
                        </li>
                    ))}
                </ol>
            ) : null}
        </Aviso>
    );
}

/**
 * Sobre la pantalla (dentro de `<main>`): el aviso de error del teléfono y, la primera vez y solo en
 * Inicio, la invitación a encenderlo.
 */
export function AvisoTelefono() {
    const t = useTelefono();
    const ruta = usePathname();
    if (!t) return null;
    const c = t.estado.conexion;
    if (c.fase === "error") return <AvisoDeError t={t} error={c.error} className="mb-4" />;
    if (c.fase === "apagado" && t.preferencia === null && ruta === "/") {
        const ext = t.mio?.extension;
        return (
            <Aviso
                tono="info"
                titulo="Ya puedes contestar la centralita aquí en Iris"
                icono={PhoneCall}
                className="mb-4"
                data-slot="invitacion-telefono"
                accion={
                    <div className="flex flex-wrap gap-2">
                        <Boton tamano="sm" onClick={() => t.abrir("encender")}>
                            <Phone />
                            Encender el teléfono
                        </Boton>
                        <Boton tamano="sm" variante="fantasma" onClick={t.apagar}>
                            Ahora no
                        </Boton>
                    </div>
                }
            >
                {`Tu extensión${ext ? ` ${ext}` : ""} ahora suena en el navegador, sin teléfono de escritorio. Para hablar, Iris necesita usar tu micrófono.`}
            </Aviso>
        );
    }
    return null;
}

/** «Encender el teléfono de Iris»: los tres pasos y el permiso del micrófono. */
export function DialogoEncender({ t }: { t: ContextoTelefono }) {
    const [encendiendo, setEncendiendo] = useState(false);
    const cerrar = () => t.abrir(null);
    return (
        <Dialogo open onOpenChange={(a) => !a && cerrar()}>
            <DialogoContenido titulo="Encender el teléfono de Iris">
                <ol className="grid gap-3 text-sm" data-slot="pasos-encender">
                    <li className="flex gap-3">
                        <Paso n={1} />
                        <span>
                            Toca <b>Encender</b>.
                        </span>
                    </li>
                    <li className="flex gap-3">
                        <Paso n={2} />
                        <div className="grid min-w-0 flex-1 gap-2">
                            <span>
                                El navegador te va a preguntar si Iris puede usar el micrófono. Elige <b>Permitir</b>.
                            </span>
                            <div aria-hidden className="grid gap-2 rounded-lg border border-border bg-elevated p-3 text-[0.8125rem]">
                                <span className="flex items-center gap-1.5 text-muted-foreground">
                                    <Lock className="size-3.5" />
                                    {typeof window === "undefined" ? "" : window.location.host}
                                </span>
                                <span className="flex items-center gap-1.5 font-medium">
                                    <Mic className="size-4" />
                                    Usar tus micrófonos
                                </span>
                                <span className="flex justify-end gap-2">
                                    <span className="rounded-md border border-border px-2.5 py-1">Bloquear</span>
                                    <span className="rounded-md bg-primary px-2.5 py-1 font-semibold text-[var(--hx-on-primary)] ring-3 ring-primary/30">Permitir</span>
                                </span>
                            </div>
                        </div>
                    </li>
                    <li className="flex gap-3">
                        <Paso n={3} />
                        <span>Suena un timbre corto de prueba. Si lo oyes, ya está: el punto del teléfono se pone verde.</span>
                    </li>
                </ol>
                <p className="text-sm text-muted-foreground">Usa audífonos con micrófono si puedes: el cliente te oye mejor y no hay eco.</p>
                <DialogoPie>
                    <Boton
                        variante="fantasma"
                        onClick={() => {
                            if (t.preferencia === null) t.apagar();
                            cerrar();
                        }}
                    >
                        Ahora no
                    </Boton>
                    <Boton
                        disabled={encendiendo}
                        onClick={async () => {
                            setEncendiendo(true);
                            await t.encender();
                            setEncendiendo(false);
                            cerrar();
                        }}
                    >
                        <Phone />
                        {encendiendo ? "Encendiendo…" : "Encender"}
                    </Boton>
                </DialogoPie>
            </DialogoContenido>
        </Dialogo>
    );
}

function Paso({ n }: { n: number }) {
    return (
        <span aria-hidden className="flex size-6 shrink-0 items-center justify-center rounded-full bg-primary/12 text-xs font-bold text-primary">
            {n}
        </span>
    );
}
