"use client";

import { zodResolver } from "@hookform/resolvers/zod";
import { Aviso, Boton, Campo, Tarjeta } from "@iris/ui";
import { PlugZap } from "lucide-react";
import { useState } from "react";
import { useForm } from "react-hook-form";
import { api, claveTrasError, nuevaClaveIdempotencia, resolver } from "@/lib/api/cliente";
import { mensajeDeError } from "@/lib/api/errores";
import { CLAVE_PANEL_ARGOS, esquemaPanelArgos, type DatosPanelArgos } from "@/lib/integraciones/presentacion";

/** Recuerda el panel para «Reintentar» desde la vuelta (solo comodidad: puede fallar). */
function recordarPanel(panel: string) {
    try {
        window.sessionStorage.setItem(CLAVE_PANEL_ARGOS, panel);
    } catch {
        // Sin almacenamiento (ventana privada): «Reintentar» vuelve a esta pantalla.
    }
}

/** Pide la URL de autorizar (contrato 0.4.0 §1.4) y abre el panel de Argos en la misma pestaña. */
export async function iniciarConexionArgos(panelUrl: string, clave: string): Promise<void> {
    const r = await resolver(api().POST("/api/v1/integraciones/argosisp/autorizaciones", { body: { panelUrl }, params: { header: { "Idempotency-Key": clave } } }));
    // Solo se navega a la URL del panel que normalizó la API (nunca a otra cosa).
    const destino = new URL(r.url);
    if (destino.origin !== r.panelUrl) {
        throw new Error("La URL de autorizar no es del panel de Argos");
    }
    recordarPanel(r.panelUrl);
    window.location.assign(destino.href);
}

/**
 * «Conectar con Argos» (contrato 0.4.0 §1.4): el administrador escribe la dirección del panel de
 * Argos y aprueba allá; Argos vuelve a `/administracion/integraciones/argos/vuelta`. No se puede
 * pulsar sin una dirección válida.
 */
export function ConectarArgos({ panelSugerido }: { panelSugerido: string | null }) {
    const [clave, setClave] = useState(nuevaClaveIdempotencia);
    const [error, setError] = useState<unknown>(null);
    const form = useForm<DatosPanelArgos>({ resolver: zodResolver(esquemaPanelArgos), mode: "onChange", defaultValues: { panelUrl: panelSugerido ?? "" } });
    const { isSubmitting, isValid, errors } = form.formState;

    async function enviar(d: DatosPanelArgos) {
        setError(null);
        try {
            await iniciarConexionArgos(d.panelUrl, clave);
        } catch (e) {
            setClave(claveTrasError(e, clave));
            setError(e);
        }
    }

    return (
        <Tarjeta className="mb-4 grid gap-3 p-4" data-testid="conectar-argos">
            <div className="grid gap-0.5">
                <h2 className="font-medium">Conectar con Argos</h2>
                <p className="text-sm text-muted-foreground">Iris te lleva a Argos para que apruebes la conexión y vuelve aquí. El conector queda apagado hasta que lo actives.</p>
            </div>
            <form className="grid gap-3 sm:grid-cols-[minmax(0,1fr)_auto] sm:items-start" noValidate onSubmit={(e) => void form.handleSubmit(enviar)(e)}>
                <Campo
                    etiqueta="Dirección del panel de Argos"
                    type="url"
                    inputMode="url"
                    autoComplete="url"
                    placeholder="https://argos.tu-isp.com"
                    maxLength={500}
                    error={errors.panelUrl?.message}
                    {...form.register("panelUrl")}
                />
                <Boton type="submit" className="sm:mt-6" disabled={!isValid || isSubmitting}>
                    <PlugZap />
                    {isSubmitting ? "Abriendo Argos…" : "Conectar con Argos"}
                </Boton>
            </form>
            {error ? (
                <Aviso tono="error" titulo="No se pudo iniciar la conexión">
                    {mensajeDeError(error).detalle ?? mensajeDeError(error).titulo}
                </Aviso>
            ) : null}
        </Tarjeta>
    );
}
