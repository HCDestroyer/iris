// Inicio de sesión: el mismo flujo que el panel y la PWA (correo y contraseña → código TOTP si la
// cuenta tiene MFA), con bloqueo por 429 con cuenta regresiva y los mismos mensajes.
import { zodResolver } from "@hookform/resolvers/zod";
import type { Schema } from "@iris/contratos";
import { ArrowLeft, Clock, LogIn, ShieldCheck } from "lucide-react-native";
import { useEffect, useState } from "react";
import { Controller, useForm } from "react-hook-form";
import { ErrorApi, formatearEspera } from "../api/errores";
import { Aviso, Boton, Campo, Pila, Texto, Titulo } from "../componentes";
import { esquemaCodigoMFA, esquemaInicioSesion, type DatosCodigoMFA, type DatosInicioSesion } from "../sesion/esquemas";
import { iniciarSesion, verificarMfa } from "../sesion/sesion";
import { useTema } from "../tema/tema";

type AvisoPantalla = { tono: "error" | "warning" | "info"; titulo: string; detalle?: string | undefined };

export function avisoDeError(error: unknown): AvisoPantalla {
    if (error instanceof ErrorApi) {
        switch (error.codigo) {
            case "credenciales_invalidas":
                return { tono: "error", titulo: "Correo o contraseña incorrectos", detalle: "Revisa los datos e inténtalo de nuevo." };
            case "mfa_bloqueado":
                return { tono: "error", titulo: "Demasiados códigos inválidos", detalle: "Por seguridad cerramos el intento. Vuelve a iniciar sesión." };
            case "no_autenticado":
                return { tono: "warning", titulo: "El inicio de sesión venció", detalle: "Vuelve a escribir tu correo y contraseña." };
            case "sin_conexion":
                return { tono: "error", titulo: "No pudimos contactar a Iris", detalle: "Revisa tu conexión e inténtalo de nuevo." };
        }
        return { tono: "error", titulo: error.titulo, detalle: error.detalle };
    }
    return { tono: "error", titulo: "Algo salió mal", detalle: "Inténtalo de nuevo." };
}

function useBloqueo() {
    const [hasta, setHasta] = useState<number | null>(null);
    const [ahora, setAhora] = useState(() => Date.now());
    useEffect(() => {
        if (hasta === null) {
            return undefined;
        }
        const id = setInterval(() => {
            const t = Date.now();
            setAhora(t);
            if (t >= hasta) {
                setHasta(null);
            }
        }, 1000);
        return () => clearInterval(id);
    }, [hasta]);
    const restante = hasta === null ? 0 : Math.max(0, Math.ceil((hasta - ahora) / 1000));
    return {
        restante,
        bloquear: (segundos: number) => {
            const t = Date.now();
            setAhora(t);
            setHasta(t + segundos * 1000);
        },
        liberar: () => setHasta(null),
    };
}

/** `alEntrar` recibe el usuario de la sesión completa (con MFA, tras el código). */
export function Entrar({ alEntrar, motivo }: { alEntrar: (usuario: Schema<"Usuario"> | undefined) => void; motivo?: "expirada" | "salida" }) {
    const { colores } = useTema();
    const [paso, setPaso] = useState<"credenciales" | "mfa">("credenciales");
    const [aviso, setAviso] = useState<AvisoPantalla | null>(
        motivo === "expirada" ? { tono: "info", titulo: "Tu sesión terminó", detalle: "Vuelve a entrar para continuar." } : null,
    );
    const bloqueo = useBloqueo();
    const credenciales = useForm<DatosInicioSesion>({ resolver: zodResolver(esquemaInicioSesion), defaultValues: { correo: "", contrasena: "" } });
    const mfa = useForm<{ codigo: string }, unknown, DatosCodigoMFA>({ resolver: zodResolver(esquemaCodigoMFA), defaultValues: { codigo: "" } });

    const enviarCredenciales = credenciales.handleSubmit(async (datos) => {
        setAviso(null);
        try {
            const sesion = await iniciarSesion(datos);
            bloqueo.liberar();
            if (sesion.mfaRequerido) {
                mfa.reset({ codigo: "" });
                setPaso("mfa");
                return;
            }
            alEntrar(sesion.usuario);
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
    });

    const enviarCodigo = mfa.handleSubmit(async (datos) => {
        setAviso(null);
        try {
            const sesion = await verificarMfa(datos);
            alEntrar(sesion.usuario);
        } catch (error) {
            if (error instanceof ErrorApi && error.codigo === "codigo_invalido") {
                mfa.setError("codigo", { message: "Código incorrecto o ya usado. Revisa tu app de autenticación." });
                mfa.setValue("codigo", "");
                return;
            }
            if (error instanceof ErrorApi && (error.codigo === "mfa_bloqueado" || error.codigo === "no_autenticado")) {
                credenciales.setValue("contrasena", "");
                setPaso("credenciales");
            }
            setAviso(avisoDeError(error));
        }
    });

    const avisoVisible = aviso ? (
        <Aviso tono={aviso.tono} titulo={aviso.titulo} testID="aviso-entrar">
            {aviso.detalle}
        </Aviso>
    ) : null;

    if (paso === "mfa") {
        const { errors, isSubmitting } = mfa.formState;
        return (
            <Pila espacio={16}>
                <ShieldCheck size={28} color={colores.primario} />
                <Titulo>Verificación en dos pasos</Titulo>
                <Texto suave>Escribe el código de 6 dígitos de tu app de autenticación.</Texto>
                {avisoVisible}
                <Controller
                    control={mfa.control}
                    name="codigo"
                    render={({ field }) => (
                        <Campo
                            etiqueta="Código de verificación"
                            valor={field.value}
                            alCambiar={field.onChange}
                            onBlur={field.onBlur}
                            keyboardType="number-pad"
                            autoComplete="one-time-code"
                            textContentType="oneTimeCode"
                            autoFocus
                            maxLength={7}
                            error={errors.codigo?.message}
                        />
                    )}
                />
                <Boton icono={ShieldCheck} cargando={isSubmitting} alPresionar={() => void enviarCodigo()}>
                    {isSubmitting ? "Verificando…" : "Verificar"}
                </Boton>
                <Boton
                    variante="fantasma"
                    icono={ArrowLeft}
                    alPresionar={() => {
                        setAviso(null);
                        setPaso("credenciales");
                    }}
                >
                    Volver
                </Boton>
            </Pila>
        );
    }

    const { errors, isSubmitting } = credenciales.formState;
    const bloqueado = bloqueo.restante > 0;
    return (
        <Pila espacio={16}>
            <Titulo>Iniciar sesión</Titulo>
            <Texto suave>Entra con tu cuenta de Iris.</Texto>
            {bloqueado ? (
                <Aviso tono="warning" icono={Clock} titulo={`Demasiados intentos, espera ${formatearEspera(bloqueo.restante)}`} testID="aviso-bloqueo">
                    Por seguridad bloqueamos este correo un momento. Podrás volver a intentar cuando termine la espera.
                </Aviso>
            ) : (
                avisoVisible
            )}
            <Controller
                control={credenciales.control}
                name="correo"
                render={({ field }) => (
                    <Campo
                        etiqueta="Correo"
                        valor={field.value}
                        alCambiar={field.onChange}
                        onBlur={field.onBlur}
                        keyboardType="email-address"
                        autoCapitalize="none"
                        autoComplete="username"
                        textContentType="username"
                        error={errors.correo?.message}
                    />
                )}
            />
            <Controller
                control={credenciales.control}
                name="contrasena"
                render={({ field }) => (
                    <Campo
                        etiqueta="Contraseña"
                        valor={field.value}
                        alCambiar={field.onChange}
                        onBlur={field.onBlur}
                        secureTextEntry
                        autoCapitalize="none"
                        autoComplete="current-password"
                        textContentType="password"
                        error={errors.contrasena?.message}
                    />
                )}
            />
            <Boton icono={LogIn} cargando={isSubmitting} deshabilitado={bloqueado} alPresionar={() => void enviarCredenciales()}>
                {isSubmitting ? "Entrando…" : "Entrar"}
            </Boton>
        </Pila>
    );
}
