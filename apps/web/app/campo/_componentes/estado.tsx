"use client";

import { horaCorta, indicadoresEstado, partesEnvio, resumenEnvio, totalPorEnviar, type EstadoUbicacionIndicador, type IdIndicador, type Indicador } from "@iris/sync/campo";
import { Boton, cn, Interruptor } from "@iris/ui";
import { ArrowUp, Building2, ChevronRight, CloudCheck, CloudOff, CloudUpload, KeyRound, MapPin, MapPinOff, TriangleAlert } from "lucide-react";
import { useState } from "react";
import type { EstadoUbicacion } from "@/lib/campo/ubicacion";
import { RUTA_ENTRAR } from "./app-campo";
import { useCampo } from "./contexto";
import { HojaCampo } from "./hoja";
import { textoIndicador, useUbicacion } from "./ubicacion";

/** La ubicación del rastreador de la PWA en la forma de los indicadores (`@iris/sync/campo`). */
export function ubicacionIndicador(e: EstadoUbicacion): EstadoUbicacionIndicador {
    if (e.permiso === "no_disponible") {
        return "sin_gps";
    }
    if (e.enTurno && (!e.activa || e.permiso !== "concedido" || e.gps === "sin_senal")) {
        return "apagada_en_turno";
    }
    return e.activa ? "activa" : "apagada";
}

/** Fondo y tinta de cada tono (claro y oscuro con los tokens de la app). */
const TONO: Record<Indicador["tono"], string> = {
    marca: "bg-[var(--campo-marca-suave)] text-[var(--campo-marca-suave-texto)]",
    success: "bg-[var(--campo-panel)] text-success",
    neutral: "bg-[var(--campo-panel-2)] text-muted-foreground",
    info: "bg-[var(--campo-info-fondo)] text-info",
    warning: "bg-[var(--campo-aviso-fondo)] text-warning",
    error: "bg-[var(--campo-error-fondo)] text-error",
};

const FONDO_MARCA: Record<Indicador["tono"], string> = {
    marca: "bg-primary text-primary-foreground",
    success: "bg-success text-white",
    neutral: "bg-muted-foreground text-white",
    info: "bg-info text-white",
    warning: "bg-warning text-white",
    error: "bg-error text-white",
};

/** El icono de cada forma: check, flecha, nube tachada, llave o el pin del GPS. */
function IconoIndicador({ indicador: i, className }: { indicador: Indicador | undefined; className: string }) {
    switch (i?.forma) {
        case undefined:
        case "al_dia":
            return <CloudCheck aria-hidden className={className} />;
        case "pendientes":
            return i.tono === "error" ? <TriangleAlert aria-hidden className={className} /> : <ArrowUp aria-hidden className={className} />;
        case "sin_senal":
        case "sin_senal_pendientes":
            return <CloudOff aria-hidden className={className} />;
        case "bloqueada":
            return <KeyRound aria-hidden className={className} />;
        case "activa":
            return <MapPin aria-hidden className={className} />;
        default:
            return <MapPinOff aria-hidden className={className} />;
    }
}

/**
 * Los indicadores del encabezado, junto a buscar (opción elegida por el PM): la nube de
 * sincronización (conexión y envíos: check discreto, flecha y número en teal, nube tachada en
 * naranja con o sin número) y el GPS. Tocar cualquiera abre la hoja de estado en su sección. Zona
 * táctil de 44 px aunque el icono se vea más chico. `compacta`: la copia de la barra compacta (sin
 * los ganchos de las pruebas, que son del encabezado).
 */
export function IndicadoresEstado({ pedirEmpresa, compacta = false }: { pedirEmpresa: () => void; compacta?: boolean }) {
    const { estado } = useCampo();
    const { estado: ubicacion, envio } = useUbicacion();
    const [abierta, setAbierta] = useState<IdIndicador | null>(null);
    const lista = indicadoresEstado(estado, ubicacionIndicador(ubicacion));
    const actuar = lista.some((i) => i.actuar);
    const r = resumenEnvio(estado);
    const { pendientes, rechazos } = partesEnvio(r);
    return (
        <>
            <section
                aria-label="Estado del teléfono"
                data-slot={compacta ? "indicadores-compactos" : "barra-conexion"}
                data-conexion={estado.conexion}
                data-actuar={actuar ? "true" : undefined}
                className="flex shrink-0 items-center"
            >
                {lista.map((i) => {
                    return (
                        <Boton
                            key={i.id}
                            variante="fantasma"
                            aria-label={i.etiqueta}
                            title={i.texto}
                            data-indicador={i.id}
                            data-forma={i.forma}
                            data-tono={i.tono}
                            data-testid={i.id === "ubicacion" && !compacta ? "indicador-ubicacion" : undefined}
                            className="relative grid size-11 place-content-center rounded-full p-0 md:size-11"
                            onClick={() => setAbierta(i.id)}
                        >
                            <span aria-hidden className={cn("grid place-content-center rounded-full border border-border", i.actuar ? "size-9" : "size-8", TONO[i.tono])}>
                                <IconoIndicador indicador={i} className="size-[1.125rem]" />
                            </span>
                            {i.marca !== null ? (
                                <span
                                    aria-hidden
                                    data-slot="marca-indicador"
                                    className={cn(
                                        "absolute top-0 right-0 grid place-content-center rounded-full text-[0.625rem] leading-4 font-bold tabular-nums ring-2 ring-[var(--campo-lienzo)]",
                                        typeof i.marca === "number" ? "min-w-4 px-1" : "size-2.5",
                                        FONDO_MARCA[i.tono],
                                    )}
                                >
                                    {typeof i.marca === "number" ? (i.marca > 99 ? "99+" : i.marca) : null}
                                </span>
                            ) : null}
                            {/* Texto para lectores de pantalla y pruebas (no se ve). */}
                            <span className="sr-only">
                                {i.id === "sincronizacion" ? (
                                    <>
                                        <span data-testid={compacta ? undefined : "estado-conexion"}>{r.texto}</span>
                                        {pendientes.length > 0 || rechazos ? <span data-testid={compacta ? undefined : "pendientes-por-enviar"}>{[...pendientes, ...(rechazos ? [rechazos] : [])].join(" · ")}</span> : null}
                                    </>
                                ) : (
                                    `${textoIndicador(ubicacion)}${envio.pendientes > 0 ? ` · Pendientes de enviar: ${envio.pendientes}` : ""}`
                                )}
                            </span>
                        </Boton>
                    );
                })}
            </section>
            {abierta ? <HojaEstado seccion={abierta} alCerrar={() => setAbierta(null)} pedirEmpresa={pedirEmpresa} /> : null}
        </>
    );
}

/**
 * Hoja de estado (5.º tablero), solo con la sección del indicador tocado: «Ubicación» (el
 * interruptor «Compartir mi ubicación» reutiliza el turno de la ubicación en vivo: encenderlo es
 * «Iniciar turno», con su explicación; apagarlo, «Terminar turno») o «Conexión y envíos» (la señal,
 * la última sincronización y lo pendiente con su detalle, «Por enviar» y, con señal y algo
 * pendiente, «Enviar ahora»). Título, X y «Atrás» la cierran.
 */
export function HojaEstado({ seccion, alCerrar, pedirEmpresa }: { seccion: IdIndicador; alCerrar: () => void; pedirEmpresa: () => void }) {
    const { estado, navegar, motor } = useCampo();
    const ubicacion = useUbicacion();
    const [enviando, setEnviando] = useState(false);
    const r = resumenEnvio(estado);
    const { pendientes, rechazos } = partesEnvio(r);
    const total = totalPorEnviar(estado);
    const [nube] = indicadoresEstado(estado, ubicacionIndicador(ubicacion.estado));
    const ultima = estado.ultimaSincronizacion === null ? null : horaCorta(new Date(estado.ultimaSincronizacion).toISOString());
    const conSenal = estado.conexion === "en_linea" || estado.conexion === "desconocida";
    const enviarAhora = async () => {
        setEnviando(true);
        try {
            await motor.sincronizar();
        } catch {
            // Lo que no salió sigue en «Por enviar» (y en el número de la nube).
        } finally {
            setEnviando(false);
        }
    };
    const cambiarUbicacion = (encender: boolean) => {
        if (encender) {
            // La explicación (si falta) es otro diálogo: la hoja se cierra antes.
            alCerrar();
            void ubicacion.iniciarTurno();
        } else {
            ubicacion.terminarTurno();
        }
    };
    const enTurno = ubicacion.estado.enTurno;
    return (
        <HojaCampo abierto alCambiar={(a) => !a && alCerrar()} titulo={seccion === "ubicacion" ? "Ubicación" : "Conexión y envíos"} className="rounded-t-[2rem] sm:rounded-[2rem]">
            <div className="grid gap-3" data-slot="hoja-estado" data-seccion={seccion}>
                {seccion === "ubicacion" ? (
                <section data-seccion-estado="ubicacion" className="grid gap-2 rounded-[1.375rem] border border-border bg-[var(--campo-panel-2)] p-3">
                    <div className="flex items-start gap-3">
                        <span aria-hidden className={cn("grid size-11 shrink-0 place-content-center rounded-[0.875rem]", enTurno ? "bg-primary text-primary-foreground" : "bg-[var(--campo-panel)] text-muted-foreground")}>
                            {enTurno ? <MapPin className="size-5" /> : <MapPinOff className="size-5" />}
                        </span>
                        <div className="min-w-0 flex-1">
                            <Interruptor
                                etiqueta="Compartir mi ubicación"
                                descripcion={
                                    enTurno
                                        ? `Encendida · despacho te ve en el mapa mientras estás en turno. ${textoIndicador(ubicacion.estado)}.`
                                        : `Apagada · ${textoIndicador(ubicacion.estado)}. Al encenderla empieza tu turno: un punto por minuto con la app abierta.`
                                }
                                seleccionado={enTurno}
                                alCambiar={cambiarUbicacion}
                            />
                        </div>
                    </div>
                    <Boton
                        variante="enlace"
                        className="min-h-12 justify-start px-0 text-sm"
                        onClick={() => {
                            alCerrar();
                            ubicacion.abrirPanel();
                        }}
                    >
                        Detalle de la ubicación
                        <ChevronRight aria-hidden />
                    </Boton>
                </section>
                ) : (
                <section data-seccion-estado="sincronizacion" className="grid gap-2 rounded-[1.375rem] border border-border bg-[var(--campo-panel)] p-3">
                    <div className="flex items-start gap-3">
                        <span aria-hidden className={cn("grid size-11 shrink-0 place-content-center rounded-[0.875rem]", TONO[nube?.tono ?? "success"])}>
                            <IconoIndicador indicador={nube} className="size-5" />
                        </span>
                        <div className="grid min-w-0 flex-1 gap-1">
                            <p className="font-semibold">{r.texto}</p>
                            <p className="text-sm text-muted-foreground">
                                {ultima ? `Última sincronización ${ultima}.` : "Todavía no se sincroniza hoy."}
                                {estado.conexion === "sin_conexion" ? " Puedes seguir trabajando." : ""}
                            </p>
                            {estado.conexion === "sin_sesion" ? (
                                <Boton variante="secundario" tamano="lg" className="justify-self-start" onClick={() => window.location.assign(`${RUTA_ENTRAR}&motivo=expirada`)}>
                                    Entrar de nuevo
                                </Boton>
                            ) : estado.conexion === "sin_empresa" ? (
                                <Boton
                                    variante="secundario"
                                    tamano="lg"
                                    className="justify-self-start"
                                    onClick={() => {
                                        alCerrar();
                                        pedirEmpresa();
                                    }}
                                >
                                    <Building2 aria-hidden />
                                    Elegir empresa
                                </Boton>
                            ) : null}
                        </div>
                    </div>
                    <div className="flex items-start gap-3 border-t border-border pt-2">
                        <span aria-hidden className={cn("grid size-11 shrink-0 place-content-center rounded-[0.875rem]", total > 0 ? TONO[r.rechazos > 0 ? "error" : "warning"] : TONO.neutral)}>
                            {r.rechazos > 0 ? <TriangleAlert className="size-5" /> : <CloudUpload className="size-5" />}
                        </span>
                        <div className="grid min-w-0 flex-1 gap-1">
                            <p className="font-semibold">{total > 0 ? `${total} por enviar` : "Todo enviado"}</p>
                            <p className="text-sm text-muted-foreground">
                                {total > 0
                                    ? `${[...pendientes, ...(rechazos ? [rechazos] : [])].join(" · ")}. ${r.rechazos > 0 ? "Revisa los rechazados." : "Se envían solos al volver la señal."}`
                                    : "No queda nada en el teléfono por enviar."}
                            </p>
                            {total > 0 ? (
                                <div className="flex flex-wrap gap-2">
                                    {conSenal && r.porEnviar + Math.max(0, r.archivos) > 0 ? (
                                        <Boton tamano="lg" disabled={enviando || estado.sincronizando} onClick={() => void enviarAhora()}>
                                            <CloudUpload aria-hidden />
                                            {enviando || estado.sincronizando ? "Enviando…" : "Enviar ahora"}
                                        </Boton>
                                    ) : null}
                                    <Boton
                                        variante="secundario"
                                        tamano="lg"
                                        onClick={() => {
                                            alCerrar();
                                            navegar({ tipo: "pendientes" });
                                        }}
                                    >
                                        Ver «Por enviar»
                                        <ChevronRight aria-hidden />
                                    </Boton>
                                </div>
                            ) : null}
                        </div>
                    </div>
                </section>
                )}
            </div>
        </HojaCampo>
    );
}
