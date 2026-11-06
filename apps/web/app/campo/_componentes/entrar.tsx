"use client";

import { zodResolver } from "@hookform/resolvers/zod";
import type { Schema } from "@iris/contratos";
import { avisoEntrar, iniciales, type AvisoEntrar, type UsuarioRecordado } from "@iris/sync/campo";
import { Aviso, Boton, CampoCodigo, CampoConIcono, LogoIris, Tarjeta } from "@iris/ui";
import { ArrowLeft, CircleHelp, Clock, CloudOff, Lock, Mail } from "lucide-react";
import { useEffect, useState } from "react";
import { Controller, useForm } from "react-hook-form";
import { api, resolver } from "@/lib/api/cliente";
import { ErrorApi } from "@/lib/api/errores";
import { cabecerasCampo } from "@/lib/campo/app";
import { borrarCachesChat } from "@/lib/campo/chat";
import { olvidarUsuarioRecordado, usuarioRecordado } from "@/lib/campo/sesion";
import { requiereCambio, rutaCambiarContrasena } from "@/lib/cuenta/sesion";
import { esquemaCodigoMFA, esquemaInicioSesion, type DatosCodigoMFA, type DatosInicioSesion } from "@/lib/esquemas";
import { formatearEspera } from "@/lib/tiempo";
import { HojaCampo } from "./hoja";

/*
 * Entrada del técnico en la PWA (diseño «Login C · Combinada», nov 2026), igual que la app nativa y
 * en un solo paso: logo y «Hola, técnico», la tarjeta con correo y contraseña (o el usuario
 * recordado con «¿No eres tú?» y solo la contraseña), el ojo, errores en línea, «Entrar» y
 * «¿Olvidaste tu contraseña?», y el aviso «Sin conexión». El código de 6 casillas solo si
 * `POST /sesion` responde `mfaRequerido`. Es de la app de campo (vive en /campo, bajo su service
 * worker): el inicio de sesión del panel (/entrar) no cambia. Sin huella (no hay en la web) y sin
 * «Entrar con Argos»: el autologin empieza en Argos con un ticket de un solo uso.
 */

function avisoDeError(error: unknown): AvisoEntrar {
    if (error instanceof ErrorApi) {
        return avisoEntrar({ estado: error.estado, codigo: error.codigo, titulo: error.titulo, detalle: error.detalle, problema: error.problema });
    }
    return avisoEntrar({ estado: -1 });
}

function useBloqueo() {
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
        bloquear: (segundos: number) => {
            const ahora = Date.now();
            setReloj({ hasta: ahora + segundos * 1000, ahora });
        },
        liberar: () => setReloj(null),
    };
}

export type PropsEntrarCampo = {
    /** Con la sesión abierta (y el código si hacía falta). */
    alEntrar: () => void;
    motivo?: "expirada" | null;
    /** Aviso «Sin conexión» (de `avisoSinConexion`). */
    sinConexion?: { titulo: string; detalle: string } | null;
};

export function EntrarCampo({ alEntrar, motivo = null, sinConexion = null }: PropsEntrarCampo) {
    const [recordado, setRecordado] = useState<UsuarioRecordado | null>(() => usuarioRecordado());
    const [paso, setPaso] = useState<"credenciales" | "mfa">("credenciales");
    const [aviso, setAviso] = useState<AvisoEntrar | null>(
        motivo === "expirada" ? { tono: "info", titulo: "Tu sesión terminó", detalle: "Vuelve a entrar para continuar.", campo: null } : null,
    );
    const [olvido, setOlvido] = useState(false);
    const bloqueo = useBloqueo();
    const bloqueado = bloqueo.restante > 0;
    const credenciales = useForm<DatosInicioSesion>({ resolver: zodResolver(esquemaInicioSesion), defaultValues: { correo: recordado?.correo ?? "", contrasena: "" } });
    const mfa = useForm<{ codigo: string }, unknown, DatosCodigoMFA>({ resolver: zodResolver(esquemaCodigoMFA), defaultValues: { codigo: "" } });

    function entrar(usuario: Schema<"Usuario"> | undefined) {
        // Contraseña temporal: la API solo deja cambiarla; luego vuelve a /campo.
        if (requiereCambio(usuario)) {
            window.location.assign(rutaCambiarContrasena("/campo"));
            return;
        }
        alEntrar();
    }

    async function enviarCredenciales(datos: DatosInicioSesion) {
        setAviso(null);
        try {
            const sesion = await resolver(api().POST("/api/v1/sesion", { body: datos, headers: cabecerasCampo() }));
            bloqueo.liberar();
            if (sesion.mfaRequerido) {
                mfa.reset({ codigo: "" });
                setPaso("mfa");
                return;
            }
            entrar(sesion.usuario);
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
            const a = avisoDeError(error);
            if (a.campo === "contrasena") {
                credenciales.setError("contrasena", { message: a.titulo });
                return;
            }
            setAviso(a);
        }
    }

    async function enviarCodigo(datos: DatosCodigoMFA) {
        setAviso(null);
        try {
            const sesion = await resolver(api().POST("/api/v1/sesion/mfa", { body: datos, headers: cabecerasCampo() }));
            entrar(sesion.usuario);
        } catch (error) {
            const a = avisoDeError(error);
            if (a.campo === "codigo") {
                mfa.setError("codigo", { message: a.titulo });
                mfa.setValue("codigo", "");
                return;
            }
            if (error instanceof ErrorApi && (error.codigo === "mfa_bloqueado" || error.codigo === "no_autenticado")) {
                credenciales.setValue("contrasena", "");
                setPaso("credenciales");
            }
            setAviso(a);
        }
    }

    const avisoVisible = aviso ? (
        <Aviso tono={aviso.tono} titulo={aviso.titulo} data-testid="aviso-entrar">
            {aviso.detalle}
        </Aviso>
    ) : null;

    if (paso === "mfa") {
        const { errors, isSubmitting } = mfa.formState;
        return (
            <form noValidate onSubmit={mfa.handleSubmit(enviarCodigo)} className="grid gap-6" aria-labelledby="titulo-codigo" data-slot="entrar-codigo">
                <Boton
                    variante="secundario"
                    aria-label="Volver"
                    title="Volver"
                    className="size-12 rounded-full bg-[var(--campo-panel)] p-0 md:size-12 [&_svg]:size-5"
                    onClick={() => {
                        setAviso(null);
                        setPaso("credenciales");
                    }}
                >
                    <ArrowLeft aria-hidden />
                </Boton>
                <div className="grid gap-2">
                    <h1 id="titulo-codigo" className="text-[2rem] leading-[1.05] font-bold tracking-[-0.03em]">
                        Código de verificación
                    </h1>
                    <p className="text-muted-foreground">Solo te lo pedimos porque tienes la verificación en dos pasos activa.</p>
                </div>
                {avisoVisible}
                <Tarjeta className="grid gap-5 p-5">
                    <Controller
                        control={mfa.control}
                        name="codigo"
                        render={({ field }) => (
                            <CampoCodigo etiqueta="Escribe los 6 dígitos de tu app de autenticación" valor={field.value} alCambiar={field.onChange} error={errors.codigo?.message} name="codigo" />
                        )}
                    />
                    <Boton type="submit" tamano="lg" disabled={isSubmitting} className="h-[3.25rem] w-full rounded-full text-base md:h-[3.25rem]">
                        {isSubmitting ? "Verificando…" : "Verificar"}
                    </Boton>
                </Tarjeta>
            </form>
        );
    }

    const { errors, isSubmitting } = credenciales.formState;
    return (
        <div className="grid gap-6" data-slot="entrar-campo">
            <div className="grid gap-2">
                <LogoIris className="size-14" titulo="Iris" />
                <h1 className="text-[2rem] leading-[1.05] font-bold tracking-[-0.03em]">Hola, técnico</h1>
                <p className="text-muted-foreground">Entra para ver tus órdenes de hoy.</p>
            </div>
            {bloqueado ? (
                <Aviso tono="warning" icono={Clock} titulo={`Demasiados intentos, espera ${formatearEspera(bloqueo.restante)}`}>
                    Por seguridad bloqueamos este correo un momento. Podrás volver a intentar cuando termine la espera.
                </Aviso>
            ) : (
                avisoVisible
            )}
            <Tarjeta className="p-5">
                <form noValidate onSubmit={credenciales.handleSubmit(enviarCredenciales)} className="grid gap-4" aria-label="Iniciar sesión">
                    {recordado ? (
                        <div className="flex items-center gap-3" data-slot="usuario-recordado">
                            <span aria-hidden className="grid size-12 shrink-0 place-content-center rounded-full bg-primary text-[1.0625rem] font-bold text-primary-foreground">
                                {iniciales(recordado.nombre) || "?"}
                            </span>
                            <span className="grid min-w-0 flex-1">
                                <span className="truncate font-semibold">{recordado.nombre}</span>
                                <span className="truncate text-sm text-muted-foreground">{recordado.correo}</span>
                            </span>
                            <Boton
                                variante="fantasma"
                                tamano="lg"
                                className="h-12 shrink-0 px-2 text-primary md:h-12"
                                onClick={() => {
                                    olvidarUsuarioRecordado();
                                    // La caché del chat de este navegador también se va (no la cola).
                                    void borrarCachesChat().catch(() => undefined);
                                    setRecordado(null);
                                    setAviso(null);
                                    credenciales.reset({ correo: "", contrasena: "" });
                                }}
                            >
                                ¿No eres tú?
                            </Boton>
                        </div>
                    ) : (
                        <CampoConIcono
                            etiqueta="Correo"
                            icono={Mail}
                            type="email"
                            inputMode="email"
                            autoComplete="username"
                            autoCapitalize="none"
                            spellCheck={false}
                            enterKeyHint="next"
                            error={errors.correo?.message}
                            {...credenciales.register("correo")}
                        />
                    )}
                    <CampoConIcono
                        etiqueta="Contraseña"
                        icono={Lock}
                        secreto
                        autoComplete="current-password"
                        enterKeyHint="go"
                        autoFocus={recordado !== null}
                        error={errors.contrasena?.message}
                        {...credenciales.register("contrasena")}
                    />
                    <Boton type="submit" tamano="lg" disabled={isSubmitting || bloqueado} className="h-[3.25rem] w-full rounded-full text-base md:h-[3.25rem]">
                        {isSubmitting ? "Entrando…" : "Entrar"}
                    </Boton>
                    <Boton variante="fantasma" tamano="lg" className="h-12 w-full text-primary md:h-12" onClick={() => setOlvido(true)}>
                        ¿Olvidaste tu contraseña?
                    </Boton>
                </form>
            </Tarjeta>
            {sinConexion ? (
                <Aviso tono="warning" icono={CloudOff} titulo={sinConexion.titulo} data-testid="aviso-sin-conexion">
                    {sinConexion.detalle}
                </Aviso>
            ) : null}
            <HojaCampo abierto={olvido} alCambiar={setOlvido} titulo="¿Olvidaste tu contraseña?">
                <div className="grid gap-4">
                    <p className="flex items-start gap-2.5">
                        <CircleHelp aria-hidden className="mt-0.5 size-5 shrink-0 text-primary" />
                        <span>Pídele a tu supervisor o al administrador de Iris que te asigne una contraseña temporal. Al entrar con ella, Iris te pedirá cambiarla.</span>
                    </p>
                    <Boton tamano="lg" className="h-12 w-full rounded-full" onClick={() => setOlvido(false)}>
                        Entendido
                    </Boton>
                </div>
            </HojaCampo>
        </div>
    );
}
