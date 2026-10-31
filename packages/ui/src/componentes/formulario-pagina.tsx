"use client";

import { useCallback, useEffect, useRef, useState, type FormEventHandler, type ReactNode } from "react";
import { cn } from "../utilidades";
import { Boton } from "./boton";
import { BotonEnviar, DialogoConfirmar, ProveedorCambios, useRegistroCambios } from "./formulario-modal";
import { Migas, type Miga } from "./migas";

/*
 * Formularios en pantalla propia (regla del panel: más de 3 campos = pantalla con su URL).
 *
 *   <FormularioPagina titulo="Nueva orden" migas={[{ etiqueta: "Órdenes", href: "/ordenes" }, { etiqueta: "Nueva orden" }]}
 *       alEnviar={form.handleSubmit(guardar)} navegar={router.push} volverA="/ordenes"
 *       barra={{ textoGuardar: "Crear orden", enviando: isSubmitting, faltan }}>
 *       <SeccionFormulario titulo="1. Cliente y servicio">…</SeccionFormulario>
 *   </FormularioPagina>
 *
 * Dentro, el formulario llama `useCambiosSinGuardar(form.formState.isDirty, isSubmitting)` (el
 * mismo hook que en los diálogos). Con cambios sin guardar, salir por un enlace, por «Cancelar»,
 * con «Atrás» del navegador o recargando pregunta antes de descartar.
 */

// --- Protección al salir ----------------------------------------------------------------------

/** Lo mínimo de un clic para decidir si sale de la pantalla. */
export type ClicEnlace = {
    button: number;
    metaKey: boolean;
    ctrlKey: boolean;
    shiftKey: boolean;
    altKey: boolean;
    defaultPrevented: boolean;
    target: EventTarget | null;
};

/**
 * Si el clic es en un enlace que lleva a otra pantalla de la misma app (sin pestaña nueva, sin
 * descarga, sin teclas modificadoras), devuelve la ruta destino (`/ruta?consulta#ancla`); si no,
 * `null`. Los enlaces a la misma pantalla (solo cambia el `#`) no cuentan como salir.
 */
export function rutaQueSale(clic: ClicEnlace, ubicacion: Pick<Location, "origin" | "pathname" | "search">): string | null {
    if (clic.defaultPrevented || clic.button !== 0 || clic.metaKey || clic.ctrlKey || clic.shiftKey || clic.altKey) return null;
    const nodo = clic.target as { closest?: (selector: string) => Element | null } | null;
    const enlace = nodo?.closest?.("a[href]") as HTMLAnchorElement | null | undefined;
    if (!enlace) return null;
    const destinoVentana = enlace.getAttribute("target");
    if ((destinoVentana && destinoVentana !== "_self") || enlace.hasAttribute("download")) return null;
    let url: URL;
    try {
        url = new URL(enlace.getAttribute("href") ?? "", `${ubicacion.origin}${ubicacion.pathname}${ubicacion.search}`);
    } catch {
        return null;
    }
    if (url.origin !== ubicacion.origin) return null;
    if (url.pathname === ubicacion.pathname && url.search === ubicacion.search) return null;
    return `${url.pathname}${url.search}${url.hash}`;
}

const MARCA_HISTORIAL = "__irisCambiosSinGuardar";

export type ProteccionSalida = {
    /** Destino pendiente de confirmar (`null` si no hay pregunta abierta). */
    pendiente: string | null;
    /** Sale a `ruta` preguntando antes si hay cambios (para «Cancelar» y navegación por código). */
    salir: (ruta: string) => void;
    confirmar: () => void;
    seguir: () => void;
};

/**
 * Protege una pantalla con cambios sin guardar: enlaces de la app, «Atrás» del navegador (con una
 * entrada propia en el historial mientras haya cambios) y recargar o cerrar (`beforeunload`).
 * `navegar` es la navegación del cliente (`router.push`); sin ella, `location.assign`.
 */
export function useProteccionSalida(activa: boolean, navegar?: (ruta: string) => void): ProteccionSalida {
    const [pendiente, setPendiente] = useState<string | null>(null);
    const activaRef = useRef(activa);
    /** Ya se confirmó salir: la entrada propia del historial se deja (la navegación está en curso). */
    const saliendoRef = useRef(false);
    useEffect(() => {
        activaRef.current = activa;
    }, [activa]);
    const ir = useCallback((ruta: string) => (navegar ? navegar(ruta) : window.location.assign(ruta)), [navegar]);

    // Recargar, cerrar la pestaña o salir a otro sitio.
    useEffect(() => {
        if (!activa) return undefined;
        const antesDeSalir = (evento: BeforeUnloadEvent) => {
            evento.preventDefault();
            evento.returnValue = "";
        };
        window.addEventListener("beforeunload", antesDeSalir);
        return () => window.removeEventListener("beforeunload", antesDeSalir);
    }, [activa]);

    // Enlaces de la app: se capturan antes que el `onClick` de `<Link>`.
    useEffect(() => {
        if (!activa) return undefined;
        const alClic = (evento: MouseEvent) => {
            const ruta = rutaQueSale(evento, window.location);
            if (!ruta) return;
            evento.preventDefault();
            evento.stopPropagation();
            setPendiente(ruta);
        };
        window.addEventListener("click", alClic, true);
        return () => window.removeEventListener("click", alClic, true);
    }, [activa]);

    // «Atrás» del navegador: una entrada propia en el historial que se consume al volver.
    useEffect(() => {
        const marcada = Boolean((window.history.state as Record<string, unknown> | null)?.[MARCA_HISTORIAL]);
        if (!activa) {
            // Sin cambios, la entrada propia sobra (misma URL): se quita para que «Atrás» funcione a la primera.
            if (marcada && !saliendoRef.current) window.history.back();
            return undefined;
        }
        if (!marcada) window.history.pushState({ ...(window.history.state as object | null), [MARCA_HISTORIAL]: true }, "", window.location.href);
        const alVolver = () => {
            if (activaRef.current) setPendiente(ATRAS);
        };
        window.addEventListener("popstate", alVolver);
        return () => window.removeEventListener("popstate", alVolver);
    }, [activa]);

    const salir = useCallback(
        (ruta: string) => {
            if (activaRef.current) setPendiente(ruta);
            else ir(ruta);
        },
        [ir],
    );
    const seguir = useCallback(() => {
        if (pendiente === ATRAS) window.history.pushState({ ...(window.history.state as object | null), [MARCA_HISTORIAL]: true }, "", window.location.href);
        setPendiente(null);
    }, [pendiente]);
    const confirmar = useCallback(() => {
        const destino = pendiente;
        setPendiente(null);
        activaRef.current = false;
        saliendoRef.current = true;
        if (destino === ATRAS) window.history.back();
        else if (destino) ir(destino);
    }, [pendiente, ir]);
    return { pendiente, salir, confirmar, seguir };
}

const ATRAS = "\u0000atras";

// --- Barra de guardar -------------------------------------------------------------------------

export type PropsBarraGuardar = {
    textoGuardar?: string;
    textoEnviando?: string;
    textoCancelar?: string;
    enviando?: boolean;
    /** Selecciones obligatorias que faltan: deshabilitan «Guardar» y se explican al lado. */
    faltan?: readonly string[];
    motivo?: string;
    alCancelar?: () => void;
    /** `id` del `<form>` si la barra queda fuera de él. */
    form?: string;
    className?: string;
};

/**
 * Barra fija al pie del formulario con «Cancelar» y «Guardar», siempre visible al desplazarse.
 * En vidrio (capa funcional) y con el margen del área segura del teléfono.
 */
export function BarraGuardar({
    textoGuardar = "Guardar",
    textoEnviando = "Guardando…",
    textoCancelar = "Cancelar",
    enviando = false,
    faltan = [],
    motivo,
    alCancelar,
    form,
    className,
}: PropsBarraGuardar) {
    return (
        <div
            data-slot="barra-guardar"
            className={cn(
                "glass-material sticky bottom-0 z-20 mt-2 flex min-w-0 flex-col-reverse gap-2 rounded-t-[var(--hx-radius-card)] border-t px-4 pt-3",
                "pb-[max(0.75rem,env(safe-area-inset-bottom))] sm:flex-row sm:items-center sm:justify-end",
                className,
            )}
        >
            {alCancelar ? (
                <Boton variante="contorno" onClick={alCancelar} disabled={enviando}>
                    {textoCancelar}
                </Boton>
            ) : null}
            <BotonEnviar enviando={enviando} textoEnviando={textoEnviando} faltan={faltan} {...(motivo ? { motivo } : {})} {...(form ? { form } : {})}>
                {textoGuardar}
            </BotonEnviar>
        </div>
    );
}

// --- Página de formulario ---------------------------------------------------------------------

export type PropsSeccionFormulario = {
    titulo: ReactNode;
    descripcion?: ReactNode;
    children: ReactNode;
    className?: string;
};

/** Sección numerada del formulario («1. Cliente y servicio») en una tarjeta. */
export function SeccionFormulario({ titulo, descripcion, children, className }: PropsSeccionFormulario) {
    return (
        <section data-slot="seccion-formulario" className={cn("surface-card grid min-w-0 gap-4 p-4 sm:p-5 [&>*]:min-w-0", className)}>
            <div className="grid gap-1">
                <h2 className="text-base font-semibold tracking-[-0.015em]">{titulo}</h2>
                {descripcion ? <p className="text-sm text-muted-foreground">{descripcion}</p> : null}
            </div>
            {children}
        </section>
    );
}

export type PropsFormularioPagina = {
    titulo: ReactNode;
    descripcion?: ReactNode;
    migas: readonly Miga[];
    /** `onSubmit` del `<form>` (p. ej. `form.handleSubmit(guardar)`). */
    alEnviar: FormEventHandler<HTMLFormElement>;
    /** Props de la barra de guardar; «Cancelar» vuelve a `volverA` (preguntando si hay cambios). */
    barra?: Omit<PropsBarraGuardar, "alCancelar" | "form">;
    /** A dónde lleva «Cancelar» (normalmente la miga anterior). */
    volverA?: string;
    /** Navegación del cliente (`router.push`). */
    navegar?: (ruta: string) => void;
    /** Cambios sin guardar conocidos por la página (se suman a los de `useCambiosSinGuardar`). */
    sucio?: boolean;
    /** Error general del envío (p. ej. `ErrorFormulario`), arriba de las secciones. */
    error?: ReactNode;
    children: ReactNode;
    className?: string;
};

/**
 * Pantalla de formulario: migas, título, secciones y la barra de guardar fija al pie. Nunca se
 * abre encima de otro formulario: tiene su propia URL y «Atrás» vuelve a donde estabas. Si sales
 * con cambios sin guardar, pregunta antes de descartarlos.
 */
export function FormularioPagina({ titulo, descripcion, migas, alEnviar, barra, volverA, navegar, sucio = false, error, children, className }: PropsFormularioPagina) {
    const registro = useRegistroCambios();
    const hayCambios = (sucio || registro.sucio) && !registro.ocupado && !barra?.enviando;
    const proteccion = useProteccionSalida(hayCambios, navegar);
    const destinoCancelar = volverA ?? migas.at(-2)?.href;
    return (
        <ProveedorCambios valor={registro.contexto}>
            <div data-slot="formulario-pagina" className={cn("mx-auto grid w-full max-w-3xl min-w-0 gap-4 [&>*]:min-w-0", className)}>
                <header className="grid gap-2">
                    <Migas migas={migas} />
                    <div className="grid gap-1">
                        <h1 className="text-xl font-semibold tracking-[-0.015em] sm:text-2xl">{titulo}</h1>
                        {descripcion ? <p className="text-sm text-muted-foreground">{descripcion}</p> : null}
                    </div>
                </header>
                <form noValidate onSubmit={alEnviar} className="grid min-w-0 gap-4 [&>*]:min-w-0">
                    {error}
                    {children}
                    <BarraGuardar {...barra} {...(destinoCancelar ? { alCancelar: () => proteccion.salir(destinoCancelar) } : {})} />
                </form>
            </div>
            <DialogoConfirmar
                open={proteccion.pendiente !== null}
                onOpenChange={(abierto) => !abierto && proteccion.seguir()}
                titulo="¿Descartar los cambios?"
                descripcion="Hay cambios sin guardar en este formulario. Si sales ahora, se pierden."
                textoCancelar="Seguir editando"
                textoConfirmar="Descartar cambios"
                peligro
                alConfirmar={() => {
                    registro.olvidar();
                    proteccion.confirmar();
                }}
            />
        </ProveedorCambios>
    );
}
