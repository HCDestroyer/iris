"use client";

// Pantallas propias de «Yo» en la PWA (diseño aprobado por el PM, nov 2026), cada una con su
// dirección (`?vista=yo&seccion=contrasena|dos_pasos`) y su «Atrás», como en la app nativa:
// - «Cambiar contraseña»: actual, nueva y repetir con el ojo; validación espejo de la API con las
//   ayudas en verde; `POST /yo/contrasena` con `Idempotency-Key`; 422 por campo; 429 cierra la
//   sesión (lleva a entrar); sin señal no se intenta (no va a la cola).
// - «Verificación en dos pasos»: apagada, `POST /yo/mfa` una vez por intento (clave nueva: no es
//   repetible) con «Abrir app» (`otpauth://`), «Copiar clave», la clave en grupos de 4 y el código
//   de 6 dígitos → `POST /yo/mfa/activar`; activa, solo informa (no hay desactivar).
// Salir con algo escrito (botón, «Atrás» del navegador o recargar) pregunta antes.
import { zodResolver } from "@hookform/resolvers/zod";
import type { Schema } from "@iris/contratos";
import { Aviso, Boton, CampoCodigo, CampoConIcono, Cargando, DialogoConfirmar, Tarjeta, useProteccionSalida, liberarProteccion } from "@iris/ui";
import {
    agruparSecreto,
    cuerpoContrasenaPropia,
    enlaceOtpauth,
    esquemaCodigoActivar,
    esquemaContrasenaPropia,
    LONGITUD_MINIMA_CONTRASENA,
    reglasContrasena,
    TEXTO_ACTIVAR_DOS_PASOS,
    TEXTO_REINICIAR_DOS_PASOS,
    TEXTO_SESIONES_CONTRASENA,
    TEXTO_SIN_SENAL_CUENTA,
    textoBloqueoContrasena,
    type DatosContrasenaPropia,
} from "@iris/sync/campo";
import { ArrowLeft, Check, CircleCheck, Clock, CloudOff, ExternalLink, ShieldCheck } from "lucide-react";
import { useCallback, useEffect, useRef, useState, type ReactNode } from "react";
import { Controller, useForm, useWatch } from "react-hook-form";
import { BotonCopiar, useBorradorPortapapeles } from "@/componentes/cuenta/boton-copiar";
import { crearClienteIris, resolver } from "@/lib/api/cliente";
import { ErrorApi } from "@/lib/api/errores";
import { cabecerasCampo } from "@/lib/campo/app";
import { leerYo } from "@/lib/campo/sesion";
import { formatearEspera } from "@/lib/tiempo";
import { useClaveIdempotencia } from "@/lib/idempotencia";
import { useLectura } from "@/lib/lectura";
import { useCampo } from "./contexto";

/** Cliente de las llamadas de la cuenta: la sesión del teléfono, sin empresa ni diálogos del panel. */
function clienteCuenta() {
    return crearClienteIris({ leerEmpresa: () => undefined, alProblemaEmpresa: () => undefined, alCambioRequerido: () => undefined, reautenticar: async () => false });
}

/** `GET /yo` al abrir (con «Reintentar»): rol, empresa y si la verificación en dos pasos está activa. */
export function useCuenta() {
    const { sesion } = useCampo();
    return useLectura(() => leerYo(sesion.empresaActiva));
}

function useConSenal(): boolean {
    return useCampo().estado.conexion !== "sin_conexion";
}

/** «Atrás» redondo de 48 px y el título de la pantalla. */
function Encabezado({ titulo, alVolver }: { titulo: string; alVolver: () => void }) {
    return (
        <div className="flex items-center gap-3.5">
            <Boton
                variante="secundario"
                aria-label="Atrás"
                title="Atrás"
                className="size-12 shrink-0 rounded-full bg-[var(--campo-panel)] p-0 md:size-12 [&_svg]:size-5"
                onClick={alVolver}
            >
                <ArrowLeft aria-hidden />
            </Boton>
            <h1 className="text-[1.375rem] leading-tight font-bold tracking-[-0.015em]">{titulo}</h1>
        </div>
    );
}

/**
 * Volver a «Yo»; con algo escrito pregunta antes (el botón, el «Atrás» del navegador con una
 * entrada propia en el historial, y recargar o cerrar).
 */
function useSalida(sucio: boolean): { volver: () => void; pregunta: ReactNode; liberar: () => Promise<void> } {
    const { navegar } = useCampo();
    const proteccion = useProteccionSalida(sucio);
    const [preguntando, setPreguntando] = useState(false);
    const irAYo = useCallback(() => navegar({ tipo: "yo" }), [navegar]);
    const volver = useCallback(() => (sucio ? setPreguntando(true) : irAYo()), [sucio, irAYo]);
    const abierta = preguntando || proteccion.pendiente !== null;
    const pregunta = (
        <DialogoConfirmar
            open={abierta}
            onOpenChange={(a) => {
                if (!a) {
                    setPreguntando(false);
                    if (proteccion.pendiente !== null) {
                        proteccion.seguir();
                    }
                }
            }}
            titulo="¿Salir sin guardar?"
            descripcion="Lo que escribiste aquí no se guardó."
            textoCancelar="Seguir editando"
            textoConfirmar="Salir sin guardar"
            peligro
            alConfirmar={async () => {
                if (proteccion.pendiente !== null) {
                    proteccion.confirmar();
                    return;
                }
                setPreguntando(false);
                await liberarProteccion();
                irAYo();
            }}
        />
    );
    return { volver, pregunta, liberar: liberarProteccion };
}

function AvisoSinSenal() {
    return (
        <Aviso tono="warning" icono={CloudOff} titulo="Hace falta señal" data-testid="aviso-sin-senal">
            {TEXTO_SIN_SENAL_CUENTA}
        </Aviso>
    );
}

type ErrorPantalla = { tipo: "sin_senal" } | { tipo: "sesion"; titulo: string; detalle: string } | { tipo: "otro"; titulo: string; detalle: string };

function errorDePantalla(e: unknown, que: string): ErrorPantalla {
    if (e instanceof ErrorApi && e.estado === 0) {
        return { tipo: "sin_senal" };
    }
    if (e instanceof ErrorApi && e.estado === 429) {
        return { tipo: "sesion", ...textoBloqueoContrasena(formatearEspera(e.reintentarEn ?? 15 * 60)) };
    }
    if (e instanceof ErrorApi && e.estado === 401) {
        return { tipo: "sesion", titulo: "Tu sesión terminó", detalle: "Vuelve a entrar. Lo pendiente sigue guardado en el teléfono." };
    }
    return { tipo: "otro", titulo: `No se pudo ${que}`, detalle: e instanceof ErrorApi ? (e.detalle ?? e.titulo) : "Vuelve a intentarlo." };
}

/** 429 o sesión vencida: la API ya cerró la sesión; se vuelve a entrar conservando lo pendiente. */
function SesionCerrada({ titulo, detalle }: { titulo: string; detalle: string }) {
    const { cerrarSesion, notificar } = useCampo();
    const [saliendo, setSaliendo] = useState(false);
    return (
        <Aviso tono="warning" icono={Clock} titulo={titulo} data-testid="aviso-sesion-cerrada">
            <p>{detalle}</p>
            <Boton
                tamano="lg"
                className="mt-3 rounded-full"
                disabled={saliendo}
                onClick={() => {
                    setSaliendo(true);
                    void liberarProteccion()
                        .then(() => cerrarSesion({ conservarPendientes: true }))
                        .catch(() => {
                            setSaliendo(false);
                            notificar({ tono: "error", texto: "No se pudo cerrar la sesión en el teléfono. Vuelve a intentarlo." });
                        });
                }}
            >
                Ir a iniciar sesión
            </Boton>
        </Aviso>
    );
}

function AvisoError({ error, reintentar }: { error: ErrorPantalla; reintentar: () => void }) {
    if (error.tipo === "sesion") {
        return <SesionCerrada titulo={error.titulo} detalle={error.detalle} />;
    }
    const sinSenal = error.tipo === "sin_senal";
    return (
        <Aviso tono={sinSenal ? "warning" : "error"} {...(sinSenal ? { icono: CloudOff } : {})} titulo={sinSenal ? "Sin señal" : error.titulo} data-testid="aviso-error-cuenta">
            <p>{sinSenal ? "No llegó a Iris. Busca señal y vuelve a intentarlo." : error.detalle}</p>
            <Boton variante="secundario" tamano="lg" className="mt-3" onClick={reintentar}>
                Reintentar
            </Boton>
        </Aviso>
    );
}

function AyudaCumplida({ texto, cumplida, id }: { texto: string; cumplida: boolean; id: string }) {
    return cumplida ? (
        <p data-testid={id} className="-mt-2 flex items-center gap-1.5 text-sm font-medium text-success">
            <Check aria-hidden className="size-4" />
            {texto}
        </p>
    ) : null;
}

// --- Cambiar contraseña -----------------------------------------------------------------------

export function CambiarContrasena() {
    const { navegar, notificar } = useCampo();
    const conSenal = useConSenal();
    const clave = useClaveIdempotencia();
    const [error, setError] = useState<ErrorPantalla | null>(null);
    const formulario = useForm<DatosContrasenaPropia>({ resolver: zodResolver(esquemaContrasenaPropia), defaultValues: { actual: "", nueva: "", repetir: "" }, reValidateMode: "onChange" });
    const { errors, isSubmitting, isDirty } = formulario.formState;
    const valores = useWatch({ control: formulario.control });
    const reglas = reglasContrasena({ actual: valores.actual ?? "", nueva: valores.nueva ?? "", repetir: valores.repetir ?? "" });
    const { volver, pregunta, liberar } = useSalida(isDirty && error?.tipo !== "sesion");

    async function enviar(datos: DatosContrasenaPropia) {
        setError(null);
        try {
            await resolver(clienteCuenta().POST("/api/v1/yo/contrasena", { body: cuerpoContrasenaPropia(datos), headers: cabecerasCampo(), params: { header: { "Idempotency-Key": clave.actual() } } }));
            clave.renovar();
            formulario.reset({ actual: "", nueva: "", repetir: "" });
            await liberar();
            notificar({ tono: "success", texto: "Contraseña cambiada" });
            navegar({ tipo: "yo" });
        } catch (e) {
            // Sin respuesta se reintenta con la misma clave; con respuesta (guardada), con otra.
            clave.trasError(e);
            if (e instanceof ErrorApi && e.estado === 422 && e.errores.length > 0) {
                let alguno = false;
                for (const nombre of ["actual", "nueva"] as const) {
                    const mensaje = e.errorDeCampo(nombre);
                    if (mensaje) {
                        alguno = true;
                        formulario.setError(nombre, { message: mensaje });
                    }
                }
                if (e.errorDeCampo("actual")) {
                    formulario.setValue("actual", "");
                    formulario.setFocus("actual");
                }
                if (alguno) {
                    return;
                }
            }
            setError(errorDePantalla(e, "cambiar la contraseña"));
        }
    }

    return (
        <section aria-labelledby="titulo-contrasena" className="grid gap-4">
            <Encabezado titulo="Cambiar contraseña" alVolver={volver} />
            {!conSenal ? <AvisoSinSenal /> : null}
            {error ? <AvisoError error={error} reintentar={() => void formulario.handleSubmit(enviar)()} /> : null}
            {error?.tipo === "sesion" ? null : (
                <form noValidate aria-label="Cambiar contraseña" onSubmit={formulario.handleSubmit(enviar)} className="grid gap-4">
                    <Tarjeta className="grid gap-4 p-5" data-testid="tarjeta-contrasena">
                        <CampoConIcono etiqueta="Contraseña actual" secreto autoComplete="current-password" error={errors.actual?.message} {...formulario.register("actual")} />
                        <CampoConIcono etiqueta="Nueva contraseña" secreto autoComplete="new-password" error={errors.nueva?.message} {...formulario.register("nueva")} />
                        {errors.nueva ? null : <AyudaCumplida id="ayuda-largo" texto={`${LONGITUD_MINIMA_CONTRASENA} caracteres o más`} cumplida={reglas.largo} />}
                        <CampoConIcono etiqueta="Repite la nueva" secreto autoComplete="new-password" error={errors.repetir?.message} {...formulario.register("repetir")} />
                        {errors.repetir ? null : <AyudaCumplida id="ayuda-coinciden" texto="Coinciden" cumplida={reglas.coinciden} />}
                    </Tarjeta>
                    <p className="px-1.5 text-[0.9375rem] text-muted-foreground">{TEXTO_SESIONES_CONTRASENA}</p>
                    <Boton type="submit" tamano="lg" disabled={isSubmitting || !conSenal} className="h-[3.25rem] w-full rounded-full text-base md:h-[3.25rem]">
                        {isSubmitting ? "Guardando…" : "Guardar contraseña"}
                    </Boton>
                </form>
            )}
            {pregunta}
        </section>
    );
}

// --- Verificación en dos pasos ----------------------------------------------------------------

type Configuracion = { estado: "inactiva" } | { estado: "pidiendo" } | { estado: "lista"; mfa: Schema<"MFAConfigurado"> } | { estado: "error"; error: ErrorPantalla };

export function DosPasos() {
    const { navegar } = useCampo();
    const cuenta = useCuenta();
    const volverAYo = () => navegar({ tipo: "yo" });
    if (cuenta.estado === "cargando") {
        return (
            <section className="grid gap-4">
                <Encabezado titulo="Verificación en dos pasos" alVolver={volverAYo} />
                <Cargando visible texto="Leyendo tu cuenta…" />
            </section>
        );
    }
    if (cuenta.estado === "error") {
        return (
            <section className="grid gap-4">
                <Encabezado titulo="Verificación en dos pasos" alVolver={volverAYo} />
                <AvisoError error={errorDePantalla(cuenta.error, "leer tu cuenta")} reintentar={cuenta.reintentar} />
            </section>
        );
    }
    if (cuenta.datos.usuario.mfaActivo) {
        return (
            <section className="grid gap-4">
                <Encabezado titulo="Verificación en dos pasos" alVolver={volverAYo} />
                <Aviso tono="success" icono={ShieldCheck} titulo="Activa" data-testid="dos-pasos-activa">
                    Al entrar en un equipo nuevo, Iris te pide el código de tu app de códigos.
                </Aviso>
                <p className="px-1.5 text-[0.9375rem] text-muted-foreground">{TEXTO_REINICIAR_DOS_PASOS}</p>
            </section>
        );
    }
    return <ActivarDosPasos correo={cuenta.datos.usuario.correo} />;
}

function NumeroPaso({ n }: { n: number }) {
    return <span className="grid size-7 shrink-0 place-items-center rounded-full bg-[var(--campo-panel-2)] text-sm font-bold">{n}</span>;
}

function ActivarDosPasos({ correo }: { correo: string }) {
    const { navegar, notificar } = useCampo();
    const conSenal = useConSenal();
    const borrador = useBorradorPortapapeles();
    const [config, setConfig] = useState<Configuracion>({ estado: "inactiva" });
    const [intento, setIntento] = useState(0);
    const [error, setError] = useState<ErrorPantalla | null>(null);
    const claveActivar = useClaveIdempotencia();
    const configClave = useClaveIdempotencia();
    const pedido = useRef(-1);
    const formulario = useForm<{ codigo: string }, unknown, Schema<"EntradaActivarMFA">>({ resolver: zodResolver(esquemaCodigoActivar), defaultValues: { codigo: "" } });
    const { errors, isSubmitting, isDirty } = formulario.formState;
    const { volver, pregunta, liberar } = useSalida(isDirty && error?.tipo !== "sesion");

    // Un secreto por intento: `POST /yo/mfa` no se repite con la misma clave (409), así que cada
    // intento lleva una nueva y nunca se vuelve a pedir en otro render.
    useEffect(() => {
        if (!conSenal || pedido.current === intento) {
            return undefined;
        }
        pedido.current = intento;
        let vivo = true;
        configClave.renovar();
        setConfig({ estado: "pidiendo" });
        resolver(clienteCuenta().POST("/api/v1/yo/mfa", { headers: cabecerasCampo(), params: { header: { "Idempotency-Key": configClave.actual() } } }))
            .then((mfa) => vivo && setConfig({ estado: "lista", mfa }))
            .catch((e: unknown) => vivo && setConfig({ estado: "error", error: errorDePantalla(e, "preparar la verificación") }));
        return () => {
            vivo = false;
        };
    }, [conSenal, intento, configClave]);

    async function activar(datos: Schema<"EntradaActivarMFA">) {
        setError(null);
        try {
            await resolver(clienteCuenta().POST("/api/v1/yo/mfa/activar", { body: datos, headers: cabecerasCampo(), params: { header: { "Idempotency-Key": claveActivar.actual() } } }));
        } catch (e) {
            claveActivar.trasError(e);
            // Un reintento tras una respuesta perdida: ya quedó activa.
            if (!(e instanceof ErrorApi && e.codigo === "mfa_activo")) {
                if (e instanceof ErrorApi && e.estado === 422) {
                    formulario.setError("codigo", { message: e.errorDeCampo("codigo") ?? e.detalle ?? "El código no coincide; revisa la hora del teléfono" });
                    formulario.setValue("codigo", "");
                    return;
                }
                setError(errorDePantalla(e, "activar la verificación"));
                return;
            }
        }
        claveActivar.renovar();
        formulario.reset({ codigo: "" });
        await liberar();
        notificar({ tono: "success", texto: "Verificación en dos pasos activada" });
        navegar({ tipo: "yo" });
    }

    const lista = config.estado === "lista" ? config.mfa : null;
    return (
        <section aria-labelledby="titulo-dos-pasos" className="grid gap-4">
            <Encabezado titulo="Verificación en dos pasos" alVolver={volver} />
            {!conSenal ? <AvisoSinSenal /> : null}
            {config.estado === "error" ? <AvisoError error={config.error} reintentar={() => setIntento((i) => i + 1)} /> : null}
            {error ? <AvisoError error={error} reintentar={() => void formulario.handleSubmit(activar)()} /> : null}
            {config.estado === "pidiendo" ? <Cargando visible texto="Preparando tu clave…" /> : null}
            {lista && error?.tipo !== "sesion" ? (
                <form noValidate aria-label="Activar la verificación en dos pasos" onSubmit={formulario.handleSubmit(activar)} className="grid gap-4">
                    <Tarjeta className="grid gap-6 p-4 sm:p-5" data-testid="tarjeta-dos-pasos">
                        <div className="flex gap-3">
                            <NumeroPaso n={1} />
                            <div className="grid min-w-0 flex-1 gap-2">
                                <h2 className="pt-0.5 text-[1.0625rem] font-bold">Abre tu app de códigos</h2>
                                <p className="text-[0.9375rem] text-muted-foreground">Google Authenticator, Microsoft Authenticator o similar.</p>
                                <div className="flex flex-wrap items-start gap-2">
                                    <Boton asChild variante="secundario">
                                        <a href={enlaceOtpauth(lista, correo)}>
                                            <ExternalLink aria-hidden />
                                            Abrir app
                                        </a>
                                    </Boton>
                                    <BotonCopiar texto={lista.secreto} etiqueta="Copiar clave" copiado="Clave copiada" sensible borrador={borrador} />
                                </div>
                                <p data-testid="clave-dos-pasos" aria-label={`Clave: ${agruparSecreto(lista.secreto)}`} className="font-mono text-base tracking-[0.12em] break-all text-muted-foreground select-all">
                                    {agruparSecreto(lista.secreto)}
                                </p>
                            </div>
                        </div>
                        <div className="grid gap-3">
                            <div className="flex items-center gap-3">
                                <NumeroPaso n={2} />
                                {/* El título visible del paso; el campo lleva la misma etiqueta para lectores de pantalla. */}
                                <p aria-hidden className="text-[1.0625rem] font-bold">
                                    Escribe el código de 6 dígitos
                                </p>
                            </div>
                            {/* Las 6 casillas a lo ancho de la tarjeta (44 px cada una caben a 390 px). */}
                            <div className="[&_[data-slot=etiqueta]]:sr-only">
                                <Controller
                                    control={formulario.control}
                                    name="codigo"
                                    render={({ field }) => (
                                        <CampoCodigo etiqueta="Escribe el código de 6 dígitos" valor={field.value} alCambiar={field.onChange} error={errors.codigo?.message} autoFocus={false} name="codigo" />
                                    )}
                                />
                            </div>
                        </div>
                    </Tarjeta>
                    <p className="px-1.5 text-[0.9375rem] text-muted-foreground">{TEXTO_ACTIVAR_DOS_PASOS}</p>
                    <Boton type="submit" tamano="lg" disabled={isSubmitting || !conSenal} className="h-[3.25rem] w-full rounded-full text-base md:h-[3.25rem]">
                        <CircleCheck aria-hidden />
                        {isSubmitting ? "Activando…" : "Activar"}
                    </Boton>
                </form>
            ) : null}
            {pregunta}
        </section>
    );
}
