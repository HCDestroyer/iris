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
    ListaEleccion,
    MenuDesplegable,
    MenuDesplegableContenido,
    MenuDesplegableDisparador,
    MenuDesplegableElemento,
    MenuDesplegableSeparador,
    useCambiosSinGuardar,
} from "@iris/ui";
import { ArrowLeftRight, AtSign, BookOpen, Building2, Check, ChevronDown, Ellipsis, LifeBuoy, Lock, RotateCcw, RotateCw, Tag, Unlink, User, UserCheck, UserMinus, UserPlus, X } from "lucide-react";
import { useEffect, useRef, useState, type ReactNode } from "react";
import { useForm, useWatch, type FieldValues, type Path, type UseFormSetError } from "react-hook-form";
import { z } from "zod";
import { api, nuevaClaveIdempotencia, resolver } from "@/lib/api/cliente";
import { ErrorApi, mensajeDeError } from "@/lib/api/errores";
import { cabeceraIdem } from "@/lib/conversaciones/api";
import { useIdEnvio } from "@/lib/conversaciones/id-envio";
import type { Conversacion } from "@/lib/conversaciones/tipos";
import { erroresACampos } from "@/lib/formularios";
import { useClaveIdempotencia } from "@/lib/idempotencia";
import { useSesion } from "@/lib/sesion";
import { pasarConversacion } from "@/lib/conversaciones/api-equipo";
import { EN_OTRA_EMPRESA, esEnOtraEmpresa, estaDisponible, personasDeCola, supervisaCola, textoCarga, textoMotivoNoDisponible } from "@/lib/conversaciones/atencion";
import { leerEmpresasDestino, pasarAEmpresa } from "@/lib/meta/api";
import { esquemaPasarEmpresa } from "@/lib/meta/esquemas";
import type { EmpresaDestino } from "@/lib/meta/tipos";
import type { Schema } from "@iris/contratos";
import { agentesDeCola, useCatalogos } from "./catalogos";
import { AvatarDisponible, EstadoDisponible } from "./disponibilidad";

type Dialogo = "asignar" | "pasar" | "pasar-empresa" | "transferir" | "etiquetar" | "mencionar" | "ayuda" | "desasociar" | null;

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
    const { agentes } = useCatalogos();
    const yoAgente = agentes.estado === "listo" ? agentes.datos.find((a) => a.id === yo.usuario.id) : undefined;
    const [dialogo, setDialogo] = useState<Dialogo>(null);
    const [ocupado, setOcupado] = useState(false);
    const [errorRapido, setErrorRapido] = useState<unknown>(null);
    const mia = conv.asignado?.id === yo.usuario.id;
    const resuelta = conv.estado === "resuelta";
    const p = {
        tomar: puede(["conversaciones.tomar"]) && !conv.asignado,
        // Desde la 1.1 también asigna el supervisor de la cola (§14.1).
        asignar: puede(["conversaciones.asignar"]) || supervisaCola(yoAgente, conv.cola?.id),
        transferir: puede(["conversaciones.transferir"]),
        etiquetar: puede(["conversaciones.etiquetar"]),
        resolver: puede(["conversaciones.resolver"]),
        mencionar: puede(["conversaciones.responder"]),
        asociar: puede(["conversaciones.asociar"]) && conv.contacto.tipo !== "desconocido",
        // «Pasar a…» (§14.4): quien la tiene, o un supervisor global o de su cola.
        pasar:
            puede(["conversaciones.pasar"]) &&
            !resuelta &&
            (mia || puede(["conversaciones.supervisar"]) || supervisaCola(yoAgente, conv.cola?.id)),
    };

    const empresasDestino = useEmpresasParaPasar(conv);

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

    // §15.3: préstamo revocado o pasada a otra empresa: no hay acciones (el servidor responde 422).
    if (conv.soloLectura) {
        return (
            <span className="inline-flex shrink-0 items-center gap-1.5 rounded-full border border-border bg-background-secondary px-2.5 py-1 text-xs font-medium text-muted-foreground" data-solo-lectura>
                <Lock aria-hidden className="size-3.5" />
                Solo lectura
            </span>
        );
    }

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
                    <span className="truncate @max-[32rem]/conv:hidden">{nombreAsignado}</span>
                    <ChevronDown className="@max-[32rem]/conv:hidden" />
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
                                    <span className="@max-[36rem]/conv:hidden">Ayuda</span>
                                </Boton>
                            </MenuDesplegableDisparador>
                            <MenuDesplegableContenido align="end">{elementosAyuda}</MenuDesplegableContenido>
                        </MenuDesplegable>
                    )}
                    {p.resolver && !estrecho ? (
                            <Boton variante="fantasma" tamano="sm" className="h-8 rounded-none px-2.5" onClick={() => void (resuelta ? reabrir() : resolverConv())} disabled={ocupado} aria-label={resuelta ? "Reabrir" : "Resolver"}>
                                {resuelta ? <RotateCcw /> : <Check />}
                                <span className="@max-[36rem]/conv:hidden">{resuelta ? "Reabrir" : "Resolver"}</span>
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
                            {p.asignar ? (
                                <MenuDesplegableElemento icono={UserPlus} onSelect={() => setDialogo("asignar")} textValue="Asignar…" className="h-auto py-2">
                                    <OpcionConDetalle titulo="Asignar…" detalle={`A cualquiera de ${conv.cola ? `la cola ${conv.cola.nombre}` : "la empresa"}.`} />
                                </MenuDesplegableElemento>
                            ) : null}
                            {p.pasar ? (
                                <MenuDesplegableElemento icono={ArrowLeftRight} onSelect={() => setDialogo("pasar")} textValue="Pasar a…" className="h-auto py-2">
                                    <OpcionConDetalle titulo="Pasar a…" detalle={`A un compañero de ${conv.cola?.nombre ?? "la empresa"}, con una nota.`} />
                                </MenuDesplegableElemento>
                            ) : null}
                            {empresasDestino.length > 0 ? (
                                <MenuDesplegableElemento icono={Building2} onSelect={() => setDialogo("pasar-empresa")} textValue="Pasar a otra empresa" className="h-auto py-2">
                                    <OpcionConDetalle titulo="Pasar a otra empresa" detalle={`A ${empresasDestino.map((e) => e.nombre).join(" o ")}, con los mensajes del cliente.`} />
                                </MenuDesplegableElemento>
                            ) : null}
                            {p.asignar || p.pasar || empresasDestino.length > 0 ? <MenuDesplegableSeparador /> : null}
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
            {dialogo === "pasar" ? <DialogoPasar conv={conv} cerrar={() => setDialogo(null)} alHecho={alActualizar} recargar={recargar} /> : null}
            {dialogo === "pasar-empresa" ? <DialogoPasarEmpresa conv={conv} empresas={empresasDestino} cerrar={() => setDialogo(null)} alHecho={alActualizar} recargar={recargar} /> : null}
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

/** Opción del menú con una línea que explica a quién llega. */
function OpcionConDetalle({ titulo, detalle }: { titulo: string; detalle: string }) {
    return (
        <span className="grid min-w-0">
            <span className="font-medium">{titulo}</span>
            <span className="text-xs text-muted-foreground">{detalle}</span>
        </span>
    );
}

/** Error de una acción de un toque: aviso flotante bajo la cabecera, con «Recargar» si fue un conflicto. */
function ErrorRapido({ error, cerrar, recargar }: { error: unknown; cerrar: () => void; recargar: () => Promise<void> }) {
    // Otra empresa del número la atiende: reintentar no cambia nada, solo se explica y se cierra.
    if (esEnOtraEmpresa(error)) {
        return (
            <div className="absolute inset-x-3 top-[4.25rem] z-10" data-error-rapido="conversacion_en_otra_empresa">
                <Aviso
                    tono="warning"
                    titulo={EN_OTRA_EMPRESA.titulo}
                    accion={
                        <Boton variante="secundario" tamano="sm" onClick={cerrar}>
                            <X />
                            Cerrar
                        </Boton>
                    }
                >
                    {EN_OTRA_EMPRESA.texto}
                </Aviso>
            </div>
        );
    }
    const conflicto = error instanceof ErrorApi && error.estado === 409;
    const otro = error instanceof ErrorApi && error.codigo === "ya_asignada" ? error.detalle : undefined;
    return (
        <div className="absolute inset-x-3 top-[4.25rem] z-10">
            <Aviso
                tono={conflicto ? "warning" : "error"}
                titulo={error instanceof ErrorApi && error.codigo === "ya_asignada" ? "Ya la tomó otra persona" : conflicto ? "Alguien más cambió esta conversación" : "No se pudo hacer"}
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
                {otro ?? (conflicto ? "Recargamos la conversación para que veas el cambio." : mensajeDeError(error).titulo)}
            </Aviso>
        </div>
    );
}

type Envio = <T extends FieldValues>(peticion: (clave: string) => Promise<Conversacion>, campos: readonly Path<T>[], setError: UseFormSetError<T>, aviso: (c: Conversacion) => string) => Promise<void>;

type PropsDialogo = { conv: Conversacion; cerrar: () => void; alHecho: (c: Conversacion) => void; recargar: () => Promise<void> };

/** Diálogo corto (1 a 3 campos) con su envío, conflicto de versión y aviso al terminar. */
function DialogoCorto({ titulo, descripcion, conv, cerrar, alHecho, recargar, children }: PropsDialogo & { titulo: string; descripcion?: string; children: (enviar: Envio, pie: (etiqueta: string, enviando: boolean, faltan?: readonly string[]) => ReactNode) => ReactNode }) {
    const clave = useClaveIdempotencia();
    const [error, setError] = useState<unknown>(null);
    const enviar: Envio = async (peticion, campos, setErrorCampo, aviso) => {
        setError(null);
        try {
            const c = await peticion(clave.actual());
            clave.renovar();
            avisar({ titulo: aviso(c) });
            alHecho(c);
            cerrar();
        } catch (e) {
            clave.trasError(e);
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
    const [elegidas, setElegidas] = useState<number[]>((conv.etiquetas ?? []).map((e) => e.id));
    const [enviando, setEnviando] = useState(false);
    const sucio = elegidas.length !== (conv.etiquetas ?? []).length || elegidas.some((id) => !(conv.etiquetas ?? []).some((e) => e.id === id));
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
    const envio = useIdEnvio();
    const [error, setError] = useState<unknown>(null);
    return (
        <DialogoProtegido open onOpenChange={(a) => !a && cerrar()}>
            <DialogoContenido titulo={ayuda ? "Pedir ayuda al supervisor" : "Mencionar a un compañero"} descripcion={`${conv.titulo} · le llega un aviso y lo ve en una nota interna`}>
                <form
                    noValidate
                    className="grid gap-4"
                    onSubmit={form.handleSubmit(async (d) => {
                        setError(null);
                        const id = envio.actual();
                        try {
                            await resolver(
                                api().POST(`${BASE}/notas`, {
                                    params: { path: { id: conv.id }, header: cabeceraIdem(id) },
                                    body: { id, texto: d.texto.trim(), menciones: [Number(d.usuarioId)] },
                                }),
                            );
                            const nombre = lista.find((a) => String(a.id) === d.usuarioId)?.nombre ?? "tu compañero";
                            envio.renovar();
                            avisar({ titulo: ayuda ? `Pediste ayuda a ${nombre}` : `Mencionaste a ${nombre}` });
                            cerrar();
                        } catch (e) {
                            envio.trasError(e);
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

const esquemaPasar = z.object({
    usuarioId: z.string().min(1, "Elige a quién pasarla"),
    nota: z.string().trim().max(1000, "Hasta 1000 caracteres"),
});

/**
 * «Pasar a…» (§14.4): un compañero de la cola con su punto de disponibilidad y sus conversaciones
 * abiertas, y una nota opcional. Se puede pasar a alguien «No disponible»: se avisa que la verá
 * cuando vuelva a estar disponible.
 */
function DialogoPasar(props: PropsDialogo) {
    const { conv } = props;
    const { yo } = useSesion();
    const { agentes } = useCatalogos();
    const refrescar = agentes.refrescar;
    // La disponibilidad no llega por el flujo: se relee al abrir el diálogo.
    useEffect(() => void refrescar(), [refrescar]);
    const form = useForm({ resolver: zodResolver(esquemaPasar), defaultValues: { usuarioId: "", nota: "" } });
    useCambiosSinGuardar(form.formState.isDirty, form.formState.isSubmitting);
    const elegido = useWatch({ control: form.control, name: "usuarioId" });
    const resultado = useRef<Schema<"PaseConversacion"> | null>(null);
    const lista = agentes.estado === "listo" ? personasDeCola(agentes.datos, conv.cola?.id).filter((a) => a.id !== conv.asignado?.id && a.id !== yo.usuario.id) : [];
    const destino = lista.find((a) => String(a.id) === elegido);
    const primero = destino?.nombre.split(" ")[0];
    const cola = conv.cola?.nombre ?? "la empresa";
    const mia = conv.asignado?.id === yo.usuario.id;
    const descripcion = mia
        ? `${conv.titulo} sigue en ${cola} y deja de ser tuya.`
        : conv.asignado
          ? `${conv.titulo} sigue en ${cola} y deja de ser de ${conv.asignado.nombre}.`
          : `${conv.titulo} sigue en ${cola}.`;
    return (
        <DialogoCorto {...props} titulo="Pasar a…" descripcion={descripcion}>
            {(enviar, pie) => (
                <form
                    noValidate
                    aria-label="Pasar a…"
                    className="grid gap-4"
                    onSubmit={form.handleSubmit((d) =>
                        enviar(
                            async (clave) => {
                                const nota = d.nota.trim();
                                const r = await pasarConversacion(conv.id, { version: conv.version, usuarioId: Number(d.usuarioId), ...(nota ? { nota } : {}) }, clave);
                                resultado.current = r;
                                return r.conversacion;
                            },
                            ["usuarioId", "nota"],
                            form.setError,
                            (c) => {
                                const r = resultado.current;
                                const nombre = r?.destino.nombre ?? destino?.nombre ?? "tu compañero";
                                return r && !r.destinoDisponible ? `${c.titulo}: pasó a ${nombre}. La verá cuando vuelva a estar disponible` : `${c.titulo}: pasó a ${nombre}`;
                            },
                        ),
                    )}
                >
                    {agentes.estado === "error" ? (
                        <Aviso
                            tono="error"
                            titulo="No se pudo leer el equipo"
                            accion={
                                <Boton variante="secundario" tamano="sm" onClick={agentes.reintentar}>
                                    <RotateCw />
                                    Reintentar
                                </Boton>
                            }
                        />
                    ) : agentes.estado === "cargando" ? (
                        <p className="text-sm text-muted-foreground" role="status">
                            Leyendo el equipo…
                        </p>
                    ) : lista.length === 0 ? (
                        <Aviso tono="warning" titulo={`No hay más personas en ${cola}`}>
                            Agrega miembros a la cola en Administración.
                        </Aviso>
                    ) : (
                        <ListaEleccion
                            leyenda={`Compañero de ${cola}`}
                            nombre="usuarioId"
                            valor={elegido || undefined}
                            alCambiar={(v) => form.setValue("usuarioId", v, { shouldDirty: true, shouldValidate: form.formState.isSubmitted })}
                            error={form.formState.errors.usuarioId?.message}
                            className="max-h-[min(18rem,40dvh)] overflow-y-auto"
                            opciones={lista.map((a) => {
                                const disponible = estaDisponible(a);
                                const supervisa = supervisaCola(a, conv.cola?.id) && !(a.colas ?? []).some((c) => c.id === conv.cola?.id);
                                const detalle = disponible ? textoCarga(a, supervisa) : [textoMotivoNoDisponible(a.disponibilidad.origen), textoCarga(a, supervisa)].filter(Boolean).join(" · ");
                                return {
                                    valor: String(a.id),
                                    texto: `${a.nombre}, ${disponible ? "disponible" : "no disponible"}`,
                                    atenuada: !disponible,
                                    contenido: (
                                        <>
                                            <AvatarDisponible nombre={a.nombre} disponible={disponible} />
                                            <span className="grid min-w-0">
                                                <span className="truncate font-semibold">{a.nombre}</span>
                                                {detalle ? <span className="truncate text-xs text-muted-foreground">{detalle}</span> : null}
                                            </span>
                                        </>
                                    ),
                                    fin: <EstadoDisponible disponible={disponible} className="max-[24rem]:sr-only" />,
                                };
                            })}
                        />
                    )}
                    {destino && !estaDisponible(destino) ? (
                        <Aviso tono="warning" titulo={`${primero} está No disponible`}>
                            La verá cuando vuelva a estar disponible.
                        </Aviso>
                    ) : null}
                    <CampoArea
                        etiqueta="Nota (opcional)"
                        ayuda={`Queda como nota interna${primero ? ` para ${primero}` : ""}.`}
                        rows={2}
                        maxLength={1000}
                        error={form.formState.errors.nota?.message}
                        {...form.register("nota")}
                    />
                    {pie(primero ? `Pasar a ${primero}` : "Pasar", form.formState.isSubmitting, elegido ? [] : ["a quién pasarla"])}
                </form>
            )}
        </DialogoCorto>
    );
}

/**
 * Empresas a las que se puede pasar esta conversación (contrato §15.3): las da el servidor
 * (`GET /conversaciones/{id}/empresas-destino`, con `conversaciones.transferir`); si la empresa no es
 * la dueña del número, la lista viene vacía. La opción solo aparece con la lista no vacía; si la
 * lectura falla, tampoco: nunca se ofrece una empresa que no se pudo comprobar.
 */
function useEmpresasParaPasar(conv: Conversacion): EmpresaDestino[] {
    const { puede } = useSesion();
    const habilitado = puede(["conversaciones.transferir"]) && !conv.soloLectura;
    const id = conv.id;
    const [empresas, setEmpresas] = useState<{ id: number; lista: EmpresaDestino[] } | null>(null);
    useEffect(() => {
        if (!habilitado) return undefined;
        const control = new AbortController();
        leerEmpresasDestino(id, control.signal).then(
            (lista) => setEmpresas({ id, lista }),
            () => undefined,
        );
        return () => control.abort();
    }, [habilitado, id]);
    if (!habilitado || empresas?.id !== id) return [];
    return empresas.lista;
}

/**
 * «Pasar a otra empresa» (§15.3): pasan los mensajes del cliente y los enviados (no las notas
 * internas ni los eventos de la dueña) a la cola del préstamo, sin asignar, sin etiquetas y sin contacto; desde ahí solo la ve la otra empresa (aquí queda de
 * solo lectura). Nota opcional para quien la recibe.
 */
function DialogoPasarEmpresa(props: PropsDialogo & { empresas: EmpresaDestino[] }) {
    const { conv, empresas } = props;
    const form = useForm({ resolver: zodResolver(esquemaPasarEmpresa), defaultValues: { empresaId: empresas.length === 1 ? String(empresas[0]?.empresaId ?? "") : "", nota: "" } });
    useCambiosSinGuardar(form.formState.isDirty, form.formState.isSubmitting);
    const empresaId = useWatch({ control: form.control, name: "empresaId" });
    const elegida = empresas.find((e) => String(e.empresaId) === empresaId);
    return (
        <DialogoCorto {...props} titulo="Pasar a otra empresa" descripcion={`${conv.titulo}: pasan los mensajes con el cliente y aquí queda de solo lectura.`}>
            {(enviar, pie) => (
                <form
                    noValidate
                    className="grid gap-4"
                    onSubmit={form.handleSubmit((d) =>
                        enviar(
                            async (clave) => {
                                try {
                                    return await pasarAEmpresa(conv.id, { version: conv.version, empresaId: Number(d.empresaId), ...(d.nota ? { nota: d.nota } : {}) }, clave);
                                } catch (e) {
                                    if (e instanceof ErrorApi && e.codigo === "conversacion_existente") form.setError("empresaId", { message: "Esa empresa ya tiene una conversación con este cliente por este número" });
                                    if (e instanceof ErrorApi && e.codigo === "conversacion_con_vinculos") form.setError("empresaId", { message: "Tiene tickets, órdenes, prospectos, boletas o promesas: no se puede pasar" });
                                    throw e;
                                }
                            },
                            ["empresaId", "nota"],
                            form.setError,
                            () => `Conversación con ${conv.titulo} pasada a ${elegida?.nombre ?? "otra empresa"}`,
                        ),
                    )}
                >
                    <CampoSelector etiqueta="Empresa" error={form.formState.errors.empresaId?.message} {...form.register("empresaId")}>
                        <option value="">Elegir…</option>
                        {empresas.map((e) => (
                            <option key={e.empresaId} value={String(e.empresaId)}>
                                {e.nombre}
                            </option>
                        ))}
                    </CampoSelector>
                    <CampoArea etiqueta="Nota para quien la recibe (opcional)" rows={3} maxLength={1000} error={form.formState.errors.nota?.message} {...form.register("nota")} />
                    <p className="text-sm text-muted-foreground">Se pasa sin asignar, sin etiquetas, sin el cliente asociado y sin las notas internas: esos datos son de tu empresa. La nota de aquí es lo único interno que verán.</p>
                    {pie(elegida ? `Pasar a ${elegida.nombre}` : "Pasar", form.formState.isSubmitting, empresaId ? [] : ["la empresa"])}
                </form>
            )}
        </DialogoCorto>
    );
}
