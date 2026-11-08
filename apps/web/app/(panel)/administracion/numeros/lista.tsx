"use client";

import type { Schema } from "@iris/contratos";
import { avisar, Aviso, Boton, cn, Esqueleto, EstadoVacio, Tarjeta } from "@iris/ui";
import { BookOpen, CalendarClock, KeyRound, MessageCircle, Plus, Power, SlidersHorizontal } from "lucide-react";
import Link from "next/link";
import { useState, type ReactNode } from "react";
import { EncabezadoPagina } from "@/componentes/encabezado-pagina";
import { ErrorLectura } from "@/componentes/error-lectura";
import { EstadoConexion } from "@/componentes/numeros/estado-conexion";
import { api, resolver } from "@/lib/api/cliente";
import { mensajeDeError } from "@/lib/api/errores";
import { avisoEstado } from "@/lib/avisos";
import { CLASE_PILDORA, formatearNumero, nombreUso, tonoDeCanal } from "@/lib/conversaciones/presentacion";
import { resumirFranjas } from "@/lib/horarios/franjas";
import { useClaveIdempotencia } from "@/lib/idempotencia";
import { useLectura } from "@/lib/lectura";
import { activarCanal, leerCanales } from "@/lib/numeros/api";
import { presentarConexion } from "@/lib/numeros/conexion";
import { enlaceGuia } from "@/lib/numeros/guia";
import type { Conexion } from "@/lib/numeros/tipos";
import { useSesion } from "@/lib/sesion";

/** Lo que muestra una fila; `conexion` solo con `mensajeria.configurar` (viene de `GET /canales`). */
type Fila = Pick<Schema<"NumeroConversaciones">, "id" | "nombre" | "numero" | "usos" | "avisosEmpresa" | "colaId" | "horarioId" | "activo"> & { conexion: Conexion | null };

const AREAS = cn(
    "grid grid-cols-[minmax(0,1fr)_auto] gap-x-4 gap-y-3 [grid-template-areas:'num_est''uso_uso''det_det''acc_acc']",
    "lg:grid-cols-[minmax(11rem,1.3fr)_minmax(6rem,0.8fr)_minmax(5rem,0.6fr)_minmax(8rem,1fr)_minmax(8rem,0.9fr)_auto] lg:items-center lg:gap-y-0",
    "lg:[grid-template-areas:'num_uso_cola_hor_est_acc']",
);
const AREAS_SIN_CONEXION = cn(
    "grid grid-cols-[minmax(0,1fr)_auto] gap-x-4 gap-y-3 [grid-template-areas:'num_num''uso_uso''det_det''acc_acc']",
    "lg:grid-cols-[minmax(11rem,1.3fr)_minmax(6rem,0.8fr)_minmax(5rem,0.6fr)_minmax(8rem,1fr)_auto] lg:items-center lg:gap-y-0",
    "lg:[grid-template-areas:'num_uso_cola_hor_acc']",
);

/**
 * «Números de WhatsApp» (Administración): cada número con su uso, cola, horario y, con
 * `mensajeria.configurar`, su conexión con Meta (calculada en el servidor) y sus credenciales.
 * «Configurar» (uso, cola y horario) es de `canales.configurar`.
 */
export function ListaNumeros() {
    const { yo, puede } = useSesion();
    const credenciales = puede(["mensajeria.configurar"]);
    const configurar = puede(["canales.configurar"]);
    const clave = useClaveIdempotencia();
    const [errorAccion, setErrorAccion] = useState<{ nombre: string; error: unknown } | null>(null);
    const [activando, setActivando] = useState<number | null>(null);
    const lectura = useLectura(async (signal) => {
        const [numeros, colas, horarios] = await Promise.all([
            credenciales
                ? leerCanales(signal).then((cs) => cs.map((c): Fila => ({ ...c, conexion: c.conexion })))
                : resolver(api().GET("/api/v1/mensajeria/conversaciones/canales", { signal })).then((r) => (r.data ?? []).map((c): Fila => ({ ...c, conexion: null }))),
            resolver(api().GET("/api/v1/mensajeria/colas", { signal })),
            resolver(api().GET("/api/v1/mensajeria/horarios", { signal })),
        ]);
        return { numeros, colas: colas.data ?? [], horarios: horarios.data ?? [] };
    });

    async function activar(f: Fila) {
        setErrorAccion(null);
        setActivando(f.id);
        try {
            await activarCanal(f.id, clave.actual());
            clave.renovar();
            avisar({ titulo: avisoEstado(f.nombre, true) });
            lectura.reintentar();
        } catch (e) {
            clave.trasError(e);
            setErrorAccion({ nombre: f.nombre, error: e });
        } finally {
            setActivando(null);
        }
    }

    const agregar = credenciales ? (
        <Boton asChild>
            <Link href="/administracion/numeros/nuevo">
                <Plus aria-hidden />
                Agregar número
            </Link>
        </Boton>
    ) : null;

    let contenido: ReactNode;
    let descripcion = "Números conectados por la API oficial de Meta (WhatsApp Cloud API)";
    if (lectura.estado === "cargando") {
        contenido = (
            <div className="grid gap-2" aria-busy="true">
                <span className="sr-only" role="status">
                    Cargando números…
                </span>
                <Esqueleto className="h-20" />
                <Esqueleto className="h-20" />
            </div>
        );
    } else if (lectura.estado === "error") {
        contenido = <ErrorLectura error={lectura.error} reintentar={lectura.reintentar} que="los números" />;
    } else if (lectura.datos.numeros.length === 0) {
        descripcion = "Conecta un número para recibir y contestar WhatsApp en la bandeja";
        contenido = (
            <Tarjeta>
                <EstadoVacio
                    icono={MessageCircle}
                    titulo="Todavía no hay números de WhatsApp"
                    descripcion="Necesitas una cuenta de Meta Business con WhatsApp Cloud API y un número que no esté en la app de WhatsApp."
                    accion={
                        <div className="flex flex-wrap justify-center gap-2">
                            <Boton asChild variante="contorno">
                                <Link href={enlaceGuia()}>
                                    <BookOpen aria-hidden />
                                    Ver la guía
                                </Link>
                            </Boton>
                            {agregar}
                        </div>
                    }
                />
            </Tarjeta>
        );
    } else {
        const { numeros, colas, horarios } = lectura.datos;
        const activos = numeros.filter((n) => n.activo).length;
        const inactivos = numeros.length - activos;
        descripcion = `${yo.empresaActiva?.nombre ? `${yo.empresaActiva.nombre} · ` : ""}${activos} ${activos === 1 ? "activo" : "activos"}${inactivos ? ` y ${inactivos} ${inactivos === 1 ? "desactivado" : "desactivados"}` : ""}, por la API oficial de Meta (WhatsApp Cloud API)`;
        const conConexion = credenciales;
        contenido = (
            <div className="grid gap-3">
                {errorAccion ? (
                    <Aviso tono="error" titulo={`No se pudo activar «${errorAccion.nombre}»`}>
                        {mensajeDeError(errorAccion.error).titulo}
                    </Aviso>
                ) : null}
                <div className="overflow-hidden lg:rounded-xl lg:border lg:border-border lg:bg-card">
                    <div
                        aria-hidden
                        className={cn(
                            conConexion ? AREAS : AREAS_SIN_CONEXION,
                            // Después de las áreas: «hidden» gana al «grid» de las áreas en el teléfono.
                            "hidden border-b border-border px-4 py-2.5 text-[0.6875rem] font-semibold tracking-wide text-muted-foreground uppercase lg:grid",
                        )}
                    >
                        <span className="[grid-area:num]">Número</span>
                        <span className="[grid-area:uso]">Uso</span>
                        <span className="[grid-area:cola]">Cola</span>
                        <span className="[grid-area:hor]">Horario</span>
                        {conConexion ? <span className="[grid-area:est]">Conexión</span> : null}
                        <span className="text-right [grid-area:acc]">Acciones</span>
                    </div>
                    <ul aria-label="Números de WhatsApp" className="grid gap-3 lg:gap-0">
                        {numeros.map((n) => {
                            const horario = horarios.find((h) => h.id === n.horarioId);
                            const cola = colas.find((x) => x.id === n.colaId);
                            const pide = n.conexion ? presentarConexion(n.conexion).credenciales : false;
                            return (
                                <li
                                    key={n.id}
                                    data-numero={n.id}
                                    className={cn(
                                        "rounded-xl border border-border bg-card p-4 lg:rounded-none lg:border-0 lg:border-b lg:px-4 lg:py-3 lg:last:border-b-0",
                                        conConexion ? AREAS : AREAS_SIN_CONEXION,
                                    )}
                                >
                                    <div className={cn("grid min-w-0 gap-0.5 [grid-area:num]", !n.activo && "text-muted-foreground")}>
                                        <span className="truncate font-semibold">{n.nombre}</span>
                                        {n.avisosEmpresa ? <span className="hidden h-5 w-fit items-center rounded-full bg-info/10 px-2 text-[0.6875rem] font-semibold whitespace-nowrap text-info lg:inline-flex">Avisos de la empresa</span> : null}
                                        <span className="truncate font-mono text-sm text-muted-foreground">{formatearNumero(n.numero)}</span>
                                    </div>
                                    <div className="flex flex-wrap content-start gap-1 [grid-area:uso]">
                                        {n.avisosEmpresa ? <span className="inline-flex h-5 items-center rounded-full bg-info/10 px-2 text-[0.6875rem] font-semibold whitespace-nowrap text-info lg:hidden">Avisos de la empresa</span> : null}
                                        {(n.usos ?? []).map((u) => (
                                            <span key={u} className={cn("inline-flex h-5 items-center rounded-full px-2 text-[0.6875rem] font-semibold", CLASE_PILDORA[tonoDeCanal({ usos: [u] })])}>
                                                {nombreUso(u)}
                                            </span>
                                        ))}
                                    </div>
                                    <div className={cn("min-w-0 flex-wrap gap-x-4 gap-y-1 text-sm text-muted-foreground [grid-area:det] lg:contents lg:text-foreground", n.activo ? "flex" : "hidden")}>
                                        <span className="min-w-0 [grid-area:cola]">
                                            <span className="lg:hidden">Cola: </span>
                                            {n.activo ? (cola?.nombre ?? <span className="text-muted-foreground">Sin cola</span>) : <Nada />}
                                        </span>
                                        <span className="min-w-0 [grid-area:hor]">
                                            {!n.activo ? <Nada /> : horario ? resumirFranjas(horario.franjas) : <span className="text-muted-foreground">Siempre abierto</span>}
                                        </span>
                                    </div>
                                    {n.conexion ? (
                                        <div className="min-w-0 justify-self-end text-right [grid-area:est] lg:justify-self-start lg:text-left">
                                            <EstadoConexion conexion={n.conexion} />
                                        </div>
                                    ) : null}
                                    <div className="grid grid-cols-2 gap-2 [grid-area:acc] lg:flex lg:justify-end [&>*]:w-full lg:[&>*]:w-auto">
                                        {configurar && n.activo ? (
                                            <Boton asChild variante="contorno" tamano="sm">
                                                <Link href={`/administracion/numeros/${n.id}/configurar`} aria-label={`Configurar ${n.nombre}`}>
                                                    <SlidersHorizontal aria-hidden />
                                                    Configurar
                                                </Link>
                                            </Boton>
                                        ) : null}
                                        {credenciales && !n.activo ? (
                                            <Boton variante="contorno" tamano="sm" aria-label={`Activar ${n.nombre}`} disabled={activando === n.id} onClick={() => void activar(n)}>
                                                <Power aria-hidden />
                                                {activando === n.id ? "Activando…" : "Activar"}
                                            </Boton>
                                        ) : null}
                                        {credenciales ? (
                                            <Boton asChild variante="contorno" tamano="sm" className={cn(pide && "border-error/60 text-error")}>
                                                <Link href={`/administracion/numeros/${n.id}/credenciales`} aria-label={`Credenciales de ${n.nombre}`}>
                                                    <KeyRound aria-hidden />
                                                    Credenciales
                                                </Link>
                                            </Boton>
                                        ) : null}
                                    </div>
                                </li>
                            );
                        })}
                    </ul>
                </div>
            </div>
        );
    }

    const hayNumeros = lectura.estado === "listo" && lectura.datos.numeros.length > 0;
    return (
        <>
            <EncabezadoPagina
                titulo="Números de WhatsApp"
                descripcion={descripcion}
                acciones={
                    <div className="flex w-full flex-wrap gap-2 sm:w-auto [&>*]:flex-1 sm:[&>*]:flex-none">
                        {configurar ? (
                            <Boton asChild variante="secundario">
                                <Link href="/administracion/horarios">
                                    <CalendarClock aria-hidden />
                                    Horarios
                                </Link>
                            </Boton>
                        ) : null}
                        {hayNumeros ? agregar : null}
                    </div>
                }
            />
            {contenido}
        </>
    );
}

function Nada() {
    return (
        <span className="text-muted-foreground">
            <span aria-hidden>—</span>
            <span className="sr-only">Sin dato</span>
        </span>
    );
}
