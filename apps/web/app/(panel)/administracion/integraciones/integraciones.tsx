"use client";

import { zodResolver } from "@hookform/resolvers/zod";
import {
    Aviso,
    Boton,
    Campo,
    CampoSelector,
    DialogoCerrar, DialogoContenido,
    DialogoPie,
    DialogoProtegido,
    Esqueleto,
    EstadoVacio,
    InsigniaEstado,
    Tarjeta,
} from "@iris/ui";
import { ArrowLeft, CircleCheck, CirclePower, Plug, PlugZap, Power, ShieldAlert, ShieldCheck } from "lucide-react";
import Link from "next/link";
import { useState } from "react";
import { useForm } from "react-hook-form";
import { z } from "zod";
import { EncabezadoPagina } from "@/componentes/encabezado-pagina";
import { ErrorLectura } from "@/componentes/error-lectura";
import { api, claveTrasError, nuevaClaveIdempotencia, resolver } from "@/lib/api/cliente";
import { ErrorApi, mensajeDeError } from "@/lib/api/errores";
import { activoQueReemplaza, enlaceValido, SISTEMAS, textoMotivoOrigen, type Conector, type Previsualizacion, type Sistema } from "@/lib/integraciones/presentacion";
import { useLectura } from "@/lib/lectura";
import { textoFechaHora } from "@/lib/reportes/presentacion";
import { useSesion } from "@/lib/sesion";

/**
 * `/administracion/integraciones`: conectores con Argos y Horus. Conectar pega el enlace del otro
 * sistema y, antes de canjearlo, muestra adónde irá el token de Iris (`conexion/previsualizar`);
 * activar avisa si reemplaza al conector activo del mismo sistema.
 */
export function Integraciones() {
    const { yo, puede } = useSesion();
    // Como la API: conectar exige además `empresas.todas` sin alcance (rol global).
    const puedeConectar = puede(["integraciones.conectar"]) && yo.todasLasEmpresas;
    const lectura = useLectura(async (signal) => (await resolver(api().GET("/api/v1/integraciones/conectores", { params: { query: { limit: 50 } }, signal }))).data ?? []);
    const [aviso, setAviso] = useState<string | null>(null);

    return (
        <>
            <Boton variante="fantasma" tamano="sm" asChild className="mb-3 -ml-2">
                <Link href="/administracion">
                    <ArrowLeft />
                    Administración
                </Link>
            </Boton>
            <EncabezadoPagina
                titulo="Integraciones"
                descripcion="Conectores con Argos y Horus Flow. A lo más uno activo por sistema."
                acciones={
                    puedeConectar ? (
                        <DialogoConectar
                            alConectar={(c) => {
                                setAviso(`Conectado con ${SISTEMAS[c.sistema]} (${c.baseUrl}). Queda apagado hasta que lo actives.`);
                                void lectura.refrescar();
                            }}
                        />
                    ) : null
                }
            />
            {aviso ? (
                <Aviso tono="success" titulo="Conector creado" className="mb-4">
                    {aviso}
                </Aviso>
            ) : null}
            {lectura.estado === "cargando" ? (
                <Esqueleto className="h-32" />
            ) : lectura.estado === "error" ? (
                <ErrorLectura error={lectura.error} reintentar={lectura.reintentar} que="los conectores" />
            ) : lectura.datos.length === 0 ? (
                <Tarjeta>
                    <EstadoVacio icono={Plug} titulo="Sin conectores" descripcion="Iris funciona solo con sus datos. Conecta Argos o Horus Flow con el enlace que generan esos sistemas." />
                </Tarjeta>
            ) : (
                <ul className="grid gap-3" aria-label="Conectores">
                    {lectura.datos.map((c) => (
                        <FilaConector key={c.id} conector={c} todos={lectura.datos} alCambiar={() => void lectura.refrescar()} />
                    ))}
                </ul>
            )}
        </>
    );
}

function FilaConector({ conector: c, todos, alCambiar }: { conector: Conector; todos: readonly Conector[]; alCambiar: () => void }) {
    const { puede } = useSesion();
    const [dialogo, setDialogo] = useState<"activar" | "desactivar" | null>(null);
    return (
        <li className="surface-card grid gap-3 p-4" data-conector={c.id}>
            <div className="flex flex-wrap items-center justify-between gap-2">
                <div className="grid min-w-0 gap-0.5">
                    <span className="font-medium">{SISTEMAS[c.sistema]}</span>
                    <span className="text-sm break-all text-muted-foreground">{c.baseUrl}</span>
                </div>
                {c.estado === "activo" ? (
                    <InsigniaEstado tono="success" icono={CircleCheck} texto="Activo" />
                ) : (
                    <InsigniaEstado tono="neutral" icono={CirclePower} texto="Apagado" />
                )}
            </div>
            <p className="text-xs text-muted-foreground tabular-nums">
                {c.origen === "enlace_local" ? "Conectado desde Iris" : "Conectado desde el otro sistema"} el {textoFechaHora(c.creadoEn)}
                {c.activadoEn ? ` · activo desde ${textoFechaHora(c.activadoEn)}` : ""}
                {c.ultimoWebhookEn ? ` · último aviso ${textoFechaHora(c.ultimoWebhookEn)}` : ""}
            </p>
            <div className="flex flex-wrap gap-2 empty:hidden">
                {c.estado === "apagado" && puede(["integraciones.activar"]) ? (
                    <Boton tamano="sm" onClick={() => setDialogo("activar")}>
                        <Power />
                        Activar
                    </Boton>
                ) : null}
                {c.estado === "activo" && puede(["integraciones.desactivar"]) ? (
                    <Boton variante="secundario" tamano="sm" onClick={() => setDialogo("desactivar")}>
                        <CirclePower />
                        Desactivar
                    </Boton>
                ) : null}
            </div>
            {dialogo ? (
                <DialogoCambio
                    conector={c}
                    accion={dialogo}
                    reemplaza={dialogo === "activar" ? activoQueReemplaza(c, todos) : undefined}
                    alCerrar={() => setDialogo(null)}
                    alHecho={() => {
                        setDialogo(null);
                        alCambiar();
                    }}
                />
            ) : null}
        </li>
    );
}

const esquemaCambio = z.object({ motivo: z.string().trim().max(500, "Como mucho 500 caracteres") });

function DialogoCambio({
    conector: c,
    accion,
    reemplaza,
    alCerrar,
    alHecho,
}: {
    conector: Conector;
    accion: "activar" | "desactivar";
    reemplaza: Conector | undefined;
    alCerrar: () => void;
    alHecho: () => void;
}) {
    const [clave, setClave] = useState(nuevaClaveIdempotencia);
    const [error, setError] = useState<unknown>(null);
    const form = useForm<z.infer<typeof esquemaCambio>>({ resolver: zodResolver(esquemaCambio), defaultValues: { motivo: "" } });
    const { isSubmitting, errors } = form.formState;
    const sistema = SISTEMAS[c.sistema];

    async function enviar(d: z.infer<typeof esquemaCambio>) {
        setError(null);
        const opciones = {
            params: { path: { id: c.id }, header: { "Idempotency-Key": clave } },
            body: d.motivo ? { version: c.version, motivo: d.motivo } : { version: c.version },
        };
        try {
            if (accion === "activar") await resolver(api().POST("/api/v1/integraciones/conectores/{id}/activar", opciones));
            else await resolver(api().POST("/api/v1/integraciones/conectores/{id}/desactivar", opciones));
            alHecho();
        } catch (e) {
            setClave(claveTrasError(e, clave));
            setError(e);
        }
    }

    const conflicto = error instanceof ErrorApi && error.estado === 409;
    return (
        <DialogoProtegido sucio={form.formState.isDirty} open onOpenChange={(a) => !a && alCerrar()}>
            <DialogoContenido titulo={accion === "activar" ? `Activar el conector de ${sistema}` : `Desactivar el conector de ${sistema}`} descripcion={c.baseUrl}>
                <form className="grid gap-4" noValidate onSubmit={(e) => void form.handleSubmit(enviar)(e)}>
                    {accion === "activar" && reemplaza ? (
                        <Aviso tono="warning" titulo={`Reemplazará al conector activo de ${sistema}`} data-testid="aviso-reemplazo">
                            El conector activo ({reemplaza.baseUrl}) quedará apagado y su token se revocará en el mismo paso.
                        </Aviso>
                    ) : null}
                    {accion === "desactivar" ? (
                        <Aviso tono="warning" titulo={`Iris deja de hablar con ${sistema}`}>
                            Se dejan de recibir sus avisos y las acciones pendientes esperan hasta que haya un conector activo.
                        </Aviso>
                    ) : null}
                    <Campo etiqueta="Motivo (opcional)" maxLength={500} error={errors.motivo?.message} {...form.register("motivo")} />
                    {error ? (
                        <Aviso tono="error" titulo={conflicto ? "El conector cambió" : "No se pudo completar"}>
                            {conflicto ? "Otra persona lo cambió. Cierra y vuelve a intentarlo con los datos nuevos." : (mensajeDeError(error).detalle ?? mensajeDeError(error).titulo)}
                        </Aviso>
                    ) : null}
                    <DialogoPie>
                        {conflicto ? (
                            <Boton type="button" variante="secundario" onClick={alHecho}>
                                Cerrar y recargar
                            </Boton>
                        ) : (
                            <DialogoCerrar asChild>
                                <Boton type="button" variante="secundario">
                                    Cancelar
                                </Boton>
                            </DialogoCerrar>
                        )}
                        {conflicto ? null : (
                            <Boton type="submit" disabled={isSubmitting}>
                                {accion === "activar" ? <Power /> : <CirclePower />}
                                {isSubmitting ? "Guardando…" : accion === "activar" ? (reemplaza ? "Activar y reemplazar" : "Activar") : "Desactivar"}
                            </Boton>
                        )}
                    </DialogoPie>
                </form>
            </DialogoContenido>
        </DialogoProtegido>
    );
}

const esquemaEnlace = z.object({
    sistema: z.enum(["argosisp", "horus"]),
    enlace: z
        .string()
        .trim()
        .min(1, "Pega el enlace de conexión")
        .max(2000, "El enlace es demasiado largo")
        .refine(enlaceValido, "Debe ser el enlace completo: https://…/conectar#código"),
});
type DatosEnlace = z.infer<typeof esquemaEnlace>;

/**
 * Conectar: el enlace (`https://<sistema>/conectar#<código>`) se revisa primero
 * (`previsualizar`, no canjea nada) y solo con confirmación explícita se canjea. Un origen no
 * permitido no deja seguir.
 */
function DialogoConectar({ alConectar }: { alConectar: (c: Conector) => void }) {
    const [abierto, setAbierto] = useState(false);
    const [revision, setRevision] = useState<{ datos: DatosEnlace; previa: Previsualizacion } | null>(null);
    const [clave, setClave] = useState(nuevaClaveIdempotencia);
    const [error, setError] = useState<unknown>(null);
    const [conectando, setConectando] = useState(false);
    const form = useForm<DatosEnlace>({ resolver: zodResolver(esquemaEnlace), defaultValues: { sistema: "argosisp", enlace: "" } });
    const { isSubmitting, errors } = form.formState;

    function cerrar(a: boolean) {
        setAbierto(a);
        if (!a) {
            setRevision(null);
            setError(null);
            form.reset({ sistema: "argosisp", enlace: "" });
        }
    }

    async function revisar(d: DatosEnlace) {
        setError(null);
        try {
            const previa =
                d.sistema === "argosisp"
                    ? await resolver(api().POST("/api/v1/integraciones/argosisp/conexion/previsualizar", { body: { enlace: d.enlace } }))
                    : await resolver(api().POST("/api/v1/integraciones/horus/conexion/previsualizar", { body: { enlace: d.enlace } }));
            setRevision({ datos: d, previa });
        } catch (e) {
            setError(e);
        }
    }

    async function conectar() {
        if (!revision?.previa.permitido) return;
        setConectando(true);
        setError(null);
        const opciones = { body: { enlace: revision.datos.enlace }, params: { header: { "Idempotency-Key": clave } } };
        try {
            const c =
                revision.datos.sistema === "argosisp"
                    ? await resolver(api().POST("/api/v1/integraciones/argosisp/conectar", opciones))
                    : await resolver(api().POST("/api/v1/integraciones/horus/conectar", opciones));
            setClave(nuevaClaveIdempotencia());
            cerrar(false);
            alConectar(c);
        } catch (e) {
            setClave(claveTrasError(e, clave));
            setError(e);
        } finally {
            setConectando(false);
        }
    }

    const sistema = revision ? SISTEMAS[revision.datos.sistema as Sistema] : null;
    return (
        <>
            <Boton onClick={() => setAbierto(true)}>
                <PlugZap />
                Conectar
            </Boton>
            <DialogoProtegido sucio={form.formState.isDirty} open={abierto} onOpenChange={cerrar}>
                <DialogoContenido titulo="Conectar con otro sistema" descripcion="Pega el enlace de conexión que generó Argos o Horus Flow (vale 15 minutos y sirve una vez).">
                    {revision === null ? (
                        <form className="grid gap-4" noValidate onSubmit={(e) => void form.handleSubmit(revisar)(e)}>
                            <CampoSelector etiqueta="Sistema" {...form.register("sistema")}>
                                <option value="argosisp">Argos</option>
                                <option value="horus">Horus Flow</option>
                            </CampoSelector>
                            <Campo etiqueta="Enlace de conexión" autoComplete="off" placeholder="https://…/conectar#…" error={errors.enlace?.message} {...form.register("enlace")} />
                            {error ? (
                                <Aviso tono="error" titulo="No se pudo revisar el enlace">
                                    {mensajeDeError(error).detalle ?? mensajeDeError(error).titulo}
                                </Aviso>
                            ) : null}
                            <DialogoPie>
                                <DialogoCerrar asChild>
                                    <Boton type="button" variante="secundario">
                                        Cancelar
                                    </Boton>
                                </DialogoCerrar>
                                <Boton type="submit" disabled={isSubmitting}>
                                    {isSubmitting ? "Revisando…" : "Revisar enlace"}
                                </Boton>
                            </DialogoPie>
                        </form>
                    ) : (
                        <div className="grid gap-4">
                            <dl className="grid grid-cols-[auto_minmax(0,1fr)] gap-x-3 gap-y-1.5 rounded-lg border p-3 text-sm" data-testid="previsualizacion">
                                <dt className="text-muted-foreground">Sistema</dt>
                                <dd>{sistema}</dd>
                                <dt className="text-muted-foreground">Origen</dt>
                                <dd className="font-mono break-all">{revision.previa.origen}</dd>
                            </dl>
                            {revision.previa.permitido ? (
                                <Aviso tono="info" icono={ShieldCheck} titulo={`El token de Iris irá a ${revision.previa.origen}`}>
                                    {revision.previa.conectorExistente
                                        ? `Es el origen del conector activo de ${sistema}.`
                                        : "Es un origen permitido. Conéctalo solo si generaste este enlace tú mismo en ese sistema."}
                                </Aviso>
                            ) : (
                                <Aviso tono="error" icono={ShieldAlert} titulo="Este origen no está permitido">
                                    {textoMotivoOrigen(revision.previa.motivo)}
                                </Aviso>
                            )}
                            {error ? (
                                <Aviso tono="error" titulo="No se pudo conectar">
                                    {mensajeDeError(error).detalle ?? mensajeDeError(error).titulo}
                                </Aviso>
                            ) : null}
                            <DialogoPie>
                                <Boton type="button" variante="secundario" onClick={() => setRevision(null)} disabled={conectando}>
                                    Volver
                                </Boton>
                                {revision.previa.permitido ? (
                                    <Boton onClick={() => void conectar()} disabled={conectando}>
                                        <PlugZap />
                                        {conectando ? "Conectando…" : `Conectar con ${revision.previa.origen}`}
                                    </Boton>
                                ) : null}
                            </DialogoPie>
                        </div>
                    )}
                </DialogoContenido>
            </DialogoProtegido>
        </>
    );
}
