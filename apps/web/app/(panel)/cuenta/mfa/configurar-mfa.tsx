"use client";

import { zodResolver } from "@hookform/resolvers/zod";
import type { Schema } from "@iris/contratos";
import { Aviso, Boton, Campo, Tarjeta } from "@iris/ui";
import { ArrowRight, CircleCheck, RotateCw, ShieldCheck, Smartphone } from "lucide-react";
import { useRouter, useSearchParams } from "next/navigation";
import { useState } from "react";
import { useForm } from "react-hook-form";
import { BotonCopiar } from "@/componentes/cuenta/boton-copiar";
import { CodigoQr } from "@/componentes/cuenta/codigo-qr";
import { EncabezadoPagina } from "@/componentes/encabezado-pagina";
import { api, nuevaClaveIdempotencia, resolver } from "@/lib/api/cliente";
import { ErrorApi, mensajeDeError } from "@/lib/api/errores";
import { agruparSecreto } from "@/lib/cuenta/esquemas";
import { esRutaCampo, siguienteSeguro } from "@/lib/cuenta/sesion";
import { esquemaCodigoMFA, type DatosCodigoMFA } from "@/lib/esquemas";
import { useClaveIdempotencia } from "@/lib/idempotencia";
import { useSesion } from "@/lib/sesion";

type Secreto = Schema<"MFAConfigurado">;
type Etapa = { tipo: "inicio" } | { tipo: "escanear"; secreto: Secreto } | { tipo: "listo" };

/**
 * Configurar el MFA propio: `POST /yo/mfa` genera el secreto (no se guarda para la
 * `Idempotency-Key`: una clave nueva por intento) y `POST /yo/mfa/activar` lo activa con el
 * primer código. El secreto solo vive en esta pantalla.
 */
export function ConfigurarMfa() {
    const { yo, refrescarSesion } = useSesion();
    const router = useRouter();
    const params = useSearchParams();
    const siguiente = siguienteSeguro(params.get("siguiente"));
    const [etapa, setEtapa] = useState<Etapa>({ tipo: "inicio" });
    const [generando, setGenerando] = useState(false);
    const [error, setError] = useState<unknown>(null);
    const clave = useClaveIdempotencia();
    const formulario = useForm<{ codigo: string }, unknown, DatosCodigoMFA>({
        resolver: zodResolver(esquemaCodigoMFA),
        defaultValues: { codigo: "" },
    });

    function continuar() {
        if (esRutaCampo(siguiente)) {
            window.location.assign(siguiente);
            return;
        }
        router.push(siguiente);
    }

    async function generar() {
        setGenerando(true);
        setError(null);
        try {
            const secreto = await resolver(api().POST("/api/v1/yo/mfa", { params: { header: { "Idempotency-Key": nuevaClaveIdempotencia() } } }));
            formulario.reset({ codigo: "" });
            setEtapa({ tipo: "escanear", secreto });
        } catch (e) {
            if (e instanceof ErrorApi && e.codigo === "mfa_activo") {
                refrescarSesion();
            }
            setError(e);
        } finally {
            setGenerando(false);
        }
    }

    async function activar(datos: DatosCodigoMFA) {
        setError(null);
        try {
            await resolver(api().POST("/api/v1/yo/mfa/activar", { body: datos, params: { header: { "Idempotency-Key": clave.actual() } } }));
            clave.renovar();
            setEtapa({ tipo: "listo" });
            refrescarSesion();
        } catch (e) {
            clave.trasError(e);
            if (e instanceof ErrorApi && e.codigo === "codigo_invalido") {
                formulario.setError("codigo", { message: "El código no coincide. Revisa que la hora del teléfono sea automática y escribe el código que se ve ahora." });
                formulario.setValue("codigo", "");
                formulario.setFocus("codigo");
                return;
            }
            if (e instanceof ErrorApi && e.codigo === "mfa_activo") {
                setEtapa({ tipo: "listo" });
                refrescarSesion();
                return;
            }
            setError(e);
        }
    }

    const encabezado = (
        <EncabezadoPagina titulo="Verificación en dos pasos" descripcion="Un código de tu teléfono, además de la contraseña, para entrar a Iris." />
    );
    const avisoError = error ? (
        <Aviso
            tono="error"
            titulo={error instanceof ErrorApi && error.codigo === "mfa_activo" ? "Tu verificación en dos pasos ya está activa" : "No se pudo configurar la verificación"}
            accion={
                etapa.tipo === "inicio" ? (
                    <Boton variante="secundario" tamano="sm" onClick={generar} disabled={generando}>
                        <RotateCw />
                        Reintentar
                    </Boton>
                ) : undefined
            }
        >
            {mensajeDeError(error).detalle}
        </Aviso>
    ) : null;

    if (etapa.tipo === "listo" || (etapa.tipo === "inicio" && yo.usuario.mfaActivo)) {
        return (
            <>
                {encabezado}
                <Tarjeta className="grid max-w-xl gap-4 p-5 sm:p-6">
                    <div className="flex items-start gap-3">
                        <CircleCheck aria-hidden className="mt-0.5 size-5 shrink-0 text-success" />
                        <div className="grid gap-1">
                            <h2 className="font-semibold">{etapa.tipo === "listo" ? "Verificación activada" : "Tu verificación en dos pasos está activa"}</h2>
                            <p className="text-sm text-muted-foreground">
                                Desde tu próximo inicio de sesión, Iris te pedirá el código de tu app de autenticación. Si cambias de teléfono,
                                pide a un administrador que la reinicie.
                            </p>
                        </div>
                    </div>
                    <Boton onClick={continuar} className="justify-self-start">
                        Continuar
                        <ArrowRight />
                    </Boton>
                </Tarjeta>
            </>
        );
    }

    if (etapa.tipo === "inicio") {
        return (
            <>
                {encabezado}
                <Tarjeta className="grid max-w-xl gap-4 p-5 sm:p-6">
                    {avisoError}
                    <div className="flex items-start gap-3">
                        <Smartphone aria-hidden className="mt-0.5 size-5 shrink-0 text-primary" />
                        <div className="grid gap-1">
                            <h2 className="font-semibold">Necesitas una app de autenticación</h2>
                            <p className="text-sm text-muted-foreground">
                                Por ejemplo Google Authenticator, Microsoft Authenticator o la de tu gestor de contraseñas. Vas a escanear un código
                                QR y escribir el primer código de 6 dígitos que te muestre.
                            </p>
                        </div>
                    </div>
                    <div className="flex flex-col-reverse gap-2 sm:flex-row">
                        <Boton onClick={generar} disabled={generando}>
                            <ShieldCheck />
                            {generando ? "Preparando…" : "Empezar"}
                        </Boton>
                        <Boton variante="fantasma" onClick={continuar}>
                            Ahora no
                        </Boton>
                    </div>
                </Tarjeta>
            </>
        );
    }

    const { errors, isSubmitting } = formulario.formState;
    const secreto = etapa.secreto;
    return (
        <>
            {encabezado}
            <Tarjeta className="grid max-w-xl gap-5 p-5 sm:p-6">
                {avisoError}
                <ol className="grid gap-5">
                    <li className="grid gap-3">
                        <h2 className="font-semibold">1. Escanea el código con tu app</h2>
                        <CodigoQr texto={secreto.url} etiqueta="Código QR para tu app de autenticación" className="size-52 justify-self-center rounded-md sm:justify-self-start" />
                        <div className="grid gap-1.5">
                            <p className="text-sm text-muted-foreground">¿No puedes escanearlo? Escribe esta clave en la app:</p>
                            <div className="flex flex-wrap items-start gap-2">
                                <code
                                    className="min-w-0 rounded-md border bg-elevated px-3 py-2 font-mono text-sm tracking-wider break-all"
                                    data-testid="secreto-mfa"
                                >
                                    {agruparSecreto(secreto.secreto)}
                                </code>
                                <BotonCopiar texto={secreto.secreto} etiqueta="Copiar clave" copiado="Clave copiada" sensible />
                            </div>
                        </div>
                    </li>
                    <li>
                        <form noValidate onSubmit={formulario.handleSubmit(activar)} className="grid gap-3" aria-label="Activar verificación">
                            <h2 className="font-semibold">2. Escribe el código que te muestra</h2>
                            <Campo
                                etiqueta="Código de verificación"
                                inputMode="numeric"
                                autoComplete="one-time-code"
                                maxLength={7}
                                className="max-w-56 [&_input]:text-center [&_input]:font-mono [&_input]:text-lg [&_input]:tracking-[0.4em] [&_input]:tabular-nums"
                                error={errors.codigo?.message}
                                {...formulario.register("codigo")}
                            />
                            <div className="flex flex-col-reverse gap-2 sm:flex-row">
                                <Boton type="submit" disabled={isSubmitting}>
                                    <ShieldCheck />
                                    {isSubmitting ? "Activando…" : "Activar"}
                                </Boton>
                                <Boton variante="fantasma" onClick={continuar}>
                                    Ahora no
                                </Boton>
                            </div>
                        </form>
                    </li>
                </ol>
            </Tarjeta>
        </>
    );
}
