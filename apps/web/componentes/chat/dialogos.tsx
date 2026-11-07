"use client";

import { zodResolver } from "@hookform/resolvers/zod";
import { Aviso, avisar, Autocompletar, Boton, BotonEnviar, CampoBusqueda, Cargando, DialogoCerrar, DialogoContenido, DialogoPie, DialogoProtegido, GrupoSegmentado, inicialesDe, ListaEleccion, useCambiosSinGuardar } from "@iris/ui";
import { ErrorTransporte } from "@iris/sync";
import { Info, RotateCw, User, Users } from "lucide-react";
import { useEffect, useState } from "react";
import { Controller, useForm } from "react-hook-form";
import { z } from "zod";
import { mensajeDeError } from "@/lib/api/errores";
import { claveTrasError, nuevaClaveIdempotencia } from "@/lib/api/cliente";
import { useDiferido } from "@/lib/diferido";
import { agregarMiembro, buscarCuadrillas, buscarPersonas, type CuadrillaChat, type MiembroChat, type PersonaChat } from "@/lib/chat/api";
import { useChatEquipo } from "./proveedor";

type Busqueda<T> = { estado: "cargando" } | { estado: "error"; error: unknown } | { estado: "listo"; datos: T[] };

/** Búsqueda en el servidor mientras se escribe (con respiro), con «Reintentar». */
function useBusqueda<T>(q: string, buscar: (q: string, s: AbortSignal) => Promise<T[]>, activa = true): [Busqueda<T>, () => void] {
    const diferido = useDiferido(q, 250);
    const [intento, setIntento] = useState(0);
    const [r, setR] = useState<Busqueda<T>>({ estado: "cargando" });
    useEffect(() => {
        if (!activa) return undefined;
        const c = new AbortController();
        setR({ estado: "cargando" });
        buscar(diferido, c.signal).then(
            (datos) => setR({ estado: "listo", datos }),
            (error: unknown) => {
                if (!c.signal.aborted) setR({ estado: "error", error });
            },
        );
        return () => c.abort();
        // `buscar` es una función de módulo.
        // eslint-disable-next-line react-hooks/exhaustive-deps
    }, [diferido, intento, activa]);
    return [r, () => setIntento((i) => i + 1)];
}

function ErrorBusqueda({ error, reintentar, que }: { error: unknown; reintentar: () => void; que: string }) {
    return (
        <Aviso
            tono="error"
            titulo={`No se pudieron leer ${que}`}
            accion={
                <Boton variante="secundario" tamano="sm" onClick={reintentar}>
                    <RotateCw />
                    Reintentar
                </Boton>
            }
        >
            {mensajeDeError(error).titulo}
        </Aviso>
    );
}

const esquemaNuevo = z.object({ tipo: z.enum(["persona", "cuadrilla"]), id: z.string().min(1, "Elige con quién") });

/**
 * «Nuevo mensaje»: una persona (directa) o una cuadrilla. Si el chat ya existe, se abre ese. Los
 * de orden se abren desde la orden.
 */
export function DialogoNuevoMensaje({ abierto, alCerrar, alAbrir }: { abierto: boolean; alCerrar: () => void; alAbrir: (conversacionId: number) => void }) {
    const chat = useChatEquipo();
    const form = useForm({ resolver: zodResolver(esquemaNuevo), defaultValues: { tipo: "persona" as "persona" | "cuadrilla", id: "" } });
    const tipo = form.watch("tipo");
    const elegido = form.watch("id");
    const [q, setQ] = useState("");
    const [personas, reintentarPersonas] = useBusqueda<PersonaChat>(q, buscarPersonas, abierto && tipo === "persona");
    const [cuadrillas, reintentarCuadrillas] = useBusqueda<CuadrillaChat>(q, buscarCuadrillas, abierto && tipo === "cuadrilla");
    const [error, setError] = useState<unknown>(null);
    useCambiosSinGuardar(false, form.formState.isSubmitting);
    if (!chat) return null;

    const nombreElegido =
        tipo === "persona"
            ? personas.estado === "listo"
                ? personas.datos.find((p) => String(p.id) === elegido)?.nombre
                : undefined
            : cuadrillas.estado === "listo"
              ? cuadrillas.datos.find((c) => String(c.id) === elegido)?.nombre
              : undefined;

    const enviar = form.handleSubmit(async (d) => {
        setError(null);
        try {
            const id = Number(d.id);
            const conv = d.tipo === "persona" ? await chat.motor.abrirDirecta(id) : await chat.motor.abrirCuadrilla(id);
            avisar({ titulo: d.tipo === "persona" ? `Chat con ${nombreElegido ?? "la persona"} abierto` : `Chat de ${nombreElegido ?? "la cuadrilla"} abierto` });
            form.reset();
            setQ("");
            alAbrir(conv.id);
        } catch (e) {
            setError(e);
        }
    });

    const cerrar = () => {
        form.reset();
        setQ("");
        setError(null);
        alCerrar();
    };

    return (
        <DialogoProtegido open={abierto} onOpenChange={(a) => !a && cerrar()} ocupado={form.formState.isSubmitting}>
            <DialogoContenido titulo="Nuevo mensaje" descripcion="Los chats de orden se abren desde la orden.">
                <form noValidate className="grid min-w-0 gap-3" aria-label="Nuevo mensaje" onSubmit={(e) => void enviar(e)}>
                    <Controller
                        control={form.control}
                        name="tipo"
                        render={({ field }) => (
                            <GrupoSegmentado
                                etiqueta="Con quién"
                                className="flex w-full"
                                claseOpcion="flex-1"
                                valor={field.value}
                                alCambiar={(v) => {
                                    field.onChange(v);
                                    form.setValue("id", "");
                                    setQ("");
                                }}
                                opciones={[
                                    { valor: "persona", etiqueta: "Persona", icono: User },
                                    { valor: "cuadrilla", etiqueta: "Cuadrilla", icono: Users },
                                ]}
                            />
                        )}
                    />
                    <CampoBusqueda etiqueta={tipo === "persona" ? "Buscar persona" : "Buscar cuadrilla"} etiquetaVisible={false} placeholder={tipo === "persona" ? "Buscar persona" : "Buscar cuadrilla"} valor={q} alCambiar={setQ} maxLength={80} />
                    <div className="max-h-[min(18rem,40dvh)] min-w-0 overflow-y-auto">
                        {tipo === "persona" ? (
                            personas.estado === "error" ? (
                                <ErrorBusqueda error={personas.error} reintentar={reintentarPersonas} que="las personas" />
                            ) : personas.estado === "cargando" ? (
                                <Cargando texto="Buscando…" />
                            ) : personas.datos.length === 0 ? (
                                <p className="py-4 text-center text-sm text-muted-foreground">{q.trim() ? `Nadie coincide con «${q.trim()}».` : "No hay personas con quien escribir."}</p>
                            ) : (
                                <Controller
                                    control={form.control}
                                    name="id"
                                    render={({ field, fieldState }) => (
                                        <ListaEleccion
                                            leyenda="Persona"
                                            leyendaOculta
                                            nombre="persona"
                                            pegadas
                                            valor={field.value || undefined}
                                            alCambiar={field.onChange}
                                            error={fieldState.error?.message}
                                            opciones={personas.datos.map((p) => ({
                                                valor: String(p.id),
                                                texto: p.nombre,
                                                contenido: (
                                                    <span className="flex min-w-0 items-center gap-2.5">
                                                        <span aria-hidden className="grid size-8 shrink-0 place-content-center rounded-full bg-elevated text-xs font-semibold">
                                                            {inicialesDe(p.nombre)}
                                                        </span>
                                                        <span className="min-w-0 truncate font-medium">{p.nombre}</span>
                                                    </span>
                                                ),
                                                ...(p.conversacion_id !== null ? { fin: <span className="text-xs text-muted-foreground">Ya existe</span> } : {}),
                                            }))}
                                        />
                                    )}
                                />
                            )
                        ) : cuadrillas.estado === "error" ? (
                            <ErrorBusqueda error={cuadrillas.error} reintentar={reintentarCuadrillas} que="las cuadrillas" />
                        ) : cuadrillas.estado === "cargando" ? (
                            <Cargando texto="Buscando…" />
                        ) : cuadrillas.datos.length === 0 ? (
                            <p className="py-4 text-center text-sm text-muted-foreground">{q.trim() ? `Ninguna cuadrilla coincide con «${q.trim()}».` : "No hay cuadrillas cuyo chat puedas abrir."}</p>
                        ) : (
                            <Controller
                                control={form.control}
                                name="id"
                                render={({ field, fieldState }) => (
                                    <ListaEleccion
                                        leyenda="Cuadrilla"
                                        leyendaOculta
                                        nombre="cuadrilla"
                                        pegadas
                                        valor={field.value || undefined}
                                        alCambiar={field.onChange}
                                        error={fieldState.error?.message}
                                        opciones={cuadrillas.datos.map((c) => ({
                                            valor: String(c.id),
                                            texto: c.nombre,
                                            contenido: (
                                                <span className="flex min-w-0 items-center gap-2.5">
                                                    <span aria-hidden className="grid size-8 shrink-0 place-content-center rounded-lg bg-primary/10 text-primary">
                                                        <Users className="size-4" />
                                                    </span>
                                                    <span className="grid min-w-0">
                                                        <span className="truncate font-medium">{c.nombre}</span>
                                                        <span className="truncate text-xs text-muted-foreground">
                                                            {c.tecnicos === 1 ? "1 técnico" : `${c.tecnicos} técnicos`}
                                                            {c.zonas?.length ? ` · ${c.zonas.join(" y ")}` : ""}
                                                        </span>
                                                    </span>
                                                </span>
                                            ),
                                            ...(c.conversacion_id !== null ? { fin: <span className="text-xs text-muted-foreground">Ya existe</span> } : {}),
                                        }))}
                                    />
                                )}
                            />
                        )}
                    </div>
                    <p className="flex items-start gap-1.5 text-xs text-muted-foreground">
                        <Info aria-hidden className="mt-px size-3.5 shrink-0" />
                        {tipo === "persona" ? "Un chat directo entre los dos. Si ya existe, se abre ese." : "Tú y la cuadrilla, con su supervisión. Si ya existe, se abre ese."}
                    </p>
                    {error ? (
                        <Aviso tono="error" titulo="No se pudo abrir el chat">
                            {error instanceof ErrorTransporte && error.codigo === "usuario_no_disponible" ? "Esa persona ya no está disponible para el chat." : mensajeDeError(error).titulo}
                        </Aviso>
                    ) : null}
                    <DialogoPie>
                        <DialogoCerrar asChild>
                            <Boton variante="contorno" disabled={form.formState.isSubmitting}>
                                Cancelar
                            </Boton>
                        </DialogoCerrar>
                        <BotonEnviar enviando={form.formState.isSubmitting} textoEnviando="Abriendo…" disabled={!elegido}>
                            {nombreElegido ? `Abrir chat${tipo === "cuadrilla" ? " de" : " con"} ${nombreElegido}` : "Abrir chat"}
                        </BotonEnviar>
                    </DialogoPie>
                </form>
            </DialogoContenido>
        </DialogoProtegido>
    );
}

const esquemaAgregar = z.object({ usuarioId: z.string().min(1, "Elige a quién agregar") });

/** «Agregar persona» a un hilo de orden (`chat.supervisar`): un solo campo. */
export function DialogoAgregarPersona({
    abierto,
    alCerrar,
    conversacionId,
    titulo,
    miembros,
    alAgregar,
}: {
    abierto: boolean;
    alCerrar: () => void;
    conversacionId: number;
    titulo: string;
    miembros: readonly MiembroChat[];
    alAgregar: (miembros: MiembroChat[]) => void;
}) {
    const form = useForm({ resolver: zodResolver(esquemaAgregar), defaultValues: { usuarioId: "" } });
    useCambiosSinGuardar(form.formState.isDirty, form.formState.isSubmitting);
    const [q, setQ] = useState("");
    const [personas, reintentar] = useBusqueda<PersonaChat>(q, buscarPersonas, abierto);
    const [error, setError] = useState<unknown>(null);
    const [clave, setClave] = useState(nuevaClaveIdempotencia);
    const ya = new Set(miembros.map((m) => m.usuario_id));
    const opciones = personas.estado === "listo" ? personas.datos.filter((p) => !ya.has(p.id)).map((p) => ({ valor: String(p.id), etiqueta: p.nombre })) : [];
    const cerrar = () => {
        form.reset();
        setQ("");
        setError(null);
        alCerrar();
    };
    const enviar = form.handleSubmit(async (d) => {
        setError(null);
        const nombre = personas.estado === "listo" ? personas.datos.find((p) => String(p.id) === d.usuarioId)?.nombre : undefined;
        try {
            const lista = await agregarMiembro(conversacionId, Number(d.usuarioId), clave);
            setClave(nuevaClaveIdempotencia());
            avisar({ titulo: `${nombre ?? "La persona"} ya está en el chat de ${titulo}` });
            alAgregar(lista);
            form.reset();
            setQ("");
            alCerrar();
        } catch (e) {
            setClave((c) => claveTrasError(e, c));
            setError(e);
        }
    });
    return (
        <DialogoProtegido open={abierto} onOpenChange={(a) => !a && cerrar()} sucio={form.formState.isDirty} ocupado={form.formState.isSubmitting}>
            <DialogoContenido titulo="Agregar persona" descripcion={`Verá todo el historial de ${titulo}.`}>
                <form noValidate className="grid gap-4" aria-label="Agregar persona" onSubmit={(e) => void enviar(e)}>
                    {personas.estado === "error" ? <ErrorBusqueda error={personas.error} reintentar={reintentar} que="las personas" /> : null}
                    <Controller
                        control={form.control}
                        name="usuarioId"
                        render={({ field, fieldState }) => (
                            <Autocompletar
                                etiqueta="Persona"
                                conLupa
                                placeholder="Buscar por nombre"
                                opciones={opciones}
                                valor={field.value || null}
                                alCambiar={(v) => field.onChange(v ?? "")}
                                alBuscar={setQ}
                                texto={q}
                                cargando={personas.estado === "cargando"}
                                sinResultados={q.trim() ? `Nadie más coincide con «${q.trim()}».` : "No hay a quién agregar."}
                                error={fieldState.error?.message}
                                obligatorio
                            />
                        )}
                    />
                    {error ? (
                        <Aviso tono="error" titulo="No se pudo agregar">
                            {mensajeDeError(error).titulo}
                        </Aviso>
                    ) : null}
                    <DialogoPie>
                        <DialogoCerrar asChild>
                            <Boton variante="contorno" disabled={form.formState.isSubmitting}>
                                Cancelar
                            </Boton>
                        </DialogoCerrar>
                        <BotonEnviar enviando={form.formState.isSubmitting} textoEnviando="Agregando…">
                            Agregar
                        </BotonEnviar>
                    </DialogoPie>
                </form>
            </DialogoContenido>
        </DialogoProtegido>
    );
}
