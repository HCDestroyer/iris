"use client";

import { zodResolver } from "@hookform/resolvers/zod";
import {
    Boton,
    Campo,
    CampoSelector,
    cn,
    Dialogo,
    DialogoCerrar,
    DialogoContenido,
    DialogoPie,
    Entrada,
    Etiqueta,
    Tarjeta,
} from "@iris/ui";
import { BookmarkPlus, Search, X } from "lucide-react";
import { useId, useState } from "react";
import { useForm } from "react-hook-form";
import { z } from "zod";
import { EncabezadoPagina } from "@/componentes/encabezado-pagina";
import { useCategorias, useUsuarios } from "@/componentes/tickets/catalogos";
import { useDiferido } from "@/lib/diferido";
import { useSesion } from "@/lib/sesion";
import {
    consultaDeFiltros,
    FILTROS_INICIALES,
    guardarVistas,
    leerVistas,
    mismosFiltros,
    VISTAS_PREDEFINIDAS,
    type Filtros,
    type Vista,
} from "@/lib/tickets/filtros";
import { ESTADOS, ORDEN_ESTADOS, ORDEN_PRIORIDADES, PRIORIDADES } from "@/lib/tickets/presentacion";
import { AltaRapida } from "./alta-rapida";
import { ListaTickets } from "./lista-tickets";

export function Bandeja() {
    const { yo, puede } = useSesion();
    const categorias = useCategorias();
    const usuarios = useUsuarios();
    const [filtros, setFiltros] = useState<Filtros>(FILTROS_INICIALES);
    const [propias, setPropias] = useState<Vista[]>(() => leerVistas(yo.usuario.id));
    const [guardando, setGuardando] = useState(false);
    const idBuscar = useId();
    const q = useDiferido(filtros.q, 350);

    const efectivos: Filtros = { ...filtros, q };
    const consulta = consultaDeFiltros(efectivos, yo.usuario.id);
    const vistas = [...VISTAS_PREDEFINIDAS, ...propias];
    const vistaActiva = vistas.find((v) => mismosFiltros(v.filtros, filtros));
    const cambiar = (parcial: Partial<Filtros>) => setFiltros((f) => ({ ...f, ...parcial }));

    function guardarPropias(nuevas: Vista[]) {
        setPropias(nuevas);
        guardarVistas(yo.usuario.id, nuevas);
    }

    const listaCategorias = categorias.estado === "listo" ? categorias.datos.filter((c) => c.activa) : [];

    return (
        <>
            <EncabezadoPagina
                titulo="Tickets"
                descripcion="Soporte con SLA: lo urgente primero."
                acciones={puede(["tickets.crear"]) ? <AltaRapida categorias={listaCategorias} /> : null}
            />

            <div className="mb-3 flex items-center gap-2 overflow-x-auto pb-1" role="group" aria-label="Vistas">
                {vistas.map((v) => {
                    const activa = vistaActiva?.id === v.id;
                    return (
                        <span key={v.id} className="inline-flex shrink-0 items-center">
                            <Boton
                                variante={activa ? "secundario" : "fantasma"}
                                tamano="sm"
                                aria-pressed={activa}
                                className={cn(activa && "text-primary", v.propia && "rounded-r-none pr-2")}
                                onClick={() => setFiltros(v.filtros)}
                            >
                                {v.nombre}
                            </Boton>
                            {v.propia ? (
                                <Boton
                                    variante={activa ? "secundario" : "fantasma"}
                                    tamano="icono-sm"
                                    className="rounded-l-none"
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
                <Boton variante="fantasma" tamano="sm" className="shrink-0 text-muted-foreground" disabled={vistaActiva !== undefined} onClick={() => setGuardando(true)}>
                    <BookmarkPlus />
                    Guardar vista
                </Boton>
            </div>

            <Tarjeta className="mb-4 grid grid-cols-2 gap-3 p-4 lg:grid-cols-[minmax(12rem,1.4fr)_repeat(4,minmax(0,1fr))]">
                <div className="col-span-2 grid gap-1.5 lg:col-span-1">
                    <Etiqueta htmlFor={idBuscar}>Buscar</Etiqueta>
                    <span className="relative">
                        <Search aria-hidden className="pointer-events-none absolute top-1/2 left-3 size-4 -translate-y-1/2 text-muted-foreground" />
                        <Entrada
                            id={idBuscar}
                            type="search"
                            placeholder="Número, asunto o contacto"
                            className="pl-9 sm:text-sm"
                            maxLength={120}
                            value={filtros.q}
                            onChange={(e) => cambiar({ q: e.target.value })}
                        />
                    </span>
                </div>
                <CampoSelector etiqueta="Estado" value={filtros.estado} onChange={(e) => cambiar({ estado: e.target.value as Filtros["estado"] })}>
                    <option value="activos">Activos</option>
                    {ORDEN_ESTADOS.map((e) => (
                        <option key={e} value={e}>
                            {ESTADOS[e].texto}
                        </option>
                    ))}
                    <option value="todos">Todos</option>
                </CampoSelector>
                <CampoSelector
                    etiqueta="Prioridad"
                    value={filtros.prioridad}
                    onChange={(e) => cambiar({ prioridad: e.target.value as Filtros["prioridad"] })}
                >
                    <option value="todas">Todas</option>
                    {[...ORDEN_PRIORIDADES].reverse().map((p) => (
                        <option key={p} value={p}>
                            {PRIORIDADES[p].texto}
                        </option>
                    ))}
                </CampoSelector>
                <CampoSelector
                    etiqueta="Categoría"
                    value={String(filtros.categoria)}
                    disabled={categorias.estado !== "listo"}
                    ayuda={categorias.estado === "error" ? "No se pudieron cargar las categorías." : undefined}
                    onChange={(e) => cambiar({ categoria: e.target.value === "todas" ? "todas" : Number(e.target.value) })}
                >
                    <option value="todas">Todas</option>
                    {listaCategorias.map((c) => (
                        <option key={c.id} value={c.id}>
                            {c.nombre}
                        </option>
                    ))}
                </CampoSelector>
                <CampoSelector
                    etiqueta="Asignación"
                    value={filtros.asignacion}
                    onChange={(e) => cambiar({ asignacion: e.target.value as Filtros["asignacion"] })}
                >
                    <option value="todos">Todos</option>
                    <option value="mios">Asignados a mí</option>
                    <option value="sin_asignar">Sin asignar</option>
                </CampoSelector>
            </Tarjeta>

            <ListaTickets
                key={JSON.stringify(consulta)}
                consulta={consulta}
                categorias={categorias.estado === "listo" ? categorias.datos : undefined}
                usuarios={usuarios.estado === "listo" ? usuarios.datos : undefined}
                yo={{ id: yo.usuario.id, nombre: yo.usuario.nombre }}
                hayFiltros={!mismosFiltros(efectivos, FILTROS_INICIALES)}
            />

            <DialogoGuardarVista
                abierto={guardando}
                alCerrar={() => setGuardando(false)}
                alGuardar={(nombre) => {
                    guardarPropias([...propias, { id: `v-${Date.now().toString(36)}`, nombre, filtros, propia: true }]);
                    setGuardando(false);
                }}
            />
        </>
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
            <DialogoContenido titulo="Guardar vista" descripcion="Guarda los filtros actuales en este navegador.">
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
