"use client";

import { zodResolver } from "@hookform/resolvers/zod";
import type { Schema } from "@iris/contratos";
import { Aviso, Boton, Campo } from "@iris/ui";
import { Clock, KeyRound, LogIn } from "lucide-react";
import { useState, type ReactNode } from "react";
import { useForm } from "react-hook-form";
import { api, resolver } from "@/lib/api/cliente";
import { ErrorApi, mensajeDeError } from "@/lib/api/errores";
import { cuerpoCambiarContrasena, esquemaCambiarContrasena, LONGITUD_MINIMA_CONTRASENA, type DatosCambiarContrasena } from "@/lib/cuenta/esquemas";
import { erroresACampos } from "@/lib/formularios";
import { useClaveIdempotencia } from "@/lib/idempotencia";
import { formatearEspera } from "@/lib/tiempo";

type Usuario = Schema<"Usuario">;

export type PropsFormularioContrasena = {
    /** 200: la API devolvió el usuario (ya sin `cambioContrasenaRequerido`). */
    alCambiar: (usuario: Usuario) => void;
    /** 429 `demasiados_intentos`: la API ya cerró la sesión. */
    alBloquear: (segundos: number) => void;
    /** 401: la sesión ya no existe. */
    alExpirar: () => void;
    /** Botones junto a «Cambiar contraseña» (p. ej. «Cerrar sesión» o «Cancelar»). */
    acciones?: ReactNode;
    /** Texto de la contraseña actual («Contraseña temporal» si viene de un reinicio). */
    etiquetaActual?: string;
};

/**
 * `POST /api/v1/yo/contrasena { actual, nueva }` con la validación del backend (12+ y distinta de
 * la actual) y confirmación. Un 422 con `errors[]` va a sus campos; el resto, a un aviso.
 */
export function FormularioContrasena({ alCambiar, alBloquear, alExpirar, acciones, etiquetaActual = "Contraseña actual" }: PropsFormularioContrasena) {
    const clave = useClaveIdempotencia();
    const [error, setError] = useState<unknown>(null);
    const formulario = useForm<DatosCambiarContrasena>({
        resolver: zodResolver(esquemaCambiarContrasena),
        defaultValues: { actual: "", nueva: "", confirmar: "" },
    });
    const { errors, isSubmitting } = formulario.formState;

    async function enviar(datos: DatosCambiarContrasena) {
        setError(null);
        try {
            const usuario = await resolver(
                api().POST("/api/v1/yo/contrasena", {
                    body: cuerpoCambiarContrasena(datos),
                    params: { header: { "Idempotency-Key": clave.actual() } },
                }),
            );
            clave.renovar();
            formulario.reset({ actual: "", nueva: "", confirmar: "" });
            alCambiar(usuario);
        } catch (e) {
            clave.trasError(e);
            if (e instanceof ErrorApi && e.estado === 429) {
                alBloquear(e.reintentarEn ?? 15 * 60);
                return;
            }
            if (e instanceof ErrorApi && e.estado === 401) {
                alExpirar();
                return;
            }
            if (erroresACampos(e, ["actual", "nueva"], formulario.setError)) {
                if (e instanceof ErrorApi && e.errorDeCampo("actual")) {
                    formulario.setValue("actual", "");
                    formulario.setFocus("actual");
                }
                return;
            }
            setError(e);
        }
    }

    return (
        <form noValidate onSubmit={formulario.handleSubmit(enviar)} className="grid gap-4" aria-label="Cambiar contraseña">
            {error ? (
                <Aviso tono="error" titulo="No se pudo cambiar la contraseña">
                    {mensajeDeError(error).detalle ?? mensajeDeError(error).titulo}
                </Aviso>
            ) : null}
            <Campo
                etiqueta={etiquetaActual}
                type="password"
                autoComplete="current-password"
                autoFocus
                error={errors.actual?.message}
                {...formulario.register("actual")}
            />
            <Campo
                etiqueta="Contraseña nueva"
                type="password"
                autoComplete="new-password"
                ayuda={`Al menos ${LONGITUD_MINIMA_CONTRASENA} caracteres y distinta de la actual. Una frase larga es más fácil de recordar.`}
                error={errors.nueva?.message}
                {...formulario.register("nueva")}
            />
            <Campo
                etiqueta="Repite la contraseña nueva"
                type="password"
                autoComplete="new-password"
                error={errors.confirmar?.message}
                {...formulario.register("confirmar")}
            />
            <div className="flex flex-col-reverse gap-2 sm:flex-row sm:justify-end">
                {acciones}
                <Boton type="submit" disabled={isSubmitting}>
                    <KeyRound />
                    {isSubmitting ? "Cambiando…" : "Cambiar contraseña"}
                </Boton>
            </div>
        </form>
    );
}

/**
 * Tras el 429 la API ya cerró la sesión y bloquea el cambio 15 min: se explica y se vuelve a
 * entrar (no hay nada más que hacer con esta sesión).
 */
export function BloqueoContrasena({ segundos, siguiente }: { segundos: number; siguiente?: string }) {
    const destino = siguiente && siguiente !== "/" ? `/entrar?${new URLSearchParams({ siguiente })}` : "/entrar";
    return (
        <div className="grid gap-4">
            <Aviso tono="warning" icono={Clock} titulo={`Demasiados intentos; vuelve a entrar en ${formatearEspera(segundos)}`}>
                Escribiste mal tu contraseña actual demasiadas veces. Por seguridad cerramos tu sesión y bloqueamos el cambio de
                contraseña un momento.
            </Aviso>
            <Boton asChild className="justify-self-start">
                {/* Navegación completa: la sesión ya no existe y el panel debe empezar de cero. */}
                <a href={destino}>
                    <LogIn />
                    Ir a iniciar sesión
                </a>
            </Boton>
        </div>
    );
}
