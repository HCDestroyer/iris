"use client";

import { BookOpen, ChevronDown, LogOut, type LucideIcon } from "lucide-react";
import { useState, type ReactNode } from "react";
import { cn } from "../utilidades";
import { AvatarUsuario } from "./avatar-usuario";
import { CLASE_ISLA_ENCABEZADO } from "./encabezado-panel";
import { Boton } from "./boton";
import { MenuDesplegableElemento, MenuDesplegableSeparador, MenuEnLinea } from "./menu-desplegable";
import { PanelEmergente } from "./panel-emergente";

export type OpcionCuenta = {
    id: string;
    texto: string;
    icono?: LucideIcon;
    deshabilitada?: boolean;
    alElegir: () => void;
};

export type PropsMenuCuenta = {
    nombre: string;
    correo?: string;
    /** «Supervisora». */
    rol?: string;
    /** Empresa activa («Kns Telecom»). */
    empresa?: string;
    /** Punto de disponibilidad sobre el avatar: `true` disponible, `false` no; sin él, no hay punto. */
    disponible?: boolean | null;
    /** Controles bajo el nombre: empresa activa, disponibilidad en la bandeja y tema. */
    controles?: ReactNode;
    /** Opciones de la cuenta (Mi cuenta, Cambiar contraseña…), en orden. */
    opciones: readonly OpcionCuenta[];
    /** «Manual y atajos»: abre el manual de Iris. */
    alAbrirManual?: () => void;
    alCerrarSesion: () => void;
    cerrando?: boolean;
    /** Nombre accesible del panel («Tu cuenta», «Tu cuenta y disponibilidad»). */
    titulo?: string;
    /** Abierto (controlado). */
    abierto?: boolean;
    alCambiar?: (abierto: boolean) => void;
    className?: string;
};

/** «Supervisora · Kns Telecom». */
function lineaRol(rol: string | undefined, empresa: string | undefined): string {
    return [rol, empresa].filter(Boolean).join(" · ");
}

/**
 * La cuenta en el encabezado (rediseño): isla con el avatar (y su punto de disponibilidad), el
 * nombre y «rol · empresa». Abre un panel (`Popover` de HeroUI) con la identidad, los controles
 * (empresa, disponibilidad, tema) y el menú de la cuenta: sus opciones, «Manual y atajos» y, al
 * final y en rojo, «Cerrar sesión». En el teléfono, solo el avatar.
 */
export function MenuCuenta({
    nombre,
    correo,
    rol,
    empresa,
    disponible,
    controles,
    opciones,
    alAbrirManual,
    alCerrarSesion,
    cerrando = false,
    titulo = "Tu cuenta",
    abierto,
    alCambiar,
    className,
}: PropsMenuCuenta) {
    const [interno, setInterno] = useState(false);
    const estaAbierto = abierto ?? interno;
    const cambiar = (a: boolean) => {
        setInterno(a);
        alCambiar?.(a);
    };
    const linea = lineaRol(rol, empresa);
    const estado = disponible === undefined || disponible === null ? "" : disponible ? ", disponible" : ", no disponible";
    const elegir = (accion: () => void) => () => {
        cambiar(false);
        accion();
    };
    const avatar = (tamano: "sm" | "md") => (
        <span className="relative inline-flex flex-none">
            <AvatarUsuario nombre={nombre} {...(tamano === "md" ? { className: "size-12 text-[0.9375rem]" } : {})} />
            {disponible === undefined || disponible === null ? null : (
                <span aria-hidden data-slot="punto-disponible" className={cn("absolute -right-px -bottom-px size-2.5 rounded-full ring-2 ring-[var(--iris-tarjeta)]", disponible ? "bg-[var(--iris-exito-punto)]" : "bg-[var(--iris-texto-apagado)]")} />
            )}
        </span>
    );
    return (
        <PanelEmergente
            titulo={titulo}
            lado="bottom end"
            abierto={estaAbierto}
            alCambiar={cambiar}
            className="w-[min(18.5rem,calc(100vw-1rem))] max-w-[calc(100vw-1rem)]"
            disparador={
                <Boton
                    variante="contorno"
                    aria-label={`Tu cuenta: ${nombre}${estado}`}
                    title="Tu cuenta"
                    data-slot="menu-cuenta"
                    className={cn(CLASE_ISLA_ENCABEZADO, "h-11 gap-2.5 p-1 pr-1 sm:pr-3.5 hover:bg-[var(--iris-relleno)]", className)}
                >
                    {avatar("sm")}
                    <span className="hidden min-w-0 text-left leading-tight md:grid">
                        <span className="truncate text-[0.84375rem] font-bold" data-testid="usuario-nombre">
                            {nombre}
                        </span>
                        {linea ? <span className="truncate text-[0.6875rem] font-medium text-muted-foreground">{linea}</span> : null}
                    </span>
                    <ChevronDown aria-hidden className="hidden text-muted-foreground md:block" />
                </Boton>
            }
        >
            <div className="flex min-w-0 items-center gap-3" data-slot="identidad-cuenta">
                {avatar("md")}
                <span className="grid min-w-0 leading-snug">
                    <span className="truncate text-sm font-bold">{nombre}</span>
                    {rol ? <span className="truncate text-xs text-muted-foreground">{rol}</span> : null}
                    {correo ? <span className="truncate text-xs text-muted-foreground">{correo}</span> : null}
                </span>
            </div>
            {controles ? <div className="grid gap-2">{controles}</div> : null}
            <div className="-mx-2 border-t border-border pt-1">
                <MenuEnLinea etiqueta="Tu cuenta">
                    {opciones.map((o) => (
                        <MenuDesplegableElemento key={o.id} {...(o.icono ? { icono: o.icono } : {})} onSelect={elegir(o.alElegir)} disabled={o.deshabilitada ?? false}>
                            {o.texto}
                        </MenuDesplegableElemento>
                    ))}
                    {alAbrirManual ? (
                        <MenuDesplegableElemento key="manual" icono={BookOpen} onSelect={elegir(alAbrirManual)}>
                            Manual y atajos
                        </MenuDesplegableElemento>
                    ) : null}
                    <MenuDesplegableSeparador key="sep-salir" />
                    <MenuDesplegableElemento key="salir" icono={LogOut} onSelect={elegir(alCerrarSesion)} disabled={cerrando} className="text-danger [&_svg]:text-danger">
                        {cerrando ? "Cerrando sesión…" : "Cerrar sesión"}
                    </MenuDesplegableElemento>
                </MenuEnLinea>
            </div>
        </PanelEmergente>
    );
}
