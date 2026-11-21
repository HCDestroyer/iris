"use client";

import { Tooltip } from "@heroui/react";
import { CircleQuestionMark } from "lucide-react";
import { Link } from "react-aria-components";
import { cn } from "../utilidades";
import { variantesBoton } from "./boton";

/**
 * Clave de una pantalla en el manual: `modulo/pantalla` (o más niveles), en minúsculas, con
 * guiones. Es el contrato con el manual de Iris: el «?» abre `/manual/<clave>` y el manual tiene
 * una sección para cada clave (lista en `docs/manual/claves.md`).
 */
export const PATRON_CLAVE_MANUAL = /^[a-z0-9]+(?:-[a-z0-9]+)*(?:\/[a-z0-9]+(?:-[a-z0-9]+)*)+$/;

/** ¿`clave` sigue el formato `modulo/pantalla`? */
export function esClaveManual(clave: string): boolean {
    return PATRON_CLAVE_MANUAL.test(clave);
}

/** Ruta del manual para una pantalla. */
export function rutaManual(clave: string): string {
    if (!esClaveManual(clave)) throw new Error(`Clave del manual inválida: «${clave}» (formato modulo/pantalla)`);
    return `/manual/${clave}`;
}

export type PropsBotonManual = {
    /** Pantalla en el manual, con el formato `modulo/pantalla` (`tickets/lista`). */
    clave: string;
    /** Texto del globo y nombre accesible. */
    etiqueta?: string;
    /** `sm` (32 px) junto a un título; `md` (40 px) en una barra de botones redondos. */
    tamano?: "sm" | "md";
    className?: string;
};

/**
 * El «?» del manual junto al título de cada pantalla: un enlace con aspecto de botón redondo
 * (cursor de ayuda) que abre el manual en la sección de esta pantalla (`/manual/<clave>`), con su
 * nombre accesible y un globo. Navega con el enrutador de la app (`ProveedorRutas`).
 */
export function BotonManual({ clave, etiqueta = "Manual de esta pantalla", tamano = "sm", className }: PropsBotonManual) {
    return (
        <Tooltip delay={400} closeDelay={150}>
            <Link
                href={rutaManual(clave)}
                aria-label={etiqueta}
                data-slot="boton-manual"
                data-clave={clave}
                className={cn(
                    variantesBoton({ variante: "fantasma", tamano: tamano === "sm" ? "icono-sm" : "icono" }),
                    "cursor-help text-[var(--iris-texto-apagado)] hover:text-foreground [&_svg]:size-[1.125rem]",
                    className,
                )}
            >
                <CircleQuestionMark aria-hidden />
            </Link>
            <Tooltip.Content offset={6} placement="bottom" data-slot="globo" className="max-w-64 rounded-md border border-border px-2.5 py-1.5 text-xs text-foreground">
                {etiqueta}
            </Tooltip.Content>
        </Tooltip>
    );
}
