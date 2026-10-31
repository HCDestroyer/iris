"use client";

import { Toast, ToastQueue, type ToastContentValue } from "@heroui/react";
import type { ReactNode } from "react";
import { estadoHeroui, iconoPorTono, textoPorTono, type Tono } from "./tonos";

/*
 * Avisos emergentes (HeroUI `Toast`, React Aria): el aviso breve que confirma una acción
 * («Orden ORD-1042 asignada a Luis»), con «Deshacer» cuando se puede. Se anuncian en una región
 * `aria-live`; Alt+T lleva el foco a la región. Los errores que piden acción siguen siendo un
 * `Aviso` en la pantalla, con «Reintentar»: un aviso emergente se va solo.
 *
 *   <ProveedorAvisosEmergentes />                 // una vez, en el layout
 *   const { avisar } = useAvisoEmergente();
 *   avisar({ tono: "success", titulo: "Orden asignada", accion: { texto: "Deshacer", alPresionar: deshacer } });
 */

export type ContenidoAvisoEmergente = {
    /** Por defecto `success`: lo normal es confirmar que algo se guardó. */
    tono?: Tono;
    titulo: string;
    descripcion?: ReactNode;
    /** Una sola acción, p. ej. «Deshacer». Al presionarla el aviso se cierra. */
    accion?: { texto: string; alPresionar: () => void };
    /** Milisegundos antes de cerrarse (5 s; 8 s si tiene acción). 0 = hasta que se cierre a mano. */
    duracion?: number;
};

/** Cola de avisos de la app (fuera de React: se puede avisar desde cualquier lado). */
export const colaAvisos = new ToastQueue<ToastContentValue>({ maxVisibleToasts: 3 });

/** Duración por defecto: más tiempo si hay una acción que alcanzar a presionar. */
export function duracionAviso({ duracion, accion }: Pick<ContenidoAvisoEmergente, "duracion" | "accion">): number {
    return duracion ?? (accion ? 8000 : 5000);
}

/** Contenido para la cola de HeroUI: variante, icono de Lucide con el color del tono y acción. */
export function contenidoHeroui(aviso: ContenidoAvisoEmergente, cerrar: () => void): ToastContentValue {
    const tono = aviso.tono ?? "success";
    const Icono = iconoPorTono[tono];
    return {
        title: aviso.titulo,
        description: aviso.descripcion,
        variant: estadoHeroui[tono],
        indicator: <Icono aria-hidden className={`size-4 ${textoPorTono[tono]}`} />,
        actionProps: aviso.accion
            ? {
                  children: aviso.accion.texto,
                  variant: "tertiary",
                  size: "sm",
                  onPress: () => {
                      aviso.accion?.alPresionar();
                      cerrar();
                  },
              }
            : undefined,
    };
}

/** Muestra un aviso emergente y devuelve su id (para cerrarlo con `cerrarAviso`). */
export function avisar(aviso: ContenidoAvisoEmergente): string {
    let id = "";
    id = colaAvisos.add(
        contenidoHeroui(aviso, () => colaAvisos.close(id)),
        { timeout: duracionAviso(aviso) },
    );
    return id;
}

export function cerrarAviso(id: string): void {
    colaAvisos.close(id);
}

const API = { avisar, cerrar: cerrarAviso } as const;

/** `avisar` y `cerrar` para los componentes (referencias estables). */
export function useAvisoEmergente(): typeof API {
    return API;
}

export type PropsProveedorAvisosEmergentes = {
    /** Abajo a la derecha en escritorio; en el teléfono HeroUI lo centra sobre la barra inferior. */
    ubicacion?: "bottom end" | "bottom" | "top end" | "top";
};

/** Región de los avisos emergentes. Va una sola vez, en el layout de la app. */
export function ProveedorAvisosEmergentes({ ubicacion = "bottom end" }: PropsProveedorAvisosEmergentes) {
    return <Toast.Provider data-slot="avisos-emergentes" queue={colaAvisos} placement={ubicacion} />;
}
