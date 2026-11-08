"use client";

import { Aviso, Boton, Campo, Cargando, PanelEmergente } from "@iris/ui";
import { ArrowLeft, FileText, RotateCw, Send } from "lucide-react";
import { useState } from "react";
import { zodResolver } from "@hookform/resolvers/zod";
import { useForm, useWatch } from "react-hook-form";
import { z } from "zod";
import { api, resolver } from "@/lib/api/cliente";
import { ErrorApi, mensajeDeError } from "@/lib/api/errores";
import { EN_OTRA_EMPRESA, esEnOtraEmpresa } from "@/lib/conversaciones/atencion";
import { cabeceraIdem } from "@/lib/conversaciones/api";
import { useIdEnvio } from "@/lib/conversaciones/id-envio";
import { esSesionCambiada, verificarSesion, type Identidad } from "@/lib/conversaciones/sesion";
import type { Conversacion, Mensaje } from "@/lib/conversaciones/tipos";
import { useLectura } from "@/lib/lectura";
import type { Schema } from "@iris/contratos";

type Plantilla = Schema<"PlantillaDisponible">;

/** Vista previa con las variables puestas (`{{nombre}}` → lo escrito, o el hueco tal cual). */
export function vistaConVariables(vista: string, valores: Record<string, string>): string {
    return vista.replace(/\{\{\s*([\w.]+)\s*\}\}/g, (m, nombre: string) => valores[nombre]?.trim() || m);
}

/**
 * Plantillas aprobadas en el número de la conversación (§7.2.1): elegir una, llenar sus variables
 * y enviarla. Es lo único que se puede mandar con la ventana de 24 h cerrada.
 */
export function SelectorPlantilla({
    conv,
    identidad,
    alEnviado,
    alReenvio,
    principal = false,
}: {
    conv: Conversacion;
    identidad: Identidad;
    alEnviado: (m: Mensaje) => void;
    /** 200 sin cuerpo (reenvío): releer los mensajes. */
    alReenvio: () => void;
    alActualizar: (c: Conversacion) => void;
    principal?: boolean;
}) {
    const [abierto, setAbierto] = useState(false);
    return (
        <PanelEmergente
            titulo="Plantillas aprobadas"
            tituloVisible
            abierto={abierto}
            alCambiar={setAbierto}
            lado="top start"
            className="w-[min(22rem,calc(100vw-2rem))]"
            disparador={
                principal ? (
                    <Boton variante="tinta" tamano="sm" className="justify-self-start">
                        <FileText />
                        Elegir plantilla
                    </Boton>
                ) : (
                    <Boton variante="fantasma" tamano="icono-sm" className="text-muted-foreground" aria-label="Plantillas" title="Plantillas aprobadas">
                        <FileText />
                    </Boton>
                )
            }
        >
            {abierto ? <ContenidoPlantillas conv={conv} identidad={identidad} alEnviado={(m) => {
                        if (m) alEnviado(m);
                        else alReenvio();
                        setAbierto(false);
                    }} /> : null}
        </PanelEmergente>
    );
}

function ContenidoPlantillas({ conv, identidad, alEnviado }: { conv: Conversacion; identidad: Identidad; alEnviado: (m: Mensaje | undefined) => void }) {
    const lectura = useLectura(async (signal) => (await resolver(api().GET("/api/v1/mensajeria/conversaciones/{id}/plantillas", { params: { path: { id: conv.id } }, signal }))).data ?? []);
    const [elegida, setElegida] = useState<Plantilla | null>(null);
    if (lectura.estado === "cargando") return <Cargando texto="Cargando plantillas…" />;
    if (lectura.estado === "error") {
        return (
            <Aviso
                tono="error"
                titulo="No se pudieron leer las plantillas"
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
    if (elegida) return <FormPlantilla conv={conv} identidad={identidad} plantilla={elegida} volver={() => setElegida(null)} alEnviado={alEnviado} />;
    if (lectura.datos.length === 0) return <p className="text-sm text-muted-foreground">Este número no tiene plantillas aprobadas por WhatsApp.</p>;
    return (
        <ul className="grid max-h-72 gap-0.5 overflow-y-auto">
            {lectura.datos.map((p) => (
                <li key={p.codigo}>
                    <Boton variante="fantasma" className="h-auto w-full flex-col items-start gap-0 px-2 py-1.5 text-left" onClick={() => setElegida(p)}>
                        <span className="w-full truncate text-sm font-semibold">{p.nombre}</span>
                        <span className="line-clamp-2 w-full text-xs font-normal text-muted-foreground">{p.vistaPrevia}</span>
                    </Boton>
                </li>
            ))}
        </ul>
    );
}

function FormPlantilla({ conv, identidad, plantilla, volver, alEnviado }: { conv: Conversacion; identidad: Identidad; plantilla: Plantilla; volver: () => void; alEnviado: (m: Mensaje | undefined) => void }) {
    const variables = plantilla.variables ?? [];
    const [esquema] = useState(() =>
        z.object(Object.fromEntries(variables.map((v) => [v.nombre, z.string().trim().min(1, "Obligatoria").max(200)]))),
    );
    const form = useForm<Record<string, string>>({ resolver: zodResolver(esquema), defaultValues: Object.fromEntries(variables.map((v) => [v.nombre, ""])) });
    const envio = useIdEnvio();
    const [error, setError] = useState<unknown>(null);
    const valores = useWatch({ control: form.control });

    async function enviar(datos: Record<string, string>) {
        setError(null);
        const id = envio.actual();
        try {
            await verificarSesion(identidad);
            const m = await resolver(
                api().POST("/api/v1/mensajeria/conversaciones/{id}/mensajes", {
                    params: { path: { id: conv.id }, header: cabeceraIdem(id) },
                    body: { id, plantilla: { codigo: plantilla.codigo, variables: Object.fromEntries(Object.entries(datos).map(([k, v]) => [k, v.trim()])) } },
                }),
            );
            envio.renovar();
            alEnviado(m);
        } catch (e) {
            envio.trasError(e);
            setError(e);
        }
    }

    const textoError = (e: unknown) => {
        if (esSesionCambiada(e)) return "La sesión de este navegador cambió: no se envió.";
        if (esEnOtraEmpresa(e)) return EN_OTRA_EMPRESA.texto;
        if (!(e instanceof ErrorApi)) return mensajeDeError(e).titulo;
        switch (e.codigo) {
            case "baja":
                return "Este número se dio de baja de WhatsApp: no se le pueden mandar plantillas.";
            case "sin_permiso":
                return "No hay permiso de contacto vigente por WhatsApp para este número.";
            case "cliente_sin_confirmar":
                return "Falta que un supervisor confirme el cliente de esta conversación.";
            case "plantilla_no_aprobada":
                return "Esta plantilla ya no está aprobada en este número.";
            default:
                return mensajeDeError(e).titulo;
        }
    };

    return (
        <form noValidate className="grid gap-3" aria-label={`Plantilla ${plantilla.nombre}`} onSubmit={form.handleSubmit(enviar)}>
            <Boton variante="enlace" className="justify-self-start text-xs" onClick={volver}>
                <ArrowLeft />
                Otras plantillas
            </Boton>
            <p className="rounded-lg bg-background-secondary p-2.5 text-sm whitespace-pre-wrap [overflow-wrap:anywhere]">{vistaConVariables(plantilla.vistaPrevia, valores as Record<string, string>)}</p>
            {variables.map((v) => (
                <Campo
                    key={v.nombre}
                    etiqueta={v.nombre}
                    ayuda={v.formato || undefined}
                    maxLength={200}
                    error={form.formState.errors[v.nombre]?.message}
                    {...form.register(v.nombre)}
                />
            ))}
            {error ? (
                <Aviso tono="error" titulo="No se envió">
                    {textoError(error)}
                </Aviso>
            ) : null}
            <Boton type="submit" variante="tinta" tamano="sm" disabled={form.formState.isSubmitting}>
                {form.formState.isSubmitting ? "Enviando…" : "Enviar plantilla"}
                <Send />
            </Boton>
        </form>
    );
}
