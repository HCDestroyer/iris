"use client";

import { CircleAlert, TriangleAlert } from "lucide-react";
import { AlertDialog, Dialog } from "radix-ui";
import { createContext, useCallback, useContext, useEffect, useId, useMemo, useState, type ComponentProps, type ReactNode } from "react";
import { cn } from "../utilidades";
import { Aviso } from "./aviso";
import { Boton, type PropsBoton } from "./boton";

/*
 * Patrón de formularios en modales (pedido del PM: «los modales deben alertar si no hay selección
 * o impedir guardar»). Sin dependencia de react-hook-form: la app le pasa `isDirty`, lo que falta
 * y `setError`.
 *
 *   <DialogoProtegido open={abierto} onOpenChange={setAbierto}>      // pide confirmar al cerrar con cambios
 *       <DialogoContenido titulo="…">
 *           <MiFormulario />                                      // useCambiosSinGuardar(form.formState.isDirty)
 *       </DialogoContenido>
 *   </DialogoProtegido>
 *
 *   <BotonEnviar faltan={cliente ? [] : ["el cliente"]} enviando={isSubmitting}>Crear</BotonEnviar>
 *   <DialogoConfirmar peligro titulo="¿Desactivar el buzón?" … alConfirmar={desactivar} />
 */

// --- Cambios sin guardar --------------------------------------------------------------------

type ContextoProtegido = { reportar: (id: string, sucio: boolean) => void };

const Contexto = createContext<ContextoProtegido | null>(null);

/**
 * Avisa al `DialogoProtegido` más cercano si el formulario tiene cambios sin guardar
 * (`form.formState.isDirty`). Fuera de un `DialogoProtegido` no hace nada.
 */
export function useCambiosSinGuardar(sucio: boolean) {
    const contexto = useContext(Contexto);
    const id = useId();
    useEffect(() => {
        if (!contexto) return undefined;
        contexto.reportar(id, sucio);
        return () => contexto.reportar(id, false);
    }, [contexto, id, sucio]);
}

export type PropsDialogoProtegido = Omit<ComponentProps<typeof Dialog.Root>, "open" | "onOpenChange"> & {
    open: boolean;
    onOpenChange: (abierto: boolean) => void;
    /** Cambios sin guardar conocidos por quien abre el diálogo (se suman a los que reportan los formularios). */
    sucio?: boolean;
    /** Mientras se guarda no se puede cerrar (ni con Escape ni tocando fuera). */
    ocupado?: boolean;
    tituloDescartar?: string;
    descripcionDescartar?: string;
};

/**
 * `Dialogo` controlado que, si hay cambios sin guardar, pide confirmación antes de cerrarse
 * (Escape, tocar fuera, la X o «Cancelar»). Cerrar por código (`open = false` tras guardar) no
 * pregunta.
 */
export function DialogoProtegido({
    open,
    onOpenChange,
    sucio = false,
    ocupado = false,
    tituloDescartar = "¿Descartar los cambios?",
    descripcionDescartar = "Lo que escribiste en este formulario se perderá.",
    children,
    ...props
}: PropsDialogoProtegido) {
    const [sucios, setSucios] = useState<ReadonlySet<string>>(() => new Set());
    const [confirmando, setConfirmando] = useState(false);
    const reportar = useCallback((id: string, valor: boolean) => {
        setSucios((antes) => {
            if (antes.has(id) === valor) return antes;
            const despues = new Set(antes);
            if (valor) despues.add(id);
            else despues.delete(id);
            return despues;
        });
    }, []);
    const contexto = useMemo(() => ({ reportar }), [reportar]);
    const hayCambios = sucio || sucios.size > 0;

    return (
        <Contexto.Provider value={contexto}>
            <Dialog.Root
                open={open}
                onOpenChange={(abierto) => {
                    if (abierto) {
                        onOpenChange(true);
                        return;
                    }
                    if (ocupado) return;
                    if (hayCambios) {
                        setConfirmando(true);
                        return;
                    }
                    onOpenChange(false);
                }}
                {...props}
            >
                {children}
            </Dialog.Root>
            <DialogoConfirmar
                open={open && confirmando}
                onOpenChange={setConfirmando}
                titulo={tituloDescartar}
                descripcion={descripcionDescartar}
                textoCancelar="Seguir editando"
                textoConfirmar="Descartar cambios"
                peligro
                alConfirmar={() => {
                    setConfirmando(false);
                    setSucios(new Set());
                    onOpenChange(false);
                }}
            />
        </Contexto.Provider>
    );
}

// --- Confirmación -----------------------------------------------------------------------------

export type PropsDialogoConfirmar = {
    open: boolean;
    onOpenChange: (abierto: boolean) => void;
    titulo: ReactNode;
    descripcion?: ReactNode;
    textoConfirmar: string;
    textoCancelar?: string;
    /** Texto del botón mientras corre `alConfirmar`. */
    textoOcupado?: string;
    /** Acción destructiva: botón en tono de error e icono de advertencia. */
    peligro?: boolean;
    /** Si devuelve una promesa, el diálogo espera (botones deshabilitados) y no se cierra solo. */
    alConfirmar: () => void | Promise<void>;
    /** Contenido extra bajo la descripción, p. ej. el aviso de error del servidor. */
    children?: ReactNode;
};

/**
 * Confirmación (`AlertDialog`): para acciones destructivas y para descartar cambios. No se
 * cierra tocando fuera; Escape y «Cancelar» la cierran sin hacer nada. El foco empieza en
 * «Cancelar».
 */
export function DialogoConfirmar({
    open,
    onOpenChange,
    titulo,
    descripcion,
    textoConfirmar,
    textoCancelar = "Cancelar",
    textoOcupado,
    peligro = false,
    alConfirmar,
    children,
}: PropsDialogoConfirmar) {
    const [ocupado, setOcupado] = useState(false);
    return (
        <AlertDialog.Root open={open} onOpenChange={(a) => !ocupado && onOpenChange(a)}>
            <AlertDialog.Portal>
                <AlertDialog.Overlay className="fixed inset-0 z-[60] bg-black/45" />
                <AlertDialog.Content
                    {...(descripcion ? {} : { "aria-describedby": undefined })}
                    className={cn(
                        "glass-strong fixed z-[60] flex max-h-[92dvh] w-full flex-col gap-4 overflow-y-auto p-5 shadow-xl",
                        "inset-x-0 bottom-0 rounded-t-[var(--hx-radius-card)]",
                        "sm:inset-auto sm:top-1/2 sm:left-1/2 sm:w-[min(28rem,calc(100vw-2rem))] sm:-translate-x-1/2 sm:-translate-y-1/2 sm:rounded-[var(--hx-radius-card)]",
                    )}
                >
                    <div className="flex gap-3">
                        {peligro ? <TriangleAlert aria-hidden className="mt-0.5 size-5 shrink-0 text-error" /> : null}
                        <div className="grid gap-1">
                            <AlertDialog.Title className="text-lg font-semibold tracking-[-0.015em]">{titulo}</AlertDialog.Title>
                            {descripcion ? <AlertDialog.Description className="text-sm text-muted-foreground">{descripcion}</AlertDialog.Description> : null}
                        </div>
                    </div>
                    {children}
                    <div className="flex flex-col-reverse gap-2 sm:flex-row sm:justify-end">
                        <AlertDialog.Cancel asChild>
                            <Boton variante="contorno" disabled={ocupado}>
                                {textoCancelar}
                            </Boton>
                        </AlertDialog.Cancel>
                        <Boton
                            variante={peligro ? "peligro" : "primario"}
                            disabled={ocupado}
                            onClick={async () => {
                                const r = alConfirmar();
                                if (r instanceof Promise) {
                                    setOcupado(true);
                                    try {
                                        await r;
                                    } finally {
                                        setOcupado(false);
                                    }
                                }
                            }}
                        >
                            {ocupado ? (textoOcupado ?? `${textoConfirmar}…`) : textoConfirmar}
                        </Boton>
                    </div>
                </AlertDialog.Content>
            </AlertDialog.Portal>
        </AlertDialog.Root>
    );
}

// --- Guardar --------------------------------------------------------------------------------

/** «Falta elegir el cliente» / «Falta elegir el cliente y el servicio». */
export function textoFaltan(faltan: readonly string[]): string {
    if (faltan.length === 0) return "";
    const lista = faltan.length === 1 ? faltan[0] : `${faltan.slice(0, -1).join(", ")} y ${faltan[faltan.length - 1]}`;
    return `Falta elegir ${lista} para guardar.`;
}

export type PropsBotonEnviar = Omit<PropsBoton, "type"> & {
    /** Selecciones obligatorias que faltan («el cliente», «el técnico»…): deshabilitan el botón. */
    faltan?: readonly string[];
    enviando?: boolean;
    /** Texto mientras se envía («Guardando…»). */
    textoEnviando?: ReactNode;
    /** Explicación propia en lugar de «Falta elegir …» (p. ej. «Elige el cliente o escribe la dirección»). */
    motivo?: string;
};

/**
 * Botón de enviar del formulario: deshabilitado mientras se envía o mientras falte una selección
 * obligatoria, con el motivo visible a su lado y enlazado (`aria-describedby`). El mensaje del
 * propio campo lo pone el formulario (`ayuda`/`error` del campo).
 */
export function BotonEnviar({ faltan = [], enviando = false, textoEnviando, motivo: propio, disabled, children, id, className, ...props }: PropsBotonEnviar) {
    const generado = useId();
    const idMotivo = `${id ?? generado}-falta`;
    const motivo = faltan.length > 0 ? (propio ?? textoFaltan(faltan)) : "";
    return (
        <span className="contents">
            {motivo ? (
                <p id={idMotivo} className="flex items-center gap-1.5 self-center text-sm text-muted-foreground sm:order-first sm:mr-auto" data-slot="falta">
                    <CircleAlert aria-hidden className="size-4 shrink-0 text-warning" />
                    {motivo}
                </p>
            ) : null}
            <Boton
                type="submit"
                id={id}
                className={className}
                disabled={disabled || enviando || faltan.length > 0}
                aria-describedby={motivo ? idMotivo : undefined}
                {...props}
            >
                {enviando && textoEnviando ? textoEnviando : children}
            </Boton>
        </span>
    );
}

// --- Errores del servidor -------------------------------------------------------------------

/** Un error de campo de un `problem+json` (`errors[]`). */
export type ErrorDeCampo = { field: string; message: string };

/** `body.lineas[0].precio` (API) → `lineas.0.precio` (nombre del campo en el formulario). */
export function campoDelFormulario(field: string): string {
    return field.replace(/^body\./, "").replace(/\[(\d+)\]/g, ".$1");
}

/**
 * Asigna los `errors[]` a los campos conocidos (`setError` de react-hook-form u otro) y devuelve
 * los que no se pudieron asignar, para mostrarlos en el aviso general del modal.
 */
export function asignarErrores<C extends string>(
    errores: readonly ErrorDeCampo[],
    campos: readonly C[],
    setError: (campo: C, error: { type: string; message: string }) => void,
): ErrorDeCampo[] {
    const restantes: ErrorDeCampo[] = [];
    for (const e of errores) {
        const campo = campoDelFormulario(e.field);
        if ((campos as readonly string[]).includes(campo)) {
            setError(campo as C, { type: "servidor", message: e.message });
        } else {
            restantes.push(e);
        }
    }
    return restantes;
}

export type PropsErrorFormulario = {
    titulo: ReactNode;
    detalle?: ReactNode;
    /** Errores de campo que no corresponden a ningún campo visible. */
    errores?: readonly ErrorDeCampo[];
    accion?: ReactNode;
    className?: string;
};

/** Aviso de error del servidor dentro del modal, con los `errors[]` sin campo en una lista. */
export function ErrorFormulario({ titulo, detalle, errores = [], accion, className }: PropsErrorFormulario) {
    return (
        <Aviso tono="error" titulo={titulo} accion={accion} className={className} data-slot="error-formulario">
            {detalle ? <p>{detalle}</p> : null}
            {errores.length > 0 ? (
                <ul className="mt-1 grid list-disc gap-0.5 pl-4">
                    {errores.map((e) => (
                        <li key={`${e.field}:${e.message}`}>{e.message}</li>
                    ))}
                </ul>
            ) : null}
        </Aviso>
    );
}
