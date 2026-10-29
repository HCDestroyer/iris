"use client";

import {
    Aviso,
    Boton,
    Cargando,
    Dialogo,
    DialogoCerrar,
    DialogoContenido,
    DialogoDisparador,
    DialogoPie,
    Entrada,
    Esqueleto,
    Tarjeta,
    TarjetaContenido,
    TarjetaEncabezado,
    TarjetaTitulo,
} from "@iris/ui";
import { Check, Copy, Mail, RotateCw } from "lucide-react";
import { useState } from "react";
import { CalificacionEncuesta, InsigniaAlerta, InsigniaEstadoEncuesta } from "@/componentes/encuestas/calificacion";
import { ErrorLectura } from "@/componentes/error-lectura";
import { ErrorApi, mensajeDeError } from "@/lib/api/errores";
import { encuestaDe, pedirEnlace } from "@/lib/encuestas/api-panel";
import type { Encuesta } from "@/lib/encuestas/presentacion";
import { useLectura } from "@/lib/lectura";
import { textoFechaHora } from "@/lib/reportes/presentacion";
import { useSesion } from "@/lib/sesion";

/**
 * Tarjeta «Encuesta» del detalle del ticket o de la orden (`encuestas.ver`): estado, calificación,
 * comentario y, mientras está pendiente por enlace, «Copiar enlace» (`encuestas.compartir`).
 */
export function TarjetaEncuesta({ filtro }: { filtro: { ticketId: number } | { ordenId: number } }) {
    const lectura = useLectura((signal) => encuestaDe(filtro, signal));
    return (
        <Tarjeta data-testid="tarjeta-encuesta">
            <TarjetaEncabezado>
                <TarjetaTitulo>Encuesta</TarjetaTitulo>
            </TarjetaEncabezado>
            <TarjetaContenido className="grid gap-3">
                {lectura.estado === "cargando" ? (
                    <Esqueleto className="h-16" />
                ) : lectura.estado === "error" ? (
                    <ErrorLectura error={lectura.error} reintentar={lectura.reintentar} que="la encuesta" />
                ) : lectura.datos === null ? (
                    <p className="text-sm text-muted-foreground">Sin encuesta. Se envía al resolver el ticket o al cerrar con éxito una instalación o visita.</p>
                ) : (
                    <DetalleEncuesta encuesta={lectura.datos} recargar={() => void lectura.refrescar()} />
                )}
            </TarjetaContenido>
        </Tarjeta>
    );
}

function DetalleEncuesta({ encuesta: e, recargar }: { encuesta: Encuesta; recargar: () => void }) {
    const { puede } = useSesion();
    return (
        <>
            <div className="flex flex-wrap items-center gap-2">
                <InsigniaEstadoEncuesta estado={e.estado} />
                <CalificacionEncuesta calificacion={e.calificacion} />
                {e.alerta ? <InsigniaAlerta /> : null}
            </div>
            {e.comentario ? <p className="text-sm break-words whitespace-pre-line" data-testid="comentario-encuesta">«{e.comentario}»</p> : null}
            <p className="text-xs text-muted-foreground tabular-nums">
                {e.canal === "correo" ? "Enviada por correo" : "Por enlace"} el {textoFechaHora(e.creadaEn)}
                {e.respondidaEn ? ` · respondida ${textoFechaHora(e.respondidaEn)}` : e.estado === "pendiente" ? ` · vence ${textoFechaHora(e.expiraEn)}` : ""}
            </p>
            {e.estado !== "pendiente" ? null : e.canal === "correo" ? (
                <EnviadaPorCorreo />
            ) : !e.enlaceDisponible ? (
                <p className="text-xs text-muted-foreground">El enlace no se puede volver a mostrar.</p>
            ) : puede(["encuestas.compartir"]) ? (
                <DialogoEnlace encuesta={e} recargar={recargar} />
            ) : null}
        </>
    );
}

function EnviadaPorCorreo() {
    return (
        <p className="inline-flex items-center gap-1.5 text-xs text-muted-foreground" data-testid="enviada-por-correo">
            <Mail aria-hidden className="size-4" />
            Enviada por correo
        </p>
    );
}

type EstadoEnlace =
    | { paso: "pidiendo" }
    | { paso: "listo"; enlace: string; expiraEn: string; copiado: boolean }
    | { paso: "por_correo" }
    | { paso: "cambio"; mensaje: string }
    | { paso: "error"; error: unknown };

/**
 * «Copiar enlace»: pide el enlace (`POST /encuestas/{id}/enlace`, auditado), lo copia y lo muestra
 * solo dentro del diálogo. Al cerrar se descarta: no queda en el estado de la página ni en la URL.
 */
function DialogoEnlace({ encuesta, recargar }: { encuesta: Encuesta; recargar: () => void }) {
    const [abierto, setAbierto] = useState(false);
    const [estado, setEstado] = useState<EstadoEnlace>({ paso: "pidiendo" });

    async function pedir() {
        setEstado({ paso: "pidiendo" });
        let r: { enlace: string; expiraEn: string };
        try {
            r = await pedirEnlace(encuesta.id);
        } catch (error) {
            const codigo = error instanceof ErrorApi ? error.codigo : undefined;
            if (codigo === "encuesta_por_correo") setEstado({ paso: "por_correo" });
            else if (codigo === "encuesta_respondida") setEstado({ paso: "cambio", mensaje: "El cliente ya respondió la encuesta." });
            else if (codigo === "encuesta_vencida") setEstado({ paso: "cambio", mensaje: "La encuesta venció." });
            else if (codigo === "encuesta_anulada") setEstado({ paso: "cambio", mensaje: "La encuesta se anuló (el caso se reabrió o se fusionó)." });
            else if (codigo === "enlace_no_disponible") setEstado({ paso: "cambio", mensaje: "El enlace no se puede volver a mostrar." });
            else setEstado({ paso: "error", error });
            return;
        }
        let copiado = false;
        try {
            if (navigator.clipboard) {
                await navigator.clipboard.writeText(r.enlace);
                copiado = true;
            }
        } catch {
            copiado = false;
        }
        setEstado({ paso: "listo", enlace: r.enlace, expiraEn: r.expiraEn, copiado });
    }

    function alCambiarAbierto(a: boolean) {
        setAbierto(a);
        if (a) {
            void pedir();
            return;
        }
        // Se descarta el enlace; si la encuesta cambió (respondida, vencida…), se relee la tarjeta.
        const cambio = estado.paso === "cambio" || estado.paso === "por_correo";
        setEstado({ paso: "pidiendo" });
        if (cambio) recargar();
    }

    return (
        <Dialogo open={abierto} onOpenChange={alCambiarAbierto}>
            <DialogoDisparador asChild>
                <Boton variante="secundario" tamano="sm" className="justify-self-start">
                    <Copy />
                    Copiar enlace
                </Boton>
            </DialogoDisparador>
            <DialogoContenido titulo={`Enlace de la encuesta · ${encuesta.numero}`} descripcion="Es personal y de un solo uso: compártelo solo con el cliente.">
                <div className="grid gap-3">
                    {estado.paso === "pidiendo" ? (
                        <Cargando texto="Preparando el enlace…" visible />
                    ) : estado.paso === "listo" ? (
                        <>
                            <p role="status" className="inline-flex items-center gap-1.5 text-sm">
                                {estado.copiado ? <Check aria-hidden className="size-4 text-success" /> : null}
                                {estado.copiado ? "Enlace copiado al portapapeles." : "No se pudo copiar: selecciónalo y cópialo a mano."}
                            </p>
                            <Entrada readOnly value={estado.enlace} aria-label="Enlace de la encuesta" className="font-mono text-xs sm:text-xs" onFocus={(ev) => ev.currentTarget.select()} />
                            <p className="text-xs text-muted-foreground">Vence el {textoFechaHora(estado.expiraEn)}.</p>
                        </>
                    ) : estado.paso === "por_correo" ? (
                        <EnviadaPorCorreo />
                    ) : estado.paso === "cambio" ? (
                        <Aviso tono="info" titulo="La encuesta cambió">
                            {estado.mensaje}
                        </Aviso>
                    ) : (
                        <Aviso
                            tono="error"
                            titulo="No se pudo obtener el enlace"
                            accion={
                                <Boton variante="secundario" tamano="sm" onClick={() => void pedir()}>
                                    <RotateCw />
                                    Reintentar
                                </Boton>
                            }
                        >
                            {mensajeDeError(estado.error).detalle}
                        </Aviso>
                    )}
                </div>
                <DialogoPie>
                    <DialogoCerrar asChild>
                        <Boton variante="secundario">Cerrar</Boton>
                    </DialogoCerrar>
                </DialogoPie>
            </DialogoContenido>
        </Dialogo>
    );
}
