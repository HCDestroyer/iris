"use client";

import { zodResolver } from "@hookform/resolvers/zod";
import { Aviso, Boton, Campo, CampoBusqueda, cn, Dialogo, DialogoCerrar, DialogoContenido, DialogoPie, Selector } from "@iris/ui";
import { BookmarkPlus, Plus, X } from "lucide-react";
import Link from "next/link";
import { useRouter, useSearchParams } from "next/navigation";
import { useId, useState, type ReactNode } from "react";
import { useForm } from "react-hook-form";
import { z } from "zod";
import { useCategorias, useUsuarios } from "@/componentes/tickets/catalogos";
import { api, resolver } from "@/lib/api/cliente";
import { useDiferido } from "@/lib/diferido";
import { useLectura } from "@/lib/lectura";
import { useSesion } from "@/lib/sesion";
import {
    consultaDeFiltros,
    FILTROS_INICIALES,
    guardarVistas,
    leerVistas,
    mismosFiltros,
    VISTAS_PREDEFINIDAS,
    vistaDeUrl,
    type Filtros,
    type Vista,
} from "@/lib/tickets/filtros";
import { ESTADOS, ORDEN_ESTADOS, ORDEN_PRIORIDADES, PRIORIDADES } from "@/lib/tickets/presentacion";
import { contadorDe, textoContador, useResumenTickets } from "@/lib/tickets/resumen";
import { ListaTickets } from "./lista-tickets";

/**
 * `?clienteId=` (desde el buscador global): los tickets de un cliente de Argos. Solo el id viene
 * de la URL; el nombre se lee de la API (que filtra por empresa), nunca de un parámetro que
 * cualquiera puede armar.
 */
function clienteDeParametros(params: URLSearchParams): number | null {
    const id = Number(params.get("clienteId"));
    return Number.isSafeInteger(id) && id > 0 ? id : null;
}

/** Bandeja de tickets; con un cliente en la URL, la historia de ese cliente (todos los estados). */
export function PantallaTickets() {
    const params = useSearchParams();
    const clienteId = clienteDeParametros(params);
    // `?vista=` (Inicio, «Mis vistas»): la bandeja arranca con esa vista; cambiarla en la URL la remonta.
    const vista = params.get("vista");
    return <Bandeja key={`${clienteId ?? "todos"}|${vista ?? ""}`} clienteId={clienteId} vistaInicial={vista} />;
}

/** Aviso del filtro por cliente: «Cliente #id» hasta leer el nombre, o si no se pudo leer. */
function AvisoCliente({ clienteId, alQuitar }: { clienteId: number; alQuitar: () => void }) {
    const ficha = useLectura((signal) => resolver(api().GET("/api/v1/clientes/{id}", { params: { path: { id: clienteId } }, signal })));
    const nombre = ficha.estado === "listo" ? ficha.datos.cliente.nombre : `Cliente #${clienteId}`;
    return (
        <Aviso
            tono="info"
            titulo={`Tickets de ${nombre}`}
            data-testid="filtro-cliente"
            accion={
                <Boton variante="secundario" tamano="sm" onClick={alQuitar}>
                    <X />
                    Ver todos los tickets
                </Boton>
            }
        >
            Solo los de este cliente, en cualquier estado.
        </Aviso>
    );
}

/** Filtro como chip: una lista nativa con su nombre delante («Prioridad · Todas»). */
function Chip({ etiqueta, activo, children }: { etiqueta: string; activo: boolean; children: (id: string) => ReactNode }) {
    const id = useId();
    return (
        <span
            className={cn(
                "inline-flex h-8 min-w-0 items-center gap-1 rounded-lg border pl-2.5 text-xs font-medium",
                activo ? "border-primary/40 bg-primary/5 text-foreground" : "border-border bg-card text-muted-foreground",
            )}
            data-chip={etiqueta}
        >
            <label htmlFor={id} className="shrink-0">
                {etiqueta}
            </label>
            {children(id)}
        </span>
    );
}

const claseSelectorChip =
    "w-auto [&_button]:h-[1.875rem] [&_button]:min-h-0 [&_button]:w-auto [&_button]:border-0 [&_button]:bg-transparent [&_button]:pl-1 [&_button]:text-xs [&_button]:font-semibold [&_button]:text-foreground [&_button]:shadow-none [&_[data-slot=select-value]]:text-xs";

function Bandeja({ clienteId, vistaInicial }: { clienteId: number | null; vistaInicial: string | null }) {
    const { yo, puede } = useSesion();
    const router = useRouter();
    const categorias = useCategorias();
    const usuarios = useUsuarios();
    const resumen = useResumenTickets();
    const [propias, setPropias] = useState<Vista[]>(() => leerVistas(yo.usuario.id));
    const [filtros, setFiltros] = useState<Filtros>(() => {
        if (clienteId) return { ...FILTROS_INICIALES, estado: "todos" };
        return vistaDeUrl(vistaInicial, leerVistas(yo.usuario.id))?.filtros ?? FILTROS_INICIALES;
    });
    const [guardando, setGuardando] = useState(false);
    const idBuscar = useId();
    const q = useDiferido(filtros.q, 350);

    const efectivos: Filtros = { ...filtros, q };
    const consulta = { ...consultaDeFiltros(efectivos, yo.usuario.id), ...(clienteId ? { clienteId } : {}) };
    const vistas: (Vista & { contador?: (typeof VISTAS_PREDEFINIDAS)[number]["contador"] })[] = [...VISTAS_PREDEFINIDAS, ...propias];
    const vistaActiva = vistas.find((v) => mismosFiltros(v.filtros, filtros));
    const cambiar = (parcial: Partial<Filtros>) => setFiltros((f) => ({ ...f, ...parcial }));

    function guardarPropias(nuevas: Vista[]) {
        setPropias(nuevas);
        guardarVistas(yo.usuario.id, nuevas);
    }

    const listaCategorias = categorias.estado === "listo" ? categorias.datos.filter((c) => c.activa) : [];

    return (
        <div className="grid gap-4 [&>*]:min-w-0">
            <div className="flex flex-wrap items-center gap-2.5">
                <div className="mr-auto grid gap-1">
                    <h1 className="text-[1.375rem] font-semibold tracking-[-0.015em]">Tickets</h1>
                    <p className="text-[0.8125rem] text-muted-foreground">Toca un ticket para abrir su ficha: asignar, escalar y resolver se hacen ahí.</p>
                </div>
                {puede(["tickets.crear"]) ? (
                    <Boton asChild tamano="sm" className="h-[2.125rem] rounded-lg">
                        <Link href="/tickets/nuevo">
                            <Plus />
                            Nuevo ticket
                        </Link>
                    </Boton>
                ) : null}
            </div>

            {clienteId ? <AvisoCliente clienteId={clienteId} alQuitar={() => router.replace("/tickets")} /> : null}

            <div className="flex items-end gap-5 overflow-x-auto border-b border-border" role="group" aria-label="Vistas">
                {vistas.map((v) => {
                    const activa = vistaActiva?.id === v.id;
                    const n = v.contador && !clienteId ? contadorDe(resumen, v.contador) : undefined;
                    return (
                        <span key={v.id} className="inline-flex shrink-0 items-center">
                            <Boton
                                variante="fantasma"
                                aria-pressed={activa}
                                className={cn(
                                    "-mb-px h-10 gap-1.5 rounded-none border-b-2 px-0 text-[0.8125rem] hover:bg-transparent",
                                    activa
                                        ? "border-primary font-semibold text-foreground"
                                        : "border-transparent font-medium text-muted-foreground hover:text-foreground",
                                )}
                                onClick={() => setFiltros(v.filtros)}
                            >
                                {v.nombre}
                                {n !== undefined ? (
                                    <span
                                        className="inline-flex h-[1.125rem] items-center rounded-full bg-elevated px-1.5 text-[0.6875rem] font-semibold text-muted-foreground tabular-nums"
                                        aria-label={`, ${n}`}
                                    >
                                        {textoContador(n)}
                                    </span>
                                ) : null}
                            </Boton>
                            {v.propia ? (
                                <Boton
                                    variante="fantasma"
                                    tamano="icono-sm"
                                    className="ml-0.5 size-6 text-muted-foreground"
                                    aria-label={`Eliminar la vista «${v.nombre}»`}
                                    title="Eliminar vista"
                                    onClick={() => guardarPropias(propias.filter((p) => p.id !== v.id))}
                                >
                                    <X />
                                </Boton>
                            ) : null}
                        </span>
                    );
                })}
                <Boton
                    variante="fantasma"
                    tamano="sm"
                    className="mb-1 ml-auto shrink-0 text-[0.8125rem] text-muted-foreground"
                    disabled={vistaActiva !== undefined}
                    onClick={() => setGuardando(true)}
                >
                    <BookmarkPlus />
                    Guardar vista
                </Boton>
            </div>

            <section aria-label="Lista" className="min-w-0 overflow-hidden rounded-xl border border-border bg-card">
                <div className="flex flex-wrap items-center gap-2 border-b border-border px-3 py-2.5" role="group" aria-label="Filtros">
                    <CampoBusqueda
                        id={idBuscar}
                        etiqueta="Buscar"
                        etiquetaVisible={false}
                        tamano="sm"
                        className="flex-[1_1_14rem] sm:max-w-72"
                        placeholder="Número, asunto o contacto"
                        maxLength={120}
                        valor={filtros.q}
                        alCambiar={(q) => cambiar({ q })}
                    />
                    <Chip etiqueta="Estado" activo={filtros.estado !== FILTROS_INICIALES.estado}>
                        {(id) => (
                            <Selector
                                id={id}
                                tamano="sm"
                                className={claseSelectorChip}
                                value={filtros.estado}
                                onChange={(e) => cambiar({ estado: e.target.value as Filtros["estado"] })}
                            >
                                <option value="activos">Activos</option>
                                {ORDEN_ESTADOS.map((e) => (
                                    <option key={e} value={e}>
                                        {ESTADOS[e].texto}
                                    </option>
                                ))}
                                <option value="todos">Todos</option>
                            </Selector>
                        )}
                    </Chip>
                    <Chip etiqueta="Prioridad" activo={filtros.prioridad !== "todas"}>
                        {(id) => (
                            <Selector
                                id={id}
                                tamano="sm"
                                className={claseSelectorChip}
                                value={filtros.prioridad}
                                onChange={(e) => cambiar({ prioridad: e.target.value as Filtros["prioridad"] })}
                            >
                                <option value="todas">Todas</option>
                                {[...ORDEN_PRIORIDADES].reverse().map((p) => (
                                    <option key={p} value={p}>
                                        {PRIORIDADES[p].texto}
                                    </option>
                                ))}
                            </Selector>
                        )}
                    </Chip>
                    <Chip etiqueta="Categoría" activo={filtros.categoria !== "todas"}>
                        {(id) => (
                            <Selector
                                id={id}
                                tamano="sm"
                                className={claseSelectorChip}
                                value={String(filtros.categoria)}
                                disabled={categorias.estado !== "listo"}
                                title={categorias.estado === "error" ? "No se pudieron cargar las categorías." : undefined}
                                onChange={(e) => cambiar({ categoria: e.target.value === "todas" ? "todas" : Number(e.target.value) })}
                            >
                                <option value="todas">Todas</option>
                                {listaCategorias.map((c) => (
                                    <option key={c.id} value={c.id}>
                                        {c.nombre}
                                    </option>
                                ))}
                            </Selector>
                        )}
                    </Chip>
                    <Chip etiqueta="Asignación" activo={filtros.asignacion !== "todos"}>
                        {(id) => (
                            <Selector
                                id={id}
                                tamano="sm"
                                className={claseSelectorChip}
                                value={filtros.asignacion}
                                onChange={(e) => cambiar({ asignacion: e.target.value as Filtros["asignacion"] })}
                            >
                                <option value="todos">Todos</option>
                                <option value="mios">Asignados a mí</option>
                                <option value="sin_asignar">Sin asignar</option>
                            </Selector>
                        )}
                    </Chip>
                    {!mismosFiltros(filtros, FILTROS_INICIALES) ? (
                        <Boton variante="fantasma" tamano="sm" className="h-8 text-xs text-muted-foreground" onClick={() => setFiltros(FILTROS_INICIALES)}>
                            <X />
                            Quitar filtros
                        </Boton>
                    ) : null}
                    {categorias.estado === "error" ? <span className="text-xs text-muted-foreground">No se pudieron cargar las categorías.</span> : null}
                </div>

                <ListaTickets
                    key={JSON.stringify(consulta)}
                    consulta={consulta}
                    categorias={categorias.estado === "listo" ? categorias.datos : undefined}
                    usuarios={usuarios.estado === "listo" ? usuarios.datos : undefined}
                    yo={{ id: yo.usuario.id, nombre: yo.usuario.nombre }}
                    hayFiltros={clienteId !== null || !mismosFiltros(efectivos, FILTROS_INICIALES)}
                    alCambiar={() => void resumen?.refrescar()}
                />
            </section>

            <DialogoGuardarVista
                abierto={guardando}
                alCerrar={() => setGuardando(false)}
                alGuardar={(nombre) => {
                    guardarPropias([...propias, { id: `v-${Date.now().toString(36)}`, nombre, filtros, propia: true }]);
                    setGuardando(false);
                }}
            />
        </div>
    );
}

const esquemaVista = z.object({
    nombre: z.string().trim().min(1, "Escribe un nombre").max(40, "Admite hasta 40 caracteres"),
});

function DialogoGuardarVista({ abierto, alCerrar, alGuardar }: { abierto: boolean; alCerrar: () => void; alGuardar: (nombre: string) => void }) {
    const form = useForm<z.input<typeof esquemaVista>, unknown, z.output<typeof esquemaVista>>({
        resolver: zodResolver(esquemaVista),
        defaultValues: { nombre: "" },
    });
    return (
        <Dialogo
            open={abierto}
            onOpenChange={(a) => {
                if (!a) {
                    form.reset();
                    alCerrar();
                }
            }}
        >
            <DialogoContenido titulo="Guardar vista" descripcion="Guarda los filtros actuales en este navegador. Aparece en «Mis vistas».">
                <form
                    noValidate
                    className="grid gap-4"
                    onSubmit={form.handleSubmit((d) => {
                        alGuardar(d.nombre);
                        form.reset();
                    })}
                >
                    <Campo etiqueta="Nombre de la vista" autoFocus maxLength={40} error={form.formState.errors.nombre?.message} {...form.register("nombre")} />
                    <DialogoPie>
                        <DialogoCerrar asChild>
                            <Boton variante="contorno">Cancelar</Boton>
                        </DialogoCerrar>
                        <Boton type="submit">Guardar vista</Boton>
                    </DialogoPie>
                </form>
            </DialogoContenido>
        </Dialogo>
    );
}
