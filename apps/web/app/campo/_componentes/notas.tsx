"use client";

import { zodResolver } from "@hookform/resolvers/zod";
import type { NotaOrden, OrdenVista } from "@iris/sync";
import { contadorNota, MAX_NOTA, metaNota, notasOrdenadas, puedeCambiarNota } from "@iris/sync/campo";
import { Boton, CampoArea, DialogoConfirmar, DialogoPie, Tarjeta, TarjetaContenido, cn } from "@iris/ui";
import { Pencil, Plus, Trash2 } from "lucide-react";
import { useState } from "react";
import { useForm, useWatch } from "react-hook-form";
import { useAhora } from "@/componentes/tickets/insignias";
import { esquemaNota, type DatosNota } from "@/lib/campo/esquemas";
import { useCampo } from "./contexto";
import { HojaCampo, useAtrasCierraHoja } from "./hoja";

/**
 * Notas de la orden (en cualquier estado, también pausada o cancelada: evidencia). «Agregar» abre
 * la hoja de la nota nueva; tocar una nota la abre con su autor, hora y canal y, si es suya (o con
 * `ordenes.asignar`), «Eliminar» y «Editar». Todo va a la cola sin conexión. `agregando` lo abre
 * también el menú «⋯» de la orden.
 */
export function Notas({
    orden,
    editable,
    agregando,
    alAgregar,
}: {
    orden: OrdenVista;
    editable: boolean;
    agregando: boolean;
    alAgregar: (abierto: boolean) => void;
}) {
    const { sesion } = useCampo();
    const ahora = useAhora(60_000);
    const notas = notasOrdenadas(orden);
    const [abiertaId, setAbiertaId] = useState<string | null>(null);
    const abierta = notas.find((n) => n.id === abiertaId) ?? null;
    return (
        <Tarjeta data-slot="notas-orden">
            <TarjetaContenido className="grid gap-0 p-4">
                <div className="flex min-h-12 items-center justify-between gap-3 border-b border-border pb-3">
                    <h2 className="text-base font-semibold">Notas</h2>
                    {editable ? (
                        <Boton variante="secundario" className="h-11 rounded-full px-4 md:h-11" aria-label="Agregar nota" onClick={() => alAgregar(true)}>
                            <Plus aria-hidden />
                            Agregar
                        </Boton>
                    ) : null}
                </div>
                {notas.length > 0 ? (
                    <ul className="grid" aria-label="Notas de la orden">
                        {notas.map((n) => {
                            const meta = metaNota(n, sesion.usuarioId, ahora);
                            return (
                                <li key={n.id} className="border-b border-border last:border-b-0">
                                    <Boton
                                        variante="fantasma"
                                        data-slot="nota"
                                        aria-label={`Nota: ${n.texto}. ${meta.corta}`}
                                        className="grid h-auto min-h-12 w-full justify-start gap-1 rounded-none px-0 py-3 text-left text-base font-normal whitespace-normal md:h-auto"
                                        onClick={() => setAbiertaId(n.id)}
                                    >
                                        <span className="line-clamp-3 whitespace-pre-wrap">{n.texto}</span>
                                        <span className="text-sm text-muted-foreground">
                                            {meta.corta}
                                            {meta.editada ? " · editada" : ""}
                                        </span>
                                    </Boton>
                                </li>
                            );
                        })}
                    </ul>
                ) : (
                    <p className="pt-3 text-sm text-muted-foreground">Sin notas.</p>
                )}
            </TarjetaContenido>
            <HojaNuevaNota orden={orden} abierto={agregando} alCambiar={alAgregar} />
            {abierta ? (
                <HojaNota
                    orden={orden}
                    nota={abierta}
                    puedeCambiar={editable && puedeCambiarNota(abierta, sesion.usuarioId, sesion.permisos)}
                    meta={metaNota(abierta, sesion.usuarioId, ahora).larga}
                    alCerrar={() => setAbiertaId(null)}
                />
            ) : null}
        </Tarjeta>
    );
}

function Contador({ texto }: { texto: string }) {
    return (
        <p aria-live="polite" data-slot="contador-nota" className={cn("text-right text-xs tabular-nums", texto.length > MAX_NOTA ? "text-error" : "text-muted-foreground")}>
            {contadorNota(texto)}
        </p>
    );
}

function HojaNuevaNota({ orden, abierto, alCambiar }: { orden: OrdenVista; abierto: boolean; alCambiar: (abierto: boolean) => void }) {
    const { motor, ejecutar } = useCampo();
    const form = useForm<DatosNota>({ resolver: zodResolver(esquemaNota), defaultValues: { texto: "" } });
    const texto = useWatch({ control: form.control, name: "texto" });
    const enviar = form.handleSubmit(async (d) => {
        if (await ejecutar(() => motor.registrar("nota.agregar", orden.id, { texto: d.texto }), "Nota guardada")) {
            form.reset();
            alCambiar(false);
        }
    });
    return (
        <HojaCampo
            abierto={abierto}
            alCambiar={(a) => {
                alCambiar(a);
                if (!a) {
                    form.reset();
                }
            }}
            cambios={form.formState.isDirty}
            titulo="Nueva nota"
            descripcion={`Para ${orden.numero}. Despacho la ve; el cliente no.`}
        >
            <form onSubmit={enviar} className="grid gap-2" noValidate>
                <CampoArea etiqueta="Nueva nota" rows={4} autoFocus error={form.formState.errors.texto?.message} {...form.register("texto")} />
                <Contador texto={texto} />
                <DialogoPie>
                    <Boton tamano="lg" type="submit" disabled={form.formState.isSubmitting}>
                        Agregar nota
                    </Boton>
                </DialogoPie>
            </form>
        </HojaCampo>
    );
}

/**
 * Una nota abierta: su texto con autor · hora · canal y [Eliminar | Editar] (si puede). «Editar»
 * cambia la misma hoja al formulario con el contador; «Eliminar» pregunta antes. «Atrás» cierra la
 * hoja (o la pregunta).
 */
function HojaNota({ orden, nota, puedeCambiar, meta, alCerrar }: { orden: OrdenVista; nota: NotaOrden; puedeCambiar: boolean; meta: string; alCerrar: () => void }) {
    const { motor, ejecutar } = useCampo();
    const [editando, setEditando] = useState(false);
    const [eliminando, setEliminando] = useState(false);
    const form = useForm<DatosNota>({ resolver: zodResolver(esquemaNota), defaultValues: { texto: nota.texto } });
    const texto = useWatch({ control: form.control, name: "texto" });
    const sinCambios = texto.trim() === nota.texto.trim();
    useAtrasCierraHoja(eliminando, () => setEliminando(false));
    const guardar = form.handleSubmit(async (d) => {
        if (await ejecutar(() => motor.registrar("nota.editar", orden.id, { notaId: nota.id, versionNota: nota.version, texto: d.texto }), "Nota editada")) {
            alCerrar();
        }
    });
    const eliminar = async () => {
        if (await ejecutar(() => motor.registrar("nota.eliminar", orden.id, { notaId: nota.id, versionNota: nota.version }), "Nota eliminada")) {
            setEliminando(false);
            alCerrar();
        }
    };
    return (
        <>
            <HojaCampo
                abierto={!eliminando}
                alCambiar={(a) => {
                    if (!a) {
                        alCerrar();
                    }
                }}
                cambios={editando && form.formState.isDirty}
                titulo={editando ? "Editar nota" : "Nota"}
                descripcion={editando ? undefined : meta}
            >
                {editando ? (
                    <form onSubmit={guardar} className="grid gap-2" noValidate>
                        <CampoArea etiqueta="Texto de la nota" rows={5} autoFocus error={form.formState.errors.texto?.message} {...form.register("texto")} />
                        <Contador texto={texto} />
                        <div className="grid grid-cols-2 gap-2 pt-2">
                            <Boton variante="secundario" tamano="lg" className="rounded-full" onClick={() => alCerrar()}>
                                Cancelar
                            </Boton>
                            <Boton tamano="lg" type="submit" className="rounded-full" disabled={form.formState.isSubmitting || sinCambios}>
                                Guardar
                            </Boton>
                        </div>
                    </form>
                ) : (
                    <div className="grid gap-4">
                        <p className="text-base whitespace-pre-wrap" data-slot="texto-nota">
                            {nota.texto}
                        </p>
                        {puedeCambiar ? (
                            <div className="grid grid-cols-2 gap-2">
                                <Boton variante="peligro" tamano="lg" className="rounded-full" onClick={() => setEliminando(true)}>
                                    <Trash2 aria-hidden />
                                    Eliminar
                                </Boton>
                                <Boton variante="secundario" tamano="lg" className="rounded-full" onClick={() => setEditando(true)}>
                                    <Pencil aria-hidden />
                                    Editar
                                </Boton>
                            </div>
                        ) : (
                            <p className="text-sm text-muted-foreground">La escribió otra persona: solo ella (o despacho) la cambia.</p>
                        )}
                    </div>
                )}
            </HojaCampo>
            <DialogoConfirmar
                open={eliminando}
                onOpenChange={(a) => {
                    if (!a) {
                        setEliminando(false);
                    }
                }}
                titulo="¿Eliminar la nota?"
                descripcion="Se quita de la orden. Queda registrada en el historial de la orden."
                textoConfirmar="Eliminar"
                peligro
                alConfirmar={eliminar}
            />
        </>
    );
}
