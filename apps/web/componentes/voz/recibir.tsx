"use client";

import { Aviso, Boton, cn, Esqueleto, Interruptor, PanelEmergente } from "@iris/ui";
import { Mic, Phone, RotateCw, UserCheck } from "lucide-react";
import { useState } from "react";
import { mensajeDeError } from "@/lib/api/errores";
import { permisoMicrofono } from "@/lib/voz/medios";
import { useDisponibilidad } from "@/lib/conversaciones/disponibilidad";
import { useEstoyDisponible } from "../conversaciones/disponibilidad";
import { EtiquetaEnPruebas } from "./en-pruebas";
import { useVoz } from "./proveedor";

/** Qué pasa con «Recibir llamadas» en este navegador, para el punto del botón. */
type Resumen = "apagado" | "encendido" | "atencion";

function useResumen(): Resumen | null {
    const voz = useVoz();
    const disponible = useEstoyDisponible();
    if (!voz?.contesta || voz.recibir.estado !== "listo") return null;
    if (voz.microfono === "denegado" || voz.microfono === "sin_microfono" || voz.desplazado !== null) return "atencion";
    if (!voz.recibir.recibe) return "apagado";
    // Encendido pero ahora no sonaría: sin flujo en vivo o «No disponible».
    if ((voz.feed !== null && voz.feed !== "en_vivo" && voz.feed !== "conectando") || disponible === false) return "atencion";
    return "encendido";
}

/**
 * «Recibir llamadas» en este navegador: un interruptor por persona y navegador. Al encenderlo se
 * explica para qué es el micrófono y se pide el permiso (con calma, no cuando ya suena). Si el
 * navegador lo niega o no hay micrófono, lo dice y cómo resolverlo.
 */
export function ControlRecibir({ className }: { className?: string }) {
    const voz = useVoz();
    const disponible = useEstoyDisponible();
    const d = useDisponibilidad();
    const [explicando, setExplicando] = useState(false);
    // Quien solo llama (sin `llamadas.contestar`) no tiene «Recibir llamadas».
    if (!voz?.contesta) return null;
    const { recibir, microfono } = voz;
    if (!voz.admite) {
        return (
            <Aviso tono="warning" titulo="Este navegador no puede contestar llamadas" className={className}>
                Hace falta un navegador con micrófono y una conexión segura (https).
            </Aviso>
        );
    }
    if (recibir.estado === "cargando") {
        return (
            <div className={cn("grid gap-2", className)} aria-busy="true">
                <span className="sr-only" role="status">
                    Leyendo si este navegador recibe llamadas…
                </span>
                <Esqueleto className="h-8" />
            </div>
        );
    }
    if (recibir.estado === "error") {
        return (
            <Aviso
                tono="error"
                titulo="No se pudo leer si recibes llamadas aquí"
                className={className}
                accion={
                    <Boton variante="secundario" tamano="sm" onClick={voz.reintentarRecibir}>
                        <RotateCw />
                        Reintentar
                    </Boton>
                }
            >
                {mensajeDeError(recibir.error).titulo}
            </Aviso>
        );
    }

    async function alCambiar(encender: boolean) {
        if (!voz) return;
        if (!encender) {
            setExplicando(false);
            await voz.apagar();
            return;
        }
        // Con el permiso ya dado no hay nada que explicar.
        if ((await permisoMicrofono()) === "granted") await voz.encender();
        else setExplicando(true);
    }
    const bloqueado = microfono === "denegado";
    const sinMicrofono = microfono === "sin_microfono" || microfono === "no_disponible";
    return (
        <div className={cn("grid gap-2.5", className)} data-slot="control-recibir" data-recibe={recibir.recibe ? "si" : "no"}>
            <EtiquetaEnPruebas />
            <Interruptor
                alFinal
                etiqueta="Recibir llamadas en este navegador"
                checked={recibir.recibe}
                disabled={voz.cambiando}
                onChange={(e) => void alCambiar(e.target.checked)}
            />
            {explicando && !recibir.recibe && !bloqueado && !sinMicrofono ? (
                <Aviso tono="info" titulo="Iris necesita tu micrófono" icono={Mic}>
                    <span className="grid gap-2.5">
                        <span>Es para que puedas hablar con el cliente. El navegador te lo va a preguntar ahora; solo se usa mientras estás en una llamada.</span>
                        <span className="flex flex-wrap gap-2">
                            <Boton
                                tamano="sm"
                                disabled={voz.cambiando}
                                onClick={async () => {
                                    if (await voz.encender()) setExplicando(false);
                                }}
                            >
                                {voz.cambiando ? "Esperando al navegador…" : "Permitir micrófono"}
                            </Boton>
                            <Boton variante="contorno" tamano="sm" disabled={voz.cambiando} onClick={() => setExplicando(false)}>
                                Ahora no
                            </Boton>
                        </span>
                    </span>
                </Aviso>
            ) : null}
            {voz.desplazado !== null && !recibir.recibe ? (
                <Aviso
                    data-slot="recibir-apagado"
                    data-motivo={voz.desplazado}
                    tono="warning"
                    titulo="Este navegador dejó de recibir llamadas"
                    accion={
                        <Boton variante="secundario" tamano="sm" disabled={voz.cambiando} onClick={() => void voz.encender()}>
                            Volver a encender
                        </Boton>
                    }
                >
                    {voz.desplazado === "inactivo"
                        ? "Llevabas más de 30 días sin abrir Iris en este navegador y se apagó solo. Si contestas desde aquí, vuelve a encenderlo."
                        : voz.desplazado === "tope"
                          ? "Lo tienes encendido en más de 10 navegadores y solo se guardan los 10 más recientes. Si contestas desde aquí, vuelve a encenderlo."
                          : "El servidor lo apagó en este navegador. Si contestas desde aquí, vuelve a encenderlo."}
                </Aviso>
            ) : null}
            {bloqueado ? (
                <Aviso
                    tono="warning"
                    titulo="El navegador bloqueó el micrófono"
                    accion={
                        <Boton variante="secundario" tamano="sm" disabled={voz.cambiando} onClick={() => void voz.encender()}>
                            <RotateCw />
                            Volver a intentar
                        </Boton>
                    }
                >
                    Toca el candado de la barra de direcciones, permite el micrófono para Iris y vuelve a intentar. Mientras tanto no puedes contestar desde aquí.
                </Aviso>
            ) : null}
            {sinMicrofono ? (
                <Aviso
                    tono="warning"
                    titulo="No se encontró un micrófono"
                    accion={
                        <Boton variante="secundario" tamano="sm" disabled={voz.cambiando} onClick={() => void voz.encender()}>
                            <RotateCw />
                            Volver a intentar
                        </Boton>
                    }
                >
                    Conecta un micrófono o unos audífonos con micrófono y vuelve a intentar.
                </Aviso>
            ) : null}
            {voz.errorCambio ? (
                <Aviso tono="error" titulo="No se pudo cambiar">
                    {mensajeDeError(voz.errorCambio).titulo}
                </Aviso>
            ) : null}
            {recibir.recibe ? (
                disponible === false ? (
                    <div className="grid gap-1.5" data-slot="recibir-no-disponible">
                        <p className="text-xs font-medium text-warning">Estás «No disponible»: ahora no te suenan las llamadas.</p>
                        {d ? (
                            <Boton tamano="sm" className="justify-self-start" disabled={d.cambiando} onClick={() => void d.cambiar("disponible")}>
                                <UserCheck />
                                Ponerme disponible
                            </Boton>
                        ) : null}
                    </div>
                ) : voz.feed === "sin_conexion" || voz.feed === "sondeo" || voz.feed === "detenido" ? (
                    <p className="text-xs font-medium text-warning">Sin conexión en vivo: ahora no te suenan las llamadas. Se reconecta sola.</p>
                ) : (
                    <p className="text-xs text-muted-foreground">Te suenan las llamadas de WhatsApp de tus colas mientras tengas Iris abierto y estés «Disponible».</p>
                )
            ) : !explicando && !bloqueado && !sinMicrofono && voz.desplazado === null ? (
                <p className="text-xs text-muted-foreground">Apagado: las llamadas no suenan en este navegador.</p>
            ) : null}
        </div>
    );
}

const NOMBRE: Record<Resumen, string> = {
    apagado: "Recibir llamadas: apagado",
    encendido: "Recibir llamadas: encendido",
    atencion: "Recibir llamadas: necesita tu atención",
};

/** El acceso discreto en la barra superior: un teléfono con un punto que solo crece si hay que actuar. */
export function BotonRecibir() {
    const voz = useVoz();
    const resumen = useResumen();
    const [abierto, setAbierto] = useState(false);
    // Quien solo llama (sin `llamadas.contestar`) no tiene «Recibir llamadas».
    if (!voz?.contesta) return null;
    const nombre = resumen ? NOMBRE[resumen] : "Recibir llamadas";
    return (
        <PanelEmergente
            titulo="Llamadas"
            tituloVisible
            lado="bottom end"
            abierto={abierto}
            alCambiar={setAbierto}
            className="w-80 max-w-[calc(100vw-2rem)]"
            disparador={
                <Boton variante="secundario" tamano="icono-sm" className="relative hidden size-11 min-w-11 rounded-full border border-border bg-[var(--iris-tarjeta)] p-0 text-muted-foreground shadow-[var(--iris-sombra-flotante)] md:size-11 [&_svg]:size-[1.125rem] sm:inline-flex" aria-label={nombre} title="Llamadas" data-recibir={resumen ?? "cargando"}>
                    <Phone />
                    {resumen === "encendido" ? <span aria-hidden className="absolute top-2 right-2 size-[0.4375rem] rounded-full bg-success" /> : null}
                    {resumen === "atencion" ? <span aria-hidden className="absolute top-1.5 right-1.5 size-2.5 rounded-full bg-warning ring-2 ring-card" /> : null}
                </Boton>
            }
        >
            <ControlRecibir />
        </PanelEmergente>
    );
}
