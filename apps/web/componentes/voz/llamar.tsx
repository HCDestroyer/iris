"use client";

import { avisar, Aviso, Boton, cn, Esqueleto, PanelEmergente } from "@iris/ui";
import { CircleDashed, Clock, History, Phone, PhoneCall, PhoneOff, RotateCw, ShieldCheck, ShieldX, type LucideIcon } from "lucide-react";
import { useEffect, useState } from "react";
import { ErrorApi, mensajeDeError } from "@/lib/api/errores";
import { useClaveIdempotencia } from "@/lib/idempotencia";
import { useLectura } from "@/lib/lectura";
import { useSesion } from "@/lib/sesion";
import { leerPermisoLlamada, solicitarPermisoLlamada } from "@/lib/voz/api";
import { escucharEventos } from "@/lib/voz/canal";
import { desdeCuando, POR_QUE_PERMISO, SIN_CONTESTAR_RIESGO, textoErrorSolicitud, textoMotivoPermiso, textoPermiso, type TonoLlamada } from "@/lib/voz/presentacion";
import { PERMISO_LLAMAR, PERMISO_SOLICITAR_PERMISO, type PermisoLlamada } from "@/lib/voz/tipos";
import { EtiquetaEnPruebas } from "./en-pruebas";
import { useVoz } from "./proveedor";

/*
 * Llamar a un cliente por WhatsApp (contratos/voz.md, E1b). WhatsApp solo deja llamar a quien dio
 * permiso: aquí se ve cómo está ese permiso, se pide y, cuando lo hay, se llama. La llamada en sí
 * (micrófono, oferta, «Llamando…», colgar) la lleva el proveedor de voz.
 */

const ICONO: Record<PermisoLlamada["estado"], LucideIcon> = {
    sin_pedir: CircleDashed,
    pedido: Clock,
    temporal: ShieldCheck,
    permanente: ShieldCheck,
    negado: PhoneOff,
    revocado: ShieldX,
    vencido: History,
};

const COLOR: Record<TonoLlamada, string> = {
    ok: "text-success",
    info: "text-info",
    aviso: "text-warning",
    error: "text-error",
    neutro: "text-muted-foreground",
};

type Destino = { id: number; titulo: string };

/** El acceso en la cabecera de la conversación: solo para quien puede llamar o pedir el permiso. */
export function BotonLlamada({ conv }: { conv: Destino }) {
    const { puede } = useSesion();
    const [abierto, setAbierto] = useState(false);
    if (!puede([PERMISO_LLAMAR]) && !puede([PERMISO_SOLICITAR_PERMISO])) return null;
    return (
        <PanelEmergente
            titulo="Llamar por WhatsApp"
            tituloVisible
            lado="bottom end"
            abierto={abierto}
            alCambiar={setAbierto}
            className="w-80 max-w-[calc(100vw-1rem)]"
            disparador={
                <Boton variante="contorno" tamano="icono-sm" className="shrink-0" aria-label="Llamada" title="Llamar por WhatsApp" data-slot="boton-llamada">
                    <Phone />
                </Boton>
            }
        >
            {/* Se lee al abrir: el permiso cambia cuando el cliente responde. */}
            {abierto ? <PanelPermiso key={conv.id} conv={conv} cerrar={() => setAbierto(false)} /> : null}
        </PanelEmergente>
    );
}

function PanelPermiso({ conv, cerrar }: { conv: Destino; cerrar: () => void }) {
    const { puede } = useSesion();
    const voz = useVoz();
    const clave = useClaveIdempotencia();
    const lectura = useLectura((senal) => leerPermisoLlamada(conv.id, senal));
    const { refrescar, reemplazar } = lectura;
    const [pidiendo, setPidiendo] = useState(false);
    const [error, setError] = useState<unknown>(null);

    // El cliente respondió a la solicitud, o una llamada a este contacto cambió: el permiso pudo cambiar.
    useEffect(
        () =>
            escucharEventos((evento) => {
                if (evento.conversacionId === conv.id) void refrescar();
            }),
        [conv.id, refrescar],
    );

    if (lectura.estado === "cargando") {
        return (
            <div className="grid gap-2" aria-busy="true">
                <Esqueleto className="h-5 w-2/3" />
                <Esqueleto className="h-9 w-full" />
            </div>
        );
    }
    if (lectura.estado === "error") {
        // Sin leer el permiso no se ofrece llamar ni pedirlo.
        return (
            <Aviso
                tono="error"
                titulo="No se pudo leer el permiso de llamada"
                accion={
                    <Boton variante="secundario" tamano="sm" onClick={lectura.reintentar}>
                        <RotateCw />
                        Reintentar
                    </Boton>
                }
            >
                {mensajeDeError(lectura.error).titulo}
            </Aviso>
        );
    }

    const permiso = lectura.datos;
    const t = textoPermiso(permiso);
    const Icono = ICONO[permiso.estado];
    const motivo = textoMotivoPermiso(permiso.motivo);
    const puedeLlamar = puede([PERMISO_LLAMAR]);
    const puedePedir = puede([PERMISO_SOLICITAR_PERMISO]);
    const ocupada = voz ? voz.estado.fase !== "libre" && voz.estado.fase !== "aviso" : false;
    // Se perdió la respuesta de la llamada anterior a este contacto y se está viendo si quedó viva.
    const comprobando = voz?.comprobando(conv.id) ?? false;
    const proxima = desdeCuando(permiso.proximaSolicitudEn);
    const enRiesgo = permiso.puedeLlamar && permiso.sinContestar >= SIN_CONTESTAR_RIESGO;

    async function pedir() {
        setPidiendo(true);
        setError(null);
        try {
            reemplazar(await solicitarPermisoLlamada(conv.id, clave.actual()));
            // Hecho: la próxima solicitud es otra operación, con otra clave.
            clave.renovar();
            avisar({ titulo: "Se le pidió permiso para llamarle", descripcion: "El mensaje quedó en la conversación. Cuando lo acepte se le podrá llamar." });
        } catch (e) {
            clave.trasError(e);
            setError(e);
            // Lo que sabía la pantalla ya no vale (p. ej. ya había permiso): se relee.
            void refrescar();
        } finally {
            setPidiendo(false);
        }
    }

    const errorPedir = error instanceof ErrorApi ? (textoErrorSolicitud(error.codigo, error.errores[0]?.message) ?? (error.detalle || error.titulo)) : error ? mensajeDeError(error).titulo : null;

    return (
        <div className="grid gap-3" data-slot="permiso-llamada" data-estado={permiso.estado}>
            <EtiquetaEnPruebas />
            <div className="flex min-w-0 items-start gap-2">
                <Icono aria-hidden className={cn("mt-0.5 size-4 shrink-0", COLOR[t.tono])} />
                <div className="grid min-w-0 gap-0.5">
                    <p className={cn("text-sm font-semibold", COLOR[t.tono])}>{t.titulo}</p>
                    <p className="text-[0.8125rem] text-muted-foreground">{t.detalle}</p>
                </div>
            </div>
            {motivo ? <p className="text-[0.8125rem] font-medium text-warning">{motivo}</p> : null}
            {permiso.puedeLlamar && permiso.sinContestar > 0 ? (
                <p className={cn("text-[0.8125rem]", enRiesgo ? "font-medium text-warning" : "text-muted-foreground")} data-slot="sin-contestar">
                    {permiso.sinContestar === 1 ? "Va 1 llamada sin contestar." : `Van ${permiso.sinContestar} llamadas seguidas sin contestar.`} A la cuarta, WhatsApp quita el permiso.
                </p>
            ) : null}
            {puedeLlamar && permiso.puedeLlamar ? (
                voz ? (
                    <Boton
                        disabled={ocupada || comprobando || !voz.admite}
                        onClick={() => {
                            voz.llamar(conv, { sinContestar: permiso.sinContestar });
                            cerrar();
                        }}
                    >
                        <PhoneCall />
                        {comprobando ? "Comprobando la llamada anterior…" : "Llamar"}
                    </Boton>
                ) : null
            ) : null}
            {puedeLlamar && permiso.puedeLlamar && voz && ocupada ? <p className="text-xs text-muted-foreground">Ya tienes una llamada: termínala para hacer otra.</p> : null}
            {puedeLlamar && permiso.puedeLlamar && voz && !voz.admite ? <p className="text-xs text-warning">Este navegador no puede hacer llamadas (hace falta micrófono y una conexión segura).</p> : null}
            {permiso.solicitudSinConfirmar ? (
                // El envío agotó sus intentos sin respuesta de WhatsApp: pudo llegar, así que sigue contando.
                <p className="text-[0.8125rem] font-medium text-warning" data-slot="solicitud-sin-confirmar">
                    Se envió la solicitud, pero WhatsApp aún no confirma que la recibió. Iris lo sigue comprobando; mientras tanto no se puede pedir otra.
                </p>
            ) : null}
            {puedePedir && permiso.puedeSolicitar && !permiso.solicitudSinConfirmar ? (
                <Boton variante={permiso.puedeLlamar ? "contorno" : "primario"} disabled={pidiendo} onClick={() => void pedir()}>
                    <Phone />
                    {pidiendo ? "Pidiendo…" : "Pedir permiso para llamarle"}
                </Boton>
            ) : null}
            {puedePedir && !permiso.puedeSolicitar && !permiso.puedeLlamar && proxima ? <p className="text-[0.8125rem] text-muted-foreground">Se le puede volver a pedir {proxima}.</p> : null}
            {errorPedir ? (
                <Aviso tono="warning" titulo="No se le pidió el permiso" data-slot="error-permiso">
                    {errorPedir}
                </Aviso>
            ) : null}
            <p className="border-t border-border pt-2 text-xs text-muted-foreground">{POR_QUE_PERMISO}</p>
        </div>
    );
}
