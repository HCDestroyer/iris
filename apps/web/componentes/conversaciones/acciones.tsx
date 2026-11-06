"use client";

import { zodResolver } from "@hookform/resolvers/zod";
import {
    Aviso,
    avisar,
    Boton,
    BotonEnviar,
    CampoArea,
    CampoSelector,
    Casilla,
    cn,
    DialogoCerrar,
    DialogoConfirmar,
    DialogoContenido,
    DialogoPie,
    DialogoProtegido,
    Interruptor,
    MenuDesplegable,
    MenuDesplegableContenido,
    MenuDesplegableDisparador,
    MenuDesplegableElemento,
    MenuDesplegableSeparador,
    useCambiosSinGuardar,
} from "@iris/ui";
import { ArrowLeftRight, AtSign, BookOpen, Check, ChevronDown, Ellipsis, LifeBuoy, RotateCcw, RotateCw, Tag, Unlink, User, UserCheck, UserMinus, UserPlus } from "lucide-react";
import { useRef, useState, type ReactNode } from "react";
import { useForm, type FieldValues, type Path, type UseFormSetError } from "react-hook-form";
import { z } from "zod";
import { api, claveTrasError, nuevaClaveIdempotencia, resolver } from "@/lib/api/cliente";
import { ErrorApi, mensajeDeError } from "@/lib/api/errores";
import { cabeceraIdem, nuevoIdMensaje } from "@/lib/conversaciones/api";
import type { Conversacion } from "@/lib/conversaciones/tipos";
import { erroresACampos } from "@/lib/formularios";
import { useSesion } from "@/lib/sesion";
import { agentesDeCola, useCatalogos } from "./catalogos";

type Dialogo = "asignar" | "transferir" | "etiquetar" | "mencionar" | "ayuda" | "desasociar" | null;

type Props = {
    conv: Conversacion;
    /** Cabecera angosta (teléfono): Ayuda y Resolver pasan al menú «⋯». */
    estrecho: boolean;
    alActualizar: (c: Conversacion) => void;
    recargar: () => Promise<void>;
    abrirAyuda: () => void;
};

const BASE = "/api/v1/mensajeria/conversaciones/{id}";
const ruta = (c: Conversacion, clave: string) => ({ path: { id: c.id }, header: cabeceraIdem(clave) });

/**
 * Acciones de la cabecera en un grupo compacto (diseño del PM): [Asignado ▾ | Ayuda ▾ | Resolver |
 * ⋯]. Con poco ancho quedan los íconos y, en el teléfono, solo «Asignado» y «⋯» (con todo lo
 * demás adentro). Cada acción es explícita (`POST …/<verbo>`), con `version` e `Idempotency-Key`,
 * y avisa al terminar.
 */
export function AccionesConversacion({ conv, estrecho, alActualizar, recargar, abrirAyuda }: Props) {
    const { yo, puede } = useSesion();
    const [dialogo, setDialogo] = useState<Dialogo>(null);
    const [ocupado, setOcupado] = useState(false);
    const [errorRapido, setErrorRapido] = useState<unknown>(null);
    const mia = conv.asignado?.id === yo.usuario.id;
    const resuelta = conv.estado === "resuelta";
    const p = {
        tomar: puede(["conversaciones.tomar"]) && !conv.asignado,
        asignar: puede(["conversaciones.asignar"]),
        transferir: puede(["conversaciones.transferir"]),
        etiquetar: puede(["conversaciones.etiquetar"]),
        resolver: puede(["conversaciones.resolver"]),
        mencionar: puede(["conversaciones.responder"]),
        asociar: puede(["conversaciones.asociar"]) && conv.contacto.tipo !== "desconocido",
    };

    /** Acción de un toque (tomar, resolver, reabrir, quitar asignación), con aviso y «Deshacer» si se puede. */
    async function rapida(nombre: string, hacer: (clave: string) => Promise<Conversacion>, aviso: (c: Conversacion) => Parameters<typeof avisar>[0]) {
        setOcupado(true);
        setErrorRapido(null);
        try {
            const c = await hacer(nuevaClaveIdempotencia());
            alActualizar(c);
            avisar(aviso(c));
        } catch (e) {
            setErrorRapido(e);
            if (e instanceof ErrorApi && e.estado === 409) void recargar();
        } finally {
            setOcupado(false);
        }
        void nombre;
    }

    const tomar = () =>
        rapida(
            "tomar",
            async (clave) => await resolver(api().POST(`${BASE}/tomar`, { params: ruta(conv, clave), body: { version: conv.version } })),
            () => ({ titulo: `Tomaste la conversación con ${conv.titulo}` }),
        );
    const quitar = () =>
        rapida(
            "quitar",
            async (clave) => await resolver(api().POST(`${BASE}/asignar`, { params: ruta(conv, clave), body: { version: conv.version, usuarioId: null } })),
            () => ({ titulo: `${conv.titulo}: sin asignar` }),
        );
    const reabrir = (c: Conversacion = conv) =>
        rapida(
            "reabrir",
            async (clave) => await resolver(api().POST(`${BASE}/reabrir`, { params: { path: { id: c.id }, header: cabeceraIdem(clave) }, body: { version: c.version } })),
            () => ({ titulo: `Reabriste la conversación con ${c.titulo}` }),
        );
    const resolverConv = () =>
        rapida(
            "resolver",
            async (clave) => await resolver(api().POST(`${BASE}/resolver`, { params: ruta(conv, clave), body: { version: conv.version } })),
            (c) => ({ titulo: `Conversación con ${c.titulo} resuelta`, ...(p.resolver ? { accion: { texto: "Deshacer", alPresionar: () => void reabrir(c) } } : {}) }),
        );

    const nombreAsignado = conv.asignado ? (mia ? "Tú" : conv.asignado.nombre.split(" ")[0]) : "Sin asignar";
    const menuAsignado = (
        <MenuDesplegable>
            <MenuDesplegableDisparador>
                <Boton
                    variante="fantasma"
                    tamano="sm"
                    className="h-8 max-w-[11rem] rounded-none px-2.5"
                    aria-label={conv.asignado ? `Asignada a ${conv.asignado.nombre}. Cambiar` : "Sin asignar. Asignar"}
                    disabled={ocupado || !(p.tomar || p.asignar)}
                >
                    {conv.asignado ? <UserCheck /> : <User />}
                    <span className="truncate @max-[44rem]/conv:hidden">{nombreAsignado}</span>
                    <ChevronDown className="@max-[44rem]/conv:hidden" />
                </Boton>
            </MenuDesplegableDisparador>
            <MenuDesplegableContenido align="end">
                {p.tomar ? (
                    <MenuDesplegableElemento icono={UserPlus} onSelect={tomar}>
                        Asignarme
                    </MenuDesplegableElemento>
                ) : null}
                {p.asignar ? (
                    <MenuDesplegableElemento icono={UserCheck} onSelect={() => setDialogo("asignar")}>
                        Asignar a…
                    </MenuDesplegableElemento>
                ) : null}
                {p.asignar && conv.asignado ? (
                    <MenuDesplegableElemento icono={UserMinus} onSelect={quitar}>
                        Quitar asignación
                    </MenuDesplegableElemento>
                ) : null}
            </MenuDesplegableContenido>
        </MenuDesplegable>
    );

    const elementosAyuda = (
        <>
            {p.mencionar ? (
                <MenuDesplegableElemento icono={AtSign} onSelect={() => setDialogo("mencionar")}>
                    Mencionar a un compañero
                </MenuDesplegableElemento>
            ) : null}
            {p.mencionar ? (
                <MenuDesplegableElemento icono={LifeBuoy} onSelect={() => setDialogo("ayuda")}>
                    Pedir ayuda al supervisor
                </MenuDesplegableElemento>
            ) : null}
            {p.transferir ? (
                <MenuDesplegableElemento icono={ArrowLeftRight} onSelect={() => setDialogo("transferir")}>
                    Pasar a otra cola
                </MenuDesplegableElemento>
            ) : null}
            {puede(["manuales.ver"]) ? (
                <MenuDesplegableElemento icono={BookOpen} onSelect={abrirAyuda}>
                    Buscar en los manuales
                </MenuDesplegableElemento>
            ) : null}
        </>
    );

    return (
        <>
            <div className="flex shrink-0 items-center gap-1.5">
                <div role="toolbar" aria-label="Acciones de la conversación" className="flex items-stretch overflow-hidden rounded-lg border border-border bg-card [&>*+*]:border-l [&>*+*]:border-border">
                    {menuAsignado}
                    {estrecho ? null : (
                        <MenuDesplegable>
                            <MenuDesplegableDisparador>
                                <Boton variante="fantasma" tamano="sm" className="h-8 rounded-none px-2.5" aria-label="Ayuda">
                                    <LifeBuoy />
                                    <span className="@max-[52rem]/conv:hidden">Ayuda</span>
                                </Boton>
                            </MenuDesplegableDisparador>
                            <MenuDesplegableContenido align="end">{elementosAyuda}</MenuDesplegableContenido>
                        </MenuDesplegable>
                    )}
                    {p.resolver && !estrecho ? (
                            <Boton variante="fantasma" tamano="sm" className="h-8 rounded-none px-2.5" onClick={() => void (resuelta ? reabrir() : resolverConv())} disabled={ocupado} aria-label={resuelta ? "Reabrir" : "Resolver"}>
                                {resuelta ? <RotateCcw /> : <Check />}
                                <span className="@max-[52rem]/conv:hidden">{resuelta ? "Reabrir" : "Resolver"}</span>
                            </Boton>
                    ) : null}
                    <MenuDesplegable>
                        <MenuDesplegableDisparador>
                            <Boton variante="fantasma" tamano="sm" className="h-8 w-8 rounded-none px-0" aria-label="Más acciones" title="Más acciones">
                                <Ellipsis />
                            </Boton>
                        </MenuDesplegableDisparador>
                        <MenuDesplegableContenido align="end">
                            {/* En el teléfono, lo que no cabe en la cabecera va aquí. */}
                            {estrecho && p.resolver ? (
                                <MenuDesplegableElemento icono={resuelta ? RotateCcw : Check} onSelect={() => void (resuelta ? reabrir() : resolverConv())}>
                                    {resuelta ? "Reabrir" : "Resolver"}
                                </MenuDesplegableElemento>
                            ) : null}
                            {estrecho ? elementosAyuda : null}
                            {estrecho ? <MenuDesplegableSeparador /> : null}
                            {p.etiquetar ? (
                                <MenuDesplegableElemento icono={Tag} onSelect={() => setDialogo("etiquetar")}>
                                    Etiquetas…
                                </MenuDesplegableElemento>
                            ) : null}
                            {p.asociar ? (
                                <MenuDesplegableElemento icono={Unlink} onSelect={() => setDialogo("desasociar")}>
                                    Quitar el cliente asociado
                                </MenuDesplegableElemento>
                            ) : null}
                        </MenuDesplegableContenido>
                    </MenuDesplegable>
                </div>
            </div>
            {errorRapido ? <ErrorRapido error={errorRapido} cerrar={() => setErrorRapido(null)} recargar={recargar} /> : null}
            {dialogo === "asignar" ? <DialogoAsignar conv={conv} cerrar={() => setDialogo(null)} alHecho={alActualizar} recargar={recargar} /> : null}
            {dialogo === "transferir" ? <DialogoTransferir conv={conv} cerrar={() => setDialogo(null)} alHecho={alActualizar} recargar={recargar} /> : null}
            {dialogo === "etiquetar" ? <DialogoEtiquetas conv={conv} cerrar={() => setDialogo(null)} alHecho={alActualizar} recargar={recargar} /> : null}
            {dialogo === "mencionar" || dialogo === "ayuda" ? <DialogoMencion conv={conv} ayuda={dialogo === "ayuda"} cerrar={() => setDialogo(null)} /> : null}
            <DialogoConfirmar
                open={dialogo === "desasociar"}
                onOpenChange={(a) => !a && setDialogo(null)}
                titulo="¿Quitar el cliente de esta conversación?"
                descripcion="La conversación queda sin cliente, contacto ni prospecto. Puedes volver a asociarlo después."
                textoConfirmar="Quitar"
                peligro
                alConfirmar={async () => {
                    await rapida(
                        "desasociar",
                        async (clave) => await resolver(api().POST(`${BASE}/desasociar`, { params: ruta(conv, clave), body: { version: conv.version } })),
                        () => ({ titulo: `${conv.titulo}: sin cliente asociado` }),
                    );
                    setDialogo(null);
                    void recargar();
                }}
            />
        </>
    );
}

/** Error de una acción de un toque: aviso flotante bajo la cabecera, con «Recargar» si fue un conflicto. */
function ErrorRapido({ error, cerrar, recargar }: { error: unknown; cerrar: () => void; recargar: () => Promise<void> }) {
    const conflicto = error instanceof ErrorApi && error.estado === 409;
    const otro = error instanceof ErrorApi && error.codigo === "ya_asignada" ? (error.errores[0]?.value as string | undefined) : undefined;
    return (
        <div className="absolute inset-x-3 top-[4.25rem] z-10">
            <Aviso
                tono={conflicto ? "warning" : "error"}
                titulo={error instanceof ErrorApi && error.codigo === "ya_asignada" ? `Ya la tomó ${otro ?? "otra persona"}` : conflicto ? "Alguien más cambió esta conversación" : "No se pudo hacer"}
                accion={
                    <Boton
                        variante="secundario"
                        tamano="sm"
                        onClick={() => {
                            cerrar();
                            void recargar();
                        }}
                    >
                        <RotateCw />
                        {conflicto ? "Recargar" : "Cerrar"}
                    </Boton>
                }
            >
                {conflicto ? "Recargamos la conversación para que veas el cambio." : mensajeDeError(error).titulo}
            </Aviso>
        </div>
    );
}

type Envio = <T extends FieldValues>(peticion: (clave: string) => Promise<Conversacion>, campos: readonly Path<T>[], setError: UseFormSetError<T>, aviso: (c: Conversacion) => string) => Promise<void>;

type PropsDialogo = { conv: Conversacion; cerrar: () => void; alHecho: (c: Conversacion) => void; recargar: () => Promise<void> };

/** Diálogo corto (1 a 3 campos) con su envío, conflicto de versión y aviso al terminar. */
function DialogoCorto({ titulo, descripcion, conv, cerrar, alHecho, recargar, children }: PropsDialogo & { titulo: string; descripcion?: string; children: (enviar: Envio, pie: (etiqueta: string, enviando: boolean, faltan?: readonly string[]) => ReactNode) => ReactNode }) {
    const clave = useRef(nuevaClaveIdempotencia());
    const [error, setError] = useState<unknown>(null);
    const enviar: Envio = async (peticion, campos, setErrorCampo, aviso) => {
        setError(null);
        try {
            const c = await peticion(clave.current);
            clave.current = nuevaClaveIdempotencia();
            avisar({ titulo: aviso(c) });
            alHecho(c);
            cerrar();
        } catch (e) {
            clave.current = claveTrasError(e, clave.current);
            if (!erroresACampos(e, campos, setErrorCampo)) setError(e);
        }
    };
    const conflicto = error instanceof ErrorApi && error.estado === 409 && error.codigo === "conflicto_version";
    const pie = (etiqueta: string, enviando: boolean, faltan: readonly string[] = []) => (
        <>
            {conflicto ? (
                <Aviso
                    tono="warning"
                    titulo="Alguien más cambió esta conversación"
                    accion={
                        <Boton
                            variante="secundario"
                            tamano="sm"
                            onClick={async () => {
                                await recargar();
                                setError(null);
                            }}
                        >
                            <RotateCw />
                            Recargar
                        </Boton>
                    }
                >
                    Recarga y vuelve a intentarlo; lo que elegiste se conserva.
                </Aviso>
            ) : error ? (
                <Aviso tono="error" titulo="No se pudo guardar">
                    {[mensajeDeError(error).titulo, mensajeDeError(error).detalle].filter(Boolean).join(". ")}
                </Aviso>
            ) : null}
            <DialogoPie>
                <DialogoCerrar asChild>
                    <Boton variante="contorno" disabled={enviando}>
                        Cancelar
                    </Boton>
                </DialogoCerrar>
                <BotonEnviar enviando={enviando} textoEnviando="Guardando…" disabled={conflicto} faltan={faltan}>
                    {etiqueta}
                </BotonEnviar>
            </DialogoPie>
        </>
    );
    return (
        <DialogoProtegido open onOpenChange={(a) => !a && cerrar()}>
            <DialogoContenido titulo={titulo} descripcion={descripcion ?? conv.titulo}>
                {children(enviar, pie)}
            </DialogoContenido>
        </DialogoProtegido>
    );
}

const esquemaAsignar = z.object({ usuarioId: z.string().min(1, "Elige a quién") });

function DialogoAsignar(props: PropsDialogo) {
    const { conv } = props;
    const { agentes } = useCatalogos();
    const form = useForm({ resolver: zodResolver(esquemaAsignar), defaultValues: { usuarioId: conv.asignado ? String(conv.asignado.id) : "" } });
    useCambiosSinGuardar(form.formState.isDirty, form.formState.isSubmitting);
    const lista = agentes.estado === "listo" ? agentesDeCola(agentes.datos, conv.cola?.id) : [];
    return (
        <DialogoCorto {...props} titulo="Asignar conversación">
            {(enviar, pie) => (
                <form
                    noValidate
                    className="grid gap-4"
                    onSubmit={form.handleSubmit((d) =>
                        enviar(
                            async (clave) => await resolver(api().POST(`${BASE}/asignar`, { params: ruta(conv, clave), body: { version: conv.version, usuarioId: Number(d.usuarioId) } })),
                            ["usuarioId"],
                            form.setError,
                            (c) => `${c.titulo}: asignada a ${c.asignado?.nombre ?? "otra persona"}`,
                        ),
                    )}
                >
                    <CampoSelector etiqueta="Agente" error={form.formState.errors.usuarioId?.message} placeholder={agentes.estado === "cargando" ? "Cargando…" : "Elegir…"} {...form.register("usuarioId")}>
                        <option value="">Elegir…</option>
                        {lista.map((a) => (
                            <option key={a.id} value={String(a.id)}>
                                {a.nombre}
                                {a.supervisa ? " · supervisa" : ""}
                            </option>
                        ))}
                    </CampoSelector>
                    {pie("Asignar", form.formState.isSubmitting)}
                </form>
            )}
        </DialogoCorto>
    );
}

const esquemaTransferir = z.object({
    colaId: z.string().min(1, "Elige la cola"),
    resumen: z.string().trim().min(1, "Escribe un resumen para quien la recibe").max(1000, "Hasta 1000 caracteres"),
    conservar: z.boolean(),
});

function DialogoTransferir(props: PropsDialogo) {
    const { conv } = props;
    const { colas } = useCatalogos();
    const form = useForm({ resolver: zodResolver(esquemaTransferir), defaultValues: { colaId: "", resumen: "", conservar: false } });
    useCambiosSinGuardar(form.formState.isDirty, form.formState.isSubmitting);
    const otras = colas.estado === "listo" ? colas.datos.filter((c) => c.activa && c.id !== conv.cola?.id) : [];
    return (
        <DialogoCorto {...props} titulo="Pasar a otra cola">
            {(enviar, pie) => (
                <form
                    noValidate
                    className="grid gap-4"
                    onSubmit={form.handleSubmit((d) =>
                        enviar(
                            async (clave) =>
                                await resolver(
                                    api().POST(`${BASE}/transferir`, {
                                        params: ruta(conv, clave),
                                        body: { version: conv.version, colaId: Number(d.colaId), resumen: d.resumen.trim(), conservarAsignacion: d.conservar },
                                    }),
                                ),
                            ["colaId", "resumen"],
                            form.setError,
                            (c) => `${c.titulo}: pasó a ${c.cola?.nombre ?? "otra cola"}`,
                        ),
                    )}
                >
                    <CampoSelector etiqueta="Cola" error={form.formState.errors.colaId?.message} {...form.register("colaId")}>
                        <option value="">Elegir…</option>
                        {otras.map((c) => (
                            <option key={c.id} value={String(c.id)}>
                                {c.nombre}
                            </option>
                        ))}
                    </CampoSelector>
                    <CampoArea etiqueta="Resumen" ayuda="Queda como nota interna para quien la recibe." rows={3} maxLength={1000} error={form.formState.errors.resumen?.message} {...form.register("resumen")} />
                    <Interruptor etiqueta="Seguir asignada a quien la tiene" {...form.register("conservar")} />
                    {pie("Pasar", form.formState.isSubmitting)}
                </form>
            )}
        </DialogoCorto>
    );
}

function DialogoEtiquetas(props: PropsDialogo) {
    const { conv } = props;
    const { etiquetas } = useCatalogos();
    const [elegidas, setElegidas] = useState<number[]>(conv.etiquetas.map((e) => e.id));
    const [enviando, setEnviando] = useState(false);
    const sucio = elegidas.length !== conv.etiquetas.length || elegidas.some((id) => !conv.etiquetas.some((e) => e.id === id));
    useCambiosSinGuardar(sucio, enviando);
    const form = useForm();
    return (
        <DialogoCorto {...props} titulo="Etiquetas">
            {(enviar, pie) => (
                <form
                    noValidate
                    className="grid gap-4"
                    onSubmit={form.handleSubmit(async () => {
                        setEnviando(true);
                        await enviar(
                            async (clave) => await resolver(api().POST(`${BASE}/etiquetar`, { params: ruta(conv, clave), body: { version: conv.version, etiquetaIds: elegidas } })),
                            [],
                            form.setError,
                            (c) => `Etiquetas de ${c.titulo} guardadas`,
                        );
                        setEnviando(false);
                    })}
                >
                    <fieldset className="grid max-h-72 gap-1 overflow-y-auto">
                        <legend className="sr-only">Etiquetas de la conversación</legend>
                        {etiquetas.estado === "listo" && etiquetas.datos.length === 0 ? <p className="text-sm text-muted-foreground">No hay etiquetas activas.</p> : null}
                        {(etiquetas.estado === "listo" ? etiquetas.datos : []).map((e) => (
                            <Casilla
                                key={e.id}
                                etiqueta={e.nombre}
                                checked={elegidas.includes(e.id)}
                                disabled={!elegidas.includes(e.id) && elegidas.length >= 10}
                                onChange={(ev) => setElegidas((l) => (ev.target.checked ? [...l, e.id] : l.filter((x) => x !== e.id)))}
                            />
                        ))}
                    </fieldset>
                    <p className={cn("text-xs text-muted-foreground", elegidas.length >= 10 && "text-warning")}>Hasta 10 por conversación.</p>
                    {pie("Guardar", enviando)}
                </form>
            )}
        </DialogoCorto>
    );
}

const esquemaMencion = z.object({
    usuarioId: z.string().min(1, "Elige a quién"),
    texto: z.string().trim().min(1, "Escribe qué necesitas").max(4096, "Hasta 4096 caracteres"),
});

/**
 * Mencionar a un compañero o pedir ayuda al supervisor: una nota interna que lo menciona (lo ve
 * aunque no sea de su cola y le llega un aviso). La conversación sigue con quien la tiene.
 */
function DialogoMencion({ conv, ayuda, cerrar }: { conv: Conversacion; ayuda: boolean; cerrar: () => void }) {
    const { yo } = useSesion();
    const { agentes } = useCatalogos();
    const todos = agentes.estado === "listo" ? agentesDeCola(agentes.datos, conv.cola?.id).filter((a) => a.id !== yo.usuario.id) : [];
    const lista = ayuda ? todos.filter((a) => a.supervisa) : todos;
    const form = useForm({
        resolver: zodResolver(esquemaMencion),
        defaultValues: { usuarioId: ayuda && lista.length === 1 ? String(lista[0]?.id) : "", texto: ayuda ? "¿Me ayudas con esta conversación?" : "" },
    });
    useCambiosSinGuardar(form.formState.isDirty, form.formState.isSubmitting);
    const [id] = useState(nuevoIdMensaje);
    const [error, setError] = useState<unknown>(null);
    return (
        <DialogoProtegido open onOpenChange={(a) => !a && cerrar()}>
            <DialogoContenido titulo={ayuda ? "Pedir ayuda al supervisor" : "Mencionar a un compañero"} descripcion={`${conv.titulo} · le llega un aviso y lo ve en una nota interna`}>
                <form
                    noValidate
                    className="grid gap-4"
                    onSubmit={form.handleSubmit(async (d) => {
                        setError(null);
                        try {
                            await resolver(
                                api().POST(`${BASE}/notas`, {
                                    params: { path: { id: conv.id }, header: cabeceraIdem(id) },
                                    body: { id, texto: d.texto.trim(), menciones: [Number(d.usuarioId)] },
                                }),
                            );
                            const nombre = lista.find((a) => String(a.id) === d.usuarioId)?.nombre ?? "tu compañero";
                            avisar({ titulo: ayuda ? `Pediste ayuda a ${nombre}` : `Mencionaste a ${nombre}` });
                            cerrar();
                        } catch (e) {
                            if (!erroresACampos(e, ["usuarioId", "texto"], form.setError)) setError(e);
                        }
                    })}
                >
                    <CampoSelector etiqueta={ayuda ? "Supervisor" : "Compañero"} error={form.formState.errors.usuarioId?.message} {...form.register("usuarioId")}>
                        <option value="">Elegir…</option>
                        {lista.map((a) => (
                            <option key={a.id} value={String(a.id)}>
                                {a.nombre}
                            </option>
                        ))}
                    </CampoSelector>
                    {ayuda && agentes.estado === "listo" && lista.length === 0 ? <Aviso tono="warning" titulo="No hay supervisores con acceso a conversaciones" /> : null}
                    <CampoArea etiqueta="Nota interna" ayuda="El cliente no la ve." rows={3} maxLength={4096} error={form.formState.errors.texto?.message} {...form.register("texto")} />
                    {error ? (
                        <Aviso tono="error" titulo="No se pudo enviar">
                            {mensajeDeError(error).titulo}
                        </Aviso>
                    ) : null}
                    <DialogoPie>
                        <DialogoCerrar asChild>
                            <Boton variante="contorno" disabled={form.formState.isSubmitting}>
                                Cancelar
                            </Boton>
                        </DialogoCerrar>
                        <BotonEnviar enviando={form.formState.isSubmitting} textoEnviando="Enviando…">
                            {ayuda ? "Pedir ayuda" : "Mencionar"}
                        </BotonEnviar>
                    </DialogoPie>
                </form>
            </DialogoContenido>
        </DialogoProtegido>
    );
}
