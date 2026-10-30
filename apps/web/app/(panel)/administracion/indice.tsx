"use client";

import { InsigniaEstado, Tarjeta, TarjetaContenido, TarjetaDescripcion, TarjetaEncabezado, TarjetaTitulo } from "@iris/ui";
import { ChevronRight, ClipboardList, DatabaseZap, Hourglass, KeyRound, Mail, Map as IconoMapa, Plug, ShieldCheck, Users, type LucideIcon } from "lucide-react";
import Link from "next/link";
import { EncabezadoPagina } from "@/componentes/encabezado-pagina";
import { PERMISOS_CUADRILLAS, PERMISOS_ZONAS } from "@/lib/agenda/permisos";
import type { Permiso } from "@/lib/menu";
import { useSesion } from "@/lib/sesion";

type Seccion = { href?: string; titulo: string; descripcion: string; icono: LucideIcon; permisos: readonly Permiso[] };

const SECCIONES: readonly Seccion[] = [
    {
        href: "/administracion/buzones",
        titulo: "Buzones de correo",
        descripcion: "Correos que abren tickets y servidor de salida de las respuestas.",
        icono: Mail,
        permisos: ["buzones.ver"],
    },
    {
        href: "/administracion/plantillas",
        titulo: "Plantillas de formulario",
        descripcion: "Checklists de las órdenes: ítems, versiones y cuáles se ofrecen por tipo.",
        icono: ClipboardList,
        permisos: ["plantillas.ver"],
    },
    {
        href: "/administracion/cuadrillas",
        titulo: "Cuadrillas",
        descripcion: "Equipos de técnicos y las zonas que atienden.",
        icono: Users,
        permisos: PERMISOS_CUADRILLAS,
    },
    {
        href: "/administracion/zonas",
        titulo: "Zonas",
        descripcion: "Zonas de Argos y locales: crear, renombrar, activar y desactivar.",
        icono: IconoMapa,
        permisos: PERMISOS_ZONAS,
    },
    {
        href: "/administracion/integraciones",
        titulo: "Integraciones",
        descripcion: "Conectores con Argos y Horus Flow: conectar, activar y desactivar.",
        icono: Plug,
        permisos: ["integraciones.ver"],
    },
    {
        href: "/administracion/datos-sin-empresa",
        titulo: "Datos sin empresa",
        descripcion: "Asigna a una empresa lo guardado antes de conectar Argos.",
        icono: DatabaseZap,
        permisos: ["empresas.adoptar"],
    },
    { titulo: "Tokens de API", descripcion: "Accesos para integraciones y scripts.", icono: KeyRound, permisos: ["tokens.ver"] },
    { titulo: "Roles y permisos", descripcion: "Qué puede hacer cada rol y con qué alcance.", icono: ShieldCheck, permisos: ["roles.ver", "permisos.ver"] },
];

/** Índice de Administración: solo las secciones que el usuario puede ver. */
export function IndiceAdministracion() {
    const { puede } = useSesion();
    const visibles = SECCIONES.filter((s) => puede(s.permisos));
    return (
        <>
            <EncabezadoPagina titulo="Administración" />
            <ul className="grid gap-3 sm:grid-cols-2" aria-label="Secciones de administración">
                {visibles.map((s) => {
                    const cuerpo = (
                        <Tarjeta className={s.href ? "h-full transition-colors hover:bg-elevated" : "h-full"}>
                            <TarjetaEncabezado className="flex flex-row items-start gap-3">
                                <s.icono aria-hidden className="mt-0.5 size-5 shrink-0 text-muted-foreground" />
                                <div className="grid min-w-0 flex-1 gap-1">
                                    <TarjetaTitulo>{s.titulo}</TarjetaTitulo>
                                    <TarjetaDescripcion>{s.descripcion}</TarjetaDescripcion>
                                </div>
                                {s.href ? <ChevronRight aria-hidden className="size-5 shrink-0 text-muted-foreground" /> : null}
                            </TarjetaEncabezado>
                            {s.href ? null : (
                                <TarjetaContenido>
                                    <InsigniaEstado tono="neutral" icono={Hourglass} texto="Próximamente" />
                                </TarjetaContenido>
                            )}
                        </Tarjeta>
                    );
                    return (
                        <li key={s.titulo}>
                            {s.href ? (
                                <Link href={s.href} className="block h-full rounded-[var(--hx-radius-card)]">
                                    {cuerpo}
                                </Link>
                            ) : (
                                cuerpo
                            )}
                        </li>
                    );
                })}
            </ul>
        </>
    );
}
