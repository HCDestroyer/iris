"use client";

import type { Schema } from "@iris/contratos";
import { Aviso, Boton, Esqueleto } from "@iris/ui";
import { ArrowRight, CircleCheck, KeyRound, LogOut, ShieldCheck, type LucideIcon } from "lucide-react";
import Link from "next/link";
import { useRouter, useSearchParams } from "next/navigation";
import { useEffect, useState, type ReactNode } from "react";
import { BloqueoContrasena, FormularioContrasena } from "@/componentes/cuenta/formulario-contrasena";
import { ErrorLectura } from "@/componentes/error-lectura";
import { api, resolver } from "@/lib/api/cliente";
import { ErrorApi, mensajeDeError } from "@/lib/api/errores";
import { esRutaCampo, requiereCambio, rutaConfigurarMfa, siguienteSeguro } from "@/lib/cuenta/sesion";
import { useLectura } from "@/lib/lectura";
import { rutaEntrar } from "@/lib/sesion";

type Usuario = Schema<"Usuario">;
type Etapa = { tipo: "formulario" } | { tipo: "listo"; usuario: Usuario } | { tipo: "bloqueado"; segundos: number };

/**
 * Cambio de contraseña con la sesión del panel: obligatorio con una contraseña temporal (solo
 * deja cambiarla o salir) y también voluntario si se entra directo. Al terminar vuelve a
 * `siguiente` y, si el usuario no tiene MFA, se lo ofrece.
 */
export function CambioContrasena() {
    const router = useRouter();
    const params = useSearchParams();
    const siguiente = siguienteSeguro(params.get("siguiente"));
    const yo = useLectura((signal) => resolver(api().GET("/api/v1/yo", { signal })));
    const [etapa, setEtapa] = useState<Etapa>({ tipo: "formulario" });
    const [saliendo, setSaliendo] = useState(false);
    const [errorSalir, setErrorSalir] = useState<unknown>(null);
    const sinSesion = yo.estado === "error" && yo.error instanceof ErrorApi && yo.error.estado === 401;

    useEffect(() => {
        if (sinSesion) {
            router.replace(rutaEntrar(siguiente, "expirada"));
        }
    }, [sinSesion, router, siguiente]);

    function continuar() {
        if (esRutaCampo(siguiente)) {
            window.location.assign(siguiente);
            return;
        }
        router.replace(siguiente);
        router.refresh();
    }

    function expirada() {
        router.replace(rutaEntrar(siguiente, "expirada"));
    }

    async function salir() {
        setSaliendo(true);
        setErrorSalir(null);
        try {
            await resolver(api().DELETE("/api/v1/sesion"));
        } catch (error) {
            if (!(error instanceof ErrorApi && error.estado === 401)) {
                setErrorSalir(error);
                setSaliendo(false);
                return;
            }
        }
        window.location.assign("/entrar");
    }

    if (etapa.tipo === "bloqueado") {
        return (
            <div className="grid gap-5">
                <Titulo icono={KeyRound} titulo="Cambia tu contraseña" />
                <BloqueoContrasena segundos={etapa.segundos} siguiente={siguiente} />
            </div>
        );
    }

    if (etapa.tipo === "listo") {
        const sinMfa = !etapa.usuario.mfaActivo;
        return (
            <div className="grid gap-5">
                <Titulo icono={CircleCheck} titulo="Contraseña cambiada" tono="success">
                    Desde ahora entra con tu contraseña nueva. Cerramos tus otras sesiones abiertas.
                </Titulo>
                {sinMfa ? (
                    <>
                        <Aviso tono="info" icono={ShieldCheck} titulo="Protege tu cuenta con verificación en dos pasos">
                            Además de la contraseña, Iris te pedirá un código de 6 dígitos de una app de autenticación del teléfono.
                            Toma un par de minutos.
                        </Aviso>
                        <div className="flex flex-col-reverse gap-2 sm:flex-row sm:justify-end">
                            <Boton variante="fantasma" onClick={continuar}>
                                Ahora no
                            </Boton>
                            <Boton asChild>
                                <Link href={rutaConfigurarMfa(siguiente)}>
                                    <ShieldCheck />
                                    Configurar ahora
                                </Link>
                            </Boton>
                        </div>
                    </>
                ) : (
                    <Boton onClick={continuar} className="justify-self-end">
                        Continuar
                        <ArrowRight />
                    </Boton>
                )}
            </div>
        );
    }

    if (yo.estado === "error" && !sinSesion) {
        return <ErrorLectura error={yo.error} reintentar={yo.reintentar} que="tu cuenta" />;
    }
    if (yo.estado !== "listo") {
        return (
            <div className="grid gap-4" aria-busy="true">
                <span className="sr-only" role="status">
                    Cargando tu cuenta…
                </span>
                <Esqueleto className="h-8 w-48" />
                <Esqueleto className="h-10" />
                <Esqueleto className="h-10" />
                <Esqueleto className="h-10" />
            </div>
        );
    }

    const temporal = requiereCambio(yo.datos.usuario);
    const nombre = yo.datos.usuario.nombre.split(" ")[0] ?? yo.datos.usuario.nombre;
    return (
        <div className="grid gap-5">
            {temporal ? (
                <Titulo icono={KeyRound} titulo="Cambia tu contraseña">
                    Hola, {nombre}. Entraste con una contraseña temporal que te dio un administrador. Antes de continuar, elige una
                    contraseña nueva que solo tú conozcas.
                </Titulo>
            ) : (
                <Titulo icono={KeyRound} titulo="Cambiar contraseña">
                    Escribe tu contraseña actual y la nueva. Cerraremos tus otras sesiones abiertas.
                </Titulo>
            )}
            {errorSalir ? (
                <Aviso tono="error" titulo="No se pudo cerrar la sesión">
                    {mensajeDeError(errorSalir).detalle}
                </Aviso>
            ) : null}
            <FormularioContrasena
                etiquetaActual={temporal ? "Contraseña temporal" : "Contraseña actual"}
                alCambiar={(usuario) => setEtapa({ tipo: "listo", usuario })}
                alBloquear={(segundos) => setEtapa({ tipo: "bloqueado", segundos })}
                alExpirar={expirada}
                acciones={
                    temporal ? (
                        <Boton variante="fantasma" onClick={salir} disabled={saliendo}>
                            <LogOut />
                            {saliendo ? "Cerrando sesión…" : "Cerrar sesión"}
                        </Boton>
                    ) : (
                        <Boton variante="fantasma" onClick={continuar}>
                            Cancelar
                        </Boton>
                    )
                }
            />
        </div>
    );
}

function Titulo({
    icono: Icono,
    titulo,
    tono,
    children,
}: {
    icono: LucideIcon;
    titulo: string;
    tono?: "success";
    children?: ReactNode;
}) {
    return (
        <div className="grid gap-1.5">
            <Icono aria-hidden className={tono === "success" ? "size-6 text-success" : "size-6 text-primary"} />
            <h1 className="text-xl font-semibold tracking-[-0.015em]">{titulo}</h1>
            {children ? <p className="text-sm text-muted-foreground">{children}</p> : null}
        </div>
    );
}
