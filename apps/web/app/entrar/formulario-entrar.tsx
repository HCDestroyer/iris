"use client";

import { zodResolver } from "@hookform/resolvers/zod";
import { Aviso, Boton, Campo } from "@iris/ui";
import { ArrowLeft, Clock, LogIn, ShieldCheck } from "lucide-react";
import { useRouter, useSearchParams } from "next/navigation";
import { useEffect, useState } from "react";
import { useForm } from "react-hook-form";
import { api, resolver } from "@/lib/api/cliente";
import { ErrorApi, mensajeDeError } from "@/lib/api/errores";
import {
    esquemaCodigoMFA,
    esquemaInicioSesion,
    type DatosCodigoMFA,
    type DatosInicioSesion,
} from "@/lib/esquemas";
import { formatearEspera, rutaSegura } from "@/lib/tiempo";

type AvisoPantalla = { tono: "error" | "warning" | "info"; titulo: string; detalle?: string | undefined };

/** Mensaje claro para los errores de inicio de sesión; el resto, con el título de la API. */
function avisoDeError(error: unknown): AvisoPantalla {
    if (error instanceof ErrorApi) {
        switch (error.codigo) {
            case "credenciales_invalidas":
                return { tono: "error", titulo: "Correo o contraseña incorrectos", detalle: "Revisa los datos e inténtalo de nuevo." };
            case "mfa_bloqueado":
                return {
                    tono: "error",
                    titulo: "Demasiados códigos inválidos",
                    detalle: "Por seguridad cerramos el intento. Vuelve a iniciar sesión.",
                };
            case "no_autenticado":
                return { tono: "warning", titulo: "El inicio de sesión venció", detalle: "Vuelve a escribir tu correo y contraseña." };
            case "sin_conexion":
            case "api_no_disponible":
                return { tono: "error", titulo: "No pudimos contactar a Iris", detalle: "Revisa tu conexión e inténtalo de nuevo." };
        }
    }
    const { titulo, detalle } = mensajeDeError(error);
    return { tono: "error", titulo, detalle };
}

/**
 * Bloqueo por 429: cuenta regresiva desde `Retry-After`. `ahora` se fija al bloquear y avanza
 * cada segundo, así el texto («espera N min») sale del tiempo real que falta.
 */
function useBloqueo(): { restante: number; bloquear: (segundos: number) => void; liberar: () => void } {
    const [reloj, setReloj] = useState<{ hasta: number; ahora: number } | null>(null);
    const activo = reloj !== null;
    useEffect(() => {
        if (!activo) {
            return undefined;
        }
        const id = window.setInterval(() => {
            const ahora = Date.now();
            setReloj((r) => (r === null || ahora >= r.hasta ? null : { ...r, ahora }));
        }, 1000);
        return () => window.clearInterval(id);
    }, [activo]);
    const restante = reloj === null ? 0 : Math.max(0, Math.ceil((reloj.hasta - reloj.ahora) / 1000));
    return {
        restante,
        bloquear: (segundos) => {
            const ahora = Date.now();
            setReloj({ hasta: ahora + segundos * 1000, ahora });
        },
        liberar: () => setReloj(null),
    };
}

export function FormularioEntrar() {
    const router = useRouter();
    const params = useSearchParams();
    const siguiente = rutaSegura(params.get("siguiente"));
    const [paso, setPaso] = useState<"credenciales" | "mfa">("credenciales");
    const [aviso, setAviso] = useState<AvisoPantalla | null>(
        params.get("motivo") === "expirada"
            ? { tono: "info", titulo: "Tu sesión terminó", detalle: "Vuelve a entrar para continuar." }
            : null,
    );
    const bloqueo = useBloqueo();
    const restante = bloqueo.restante;
    const bloqueado = restante > 0;

    const credenciales = useForm<DatosInicioSesion>({
        resolver: zodResolver(esquemaInicioSesion),
        defaultValues: { correo: "", contrasena: "" },
    });
    const mfa = useForm<{ codigo: string }, unknown, DatosCodigoMFA>({
        resolver: zodResolver(esquemaCodigoMFA),
        defaultValues: { codigo: "" },
    });

    function entrar() {
        router.replace(siguiente);
        router.refresh();
    }

    async function enviarCredenciales(datos: DatosInicioSesion) {
        setAviso(null);
        try {
            const sesion = await resolver(api().POST("/api/v1/sesion", { body: datos }));
            bloqueo.liberar();
            if (sesion.mfaRequerido) {
                mfa.reset({ codigo: "" });
                setPaso("mfa");
                return;
            }
            entrar();
        } catch (error) {
            if (error instanceof ErrorApi && error.estado === 429) {
                bloqueo.bloquear(error.reintentarEn ?? 15 * 60);
                return;
            }
            if (error instanceof ErrorApi && error.estado === 422) {
                for (const campo of ["correo", "contrasena"] as const) {
                    const mensaje = error.errorDeCampo(campo);
                    if (mensaje) {
                        credenciales.setError(campo, { message: mensaje });
                    }
                }
            }
            setAviso(avisoDeError(error));
        }
    }

    async function enviarCodigo(datos: DatosCodigoMFA) {
        setAviso(null);
        try {
            await resolver(api().POST("/api/v1/sesion/mfa", { body: datos }));
            entrar();
        } catch (error) {
            if (error instanceof ErrorApi && error.codigo === "codigo_invalido") {
                mfa.setError("codigo", { message: "Código incorrecto o ya usado. Revisa tu app de autenticación." });
                mfa.setValue("codigo", "");
                mfa.setFocus("codigo");
                return;
            }
            if (error instanceof ErrorApi && (error.codigo === "mfa_bloqueado" || error.codigo === "no_autenticado")) {
                credenciales.setValue("contrasena", "");
                setPaso("credenciales");
            }
            setAviso(avisoDeError(error));
        }
    }

    const avisoVisible = aviso ? (
        <Aviso tono={aviso.tono} titulo={aviso.titulo}>
            {aviso.detalle}
        </Aviso>
    ) : null;

    if (paso === "mfa") {
        const { errors, isSubmitting } = mfa.formState;
        return (
            <form noValidate onSubmit={mfa.handleSubmit(enviarCodigo)} className="grid gap-5" aria-labelledby="titulo-mfa">
                <div className="grid gap-1.5">
                    <ShieldCheck aria-hidden className="size-6 text-primary" />
                    <h1 id="titulo-mfa" className="text-xl font-semibold tracking-[-0.015em]">
                        Verificación en dos pasos
                    </h1>
                    <p className="text-sm text-muted-foreground">
                        Escribe el código de 6 dígitos de tu app de autenticación.
                    </p>
                </div>
                {avisoVisible}
                <Campo
                    etiqueta="Código de verificación"
                    inputMode="numeric"
                    autoComplete="one-time-code"
                    autoFocus
                    maxLength={7}
                    className="[&_input]:text-center [&_input]:font-mono [&_input]:text-lg [&_input]:tracking-[0.4em] [&_input]:tabular-nums"
                    error={errors.codigo?.message}
                    {...mfa.register("codigo")}
                />
                <div className="grid gap-2">
                    <Boton type="submit" disabled={isSubmitting} className="w-full">
                        <ShieldCheck />
                        {isSubmitting ? "Verificando…" : "Verificar"}
                    </Boton>
                    <Boton
                        variante="fantasma"
                        className="w-full"
                        onClick={() => {
                            setAviso(null);
                            setPaso("credenciales");
                        }}
                    >
                        <ArrowLeft />
                        Volver
                    </Boton>
                </div>
            </form>
        );
    }

    const { errors, isSubmitting } = credenciales.formState;
    return (
        <form noValidate onSubmit={credenciales.handleSubmit(enviarCredenciales)} className="grid gap-5" aria-labelledby="titulo-entrar">
            <div className="grid gap-1">
                <h1 id="titulo-entrar" className="text-xl font-semibold tracking-[-0.015em]">
                    Iniciar sesión
                </h1>
                <p className="text-sm text-muted-foreground">Entra con tu cuenta de Iris.</p>
            </div>
            {bloqueado ? (
                <Aviso tono="warning" icono={Clock} titulo={`Demasiados intentos, espera ${formatearEspera(restante)}`}>
                    Por seguridad bloqueamos este correo un momento. Podrás volver a intentar cuando termine la espera.
                </Aviso>
            ) : (
                avisoVisible
            )}
            <Campo
                etiqueta="Correo"
                type="email"
                autoComplete="username"
                inputMode="email"
                autoFocus
                error={errors.correo?.message}
                {...credenciales.register("correo")}
            />
            <Campo
                etiqueta="Contraseña"
                type="password"
                autoComplete="current-password"
                error={errors.contrasena?.message}
                {...credenciales.register("contrasena")}
            />
            <Boton type="submit" disabled={isSubmitting || bloqueado} className="w-full">
                <LogIn />
                {isSubmitting ? "Entrando…" : "Entrar"}
            </Boton>
        </form>
    );
}
