"use client";

import { Boton, Cargando, cn, Tarjeta, TarjetaContenido, TarjetaDescripcion, TarjetaEncabezado, TarjetaTitulo } from "@iris/ui";
import { CircleAlert, CircleCheck, ExternalLink, Link2 } from "lucide-react";
import { useState } from "react";
import { BotonVerEn } from "@/componentes/enlaces-externos";
import { PERMISO_ABRIR_ARGOS } from "@/lib/asistente/permisos";
import type { EnlaceVer } from "@/lib/enlaces";
import { abrirEnArgos, explicarErrorAbrir, type DestinoArgos } from "@/lib/integraciones/abrir-argos";
import { useSesion } from "@/lib/sesion";

type Estado = { tipo: "nada" } | { tipo: "abriendo" } | { tipo: "abierto" } | { tipo: "error"; texto: string; reintentar: boolean };

/** Pide el enlace de un solo uso y lo abre en otra pestaña; lleva el estado para decirlo en pantalla. */
export function useAbrirArgos(destino?: DestinoArgos) {
    const [estado, setEstado] = useState<Estado>({ tipo: "nada" });
    async function abrir(): Promise<boolean> {
        setEstado({ tipo: "abriendo" });
        const r = await abrirEnArgos(destino);
        setEstado(r.abierto ? { tipo: "abierto" } : { tipo: "error", ...explicarErrorAbrir(r.error) });
        return r.abierto;
    }
    return { estado, abrir };
}

/**
 * «Abrir en Argos» con autologin (`integraciones.abrir_argos`): llega al cliente o al servicio ya
 * con la sesión de Argos. Sin el permiso, o si el autologin falla, queda el enlace simple de
 * siempre («Ver en Argos», a la base de Argos) como respaldo.
 */
export function BotonAbrirArgos({ destino, respaldo }: { destino: DestinoArgos; respaldo: EnlaceVer }) {
    const { puede } = useSesion();
    const { estado, abrir } = useAbrirArgos(destino);
    if (!puede([PERMISO_ABRIR_ARGOS])) return <BotonVerEn enlace={respaldo} />;
    return (
        <div className="flex min-w-0 flex-wrap items-center justify-end gap-2" data-slot="abrir-argos">
            {estado.tipo === "error" ? (
                <>
                    <p role="alert" className="flex min-w-0 basis-full items-start justify-end gap-1.5 text-right text-xs text-error">
                        <CircleAlert aria-hidden className="mt-0.5 size-3.5 shrink-0" />
                        <span className="min-w-0 [overflow-wrap:anywhere]">{estado.texto}</span>
                    </p>
                    <BotonVerEn enlace={respaldo} />
                </>
            ) : null}
            {estado.tipo === "error" && !estado.reintentar ? null : (
                <Boton variante="secundario" tamano="sm" onClick={() => void abrir()} disabled={estado.tipo === "abriendo"} title="Abre Argos en otra pestaña, ya con tu sesión" data-ver-en="argosisp">
                    {estado.tipo === "abriendo" ? <Cargando texto="Abriendo…" /> : <ExternalLink />}
                    {estado.tipo === "abriendo" ? null : estado.tipo === "error" ? "Reintentar" : "Abrir en Argos"}
                    <span className="sr-only"> (se abre en otra pestaña)</span>
                </Boton>
            )}
        </div>
    );
}

/**
 * «Vincular mi usuario de Argos»: abre Argos sin destino. La primera vez Argos pide el correo y la
 * contraseña de Argos y crea el vínculo que autoriza aprobar y rechazar boletas. Iris no sabe si el
 * vínculo ya existe: por eso el botón está siempre.
 */
export function BotonVincularArgos({ alAbrir, className, tamano = "sm" }: { alAbrir?: () => void; className?: string; tamano?: "sm" | "md" }) {
    const { estado, abrir } = useAbrirArgos();
    return (
        <div className={cn("grid min-w-0 gap-1.5", className)} data-slot="vincular-argos">
            <div>
                <Boton
                    variante="secundario"
                    tamano={tamano}
                    disabled={estado.tipo === "abriendo"}
                    onClick={async () => {
                        if (await abrir()) alAbrir?.();
                    }}
                >
                    {estado.tipo === "abriendo" ? <Cargando texto="Abriendo Argos…" /> : <Link2 />}
                    {estado.tipo === "abriendo" ? null : "Vincular mi usuario de Argos"}
                    <span className="sr-only"> (se abre en otra pestaña)</span>
                </Boton>
            </div>
            {estado.tipo === "abierto" ? (
                <p role="status" className="flex items-start gap-1.5 text-xs text-muted-foreground">
                    <CircleCheck aria-hidden className="mt-0.5 size-3.5 shrink-0 text-success" />
                    Argos se abrió en otra pestaña. Entra allá con tu correo y tu contraseña de Argos y vuelve aquí.
                </p>
            ) : estado.tipo === "error" ? (
                <p role="alert" className="flex items-start gap-1.5 text-xs text-error">
                    <CircleAlert aria-hidden className="mt-0.5 size-3.5 shrink-0" />
                    <span className="min-w-0 [overflow-wrap:anywhere]">{estado.texto}</span>
                </p>
            ) : null}
        </div>
    );
}

/** Tarjeta de «Mi perfil» (`/cuenta#argos`), solo con `integraciones.abrir_argos`. */
export function TarjetaVincularArgos() {
    const { puede } = useSesion();
    if (!puede([PERMISO_ABRIR_ARGOS])) return null;
    return (
        <Tarjeta id="argos">
            <TarjetaEncabezado>
                <TarjetaTitulo>Mi usuario de Argos</TarjetaTitulo>
                <TarjetaDescripcion>Para aprobar o rechazar boletas desde una conversación, Argos necesita saber quién eres allá. Se hace una sola vez.</TarjetaDescripcion>
            </TarjetaEncabezado>
            <TarjetaContenido>
                <BotonVincularArgos tamano="md" />
            </TarjetaContenido>
        </Tarjeta>
    );
}
