"use client";

import { avisar, Aviso, Boton, Dialogo, DialogoCerrar, DialogoContenido, DialogoPie } from "@iris/ui";
import { Power, PowerOff } from "lucide-react";
import { useState } from "react";
import { mensajeDeError } from "@/lib/api/errores";
import { useClaveIdempotencia } from "@/lib/idempotencia";
import { avisoEstado } from "@/lib/avisos";

/**
 * «Activar» / «Desactivar» de un técnico, una cuadrilla o una zona (acciones explícitas con
 * `version` e `Idempotency-Key`). Desactivar pide confirmación y explica qué pasa.
 */
export function CambiarEstado({
    activo,
    nombre,
    puede,
    consecuencia,
    ejecutar,
    alHecho,
}: {
    activo: boolean;
    nombre: string;
    /** ¿Tiene el permiso de la acción que toca (activar o desactivar)? */
    puede: boolean;
    /** Qué pasa al desactivar (se muestra en la confirmación). */
    consecuencia: string;
    ejecutar: (accion: "activar" | "desactivar", clave: string) => Promise<unknown>;
    alHecho: () => void | Promise<void>;
}) {
    const clave = useClaveIdempotencia();
    const [confirmando, setConfirmando] = useState(false);
    const [enviando, setEnviando] = useState(false);
    const [error, setError] = useState<unknown>(null);
    if (!puede) return null;
    const accion = activo ? "desactivar" : "activar";

    async function enviar() {
        setEnviando(true);
        setError(null);
        try {
            await ejecutar(accion, clave.actual());
            clave.renovar();
            setConfirmando(false);
            avisar({ titulo: avisoEstado(nombre, accion === "activar") });
            await alHecho();
        } catch (e) {
            clave.trasError(e);
            setError(e);
        } finally {
            setEnviando(false);
        }
    }

    return (
        <>
            <Boton
                variante="contorno"
                tamano="sm"
                aria-label={`${activo ? "Desactivar" : "Activar"} ${nombre}`}
                disabled={enviando}
                onClick={() => (activo ? setConfirmando(true) : void enviar())}
            >
                {activo ? <PowerOff /> : <Power />}
                {activo ? "Desactivar" : enviando ? "Activando…" : "Activar"}
            </Boton>
            {error && !confirmando ? (
                <Aviso tono="error" titulo={`No se pudo ${accion}`} className="basis-full">
                    {[mensajeDeError(error).titulo, mensajeDeError(error).detalle].filter(Boolean).join(". ")}
                </Aviso>
            ) : null}
            {confirmando ? (
                <Dialogo open onOpenChange={(a) => !a && !enviando && setConfirmando(false)}>
                    <DialogoContenido role="alertdialog" titulo={`¿Desactivar ${nombre}?`} descripcion={consecuencia}>
                        {error ? (
                            <Aviso tono="error" titulo="No se pudo desactivar">
                                {[mensajeDeError(error).titulo, mensajeDeError(error).detalle].filter(Boolean).join(". ")}
                            </Aviso>
                        ) : null}
                        <DialogoPie>
                            <DialogoCerrar asChild>
                                <Boton variante="contorno" disabled={enviando}>
                                    Volver
                                </Boton>
                            </DialogoCerrar>
                            <Boton onClick={() => void enviar()} disabled={enviando}>
                                <PowerOff />
                                {enviando ? "Desactivando…" : "Desactivar"}
                            </Boton>
                        </DialogoPie>
                    </DialogoContenido>
                </Dialogo>
            ) : null}
        </>
    );
}
