"use client";

import { Boton, CampoSelector, Entrada, Etiqueta, GrupoSegmentado, Tarjeta } from "@iris/ui";
import { Columns3, List, Plus, Search } from "lucide-react";
import Link from "next/link";
import { useId, useState } from "react";
import { EncabezadoPagina } from "@/componentes/encabezado-pagina";
import { tecnicosDe, usePendientesAbiertos } from "@/componentes/ordenes/catalogos";
import { useUsuarios } from "@/componentes/tickets/catalogos";
import { escribirLocal, leerLocal } from "@/lib/almacen";
import { useDiferido } from "@/lib/diferido";
import { consultaDeFiltrosOrdenes, FILTROS_ORDENES_INICIALES, hayFiltrosOrdenes, type FiltrosOrdenes } from "@/lib/ordenes/filtros";
import { ESTADOS_ORDEN, ORDEN_ESTADOS_ORDEN, ORDEN_TIPOS, TIPOS_ORDEN } from "@/lib/ordenes/presentacion";
import { useSesion } from "@/lib/sesion";
import { ListaOrdenes, type Vista } from "./lista-ordenes";

const CLAVE_VISTA = "iris-vista-ordenes";

export function Tablero() {
    const { yo, puede } = useSesion();
    const usuarios = useUsuarios();
    const pendientes = usePendientesAbiertos();
    const [filtros, setFiltros] = useState<FiltrosOrdenes>(FILTROS_ORDENES_INICIALES);
    const [vista, setVista] = useState<Vista>(() => (leerLocal(CLAVE_VISTA) === "columnas" ? "columnas" : "lista"));
    const idBuscar = useId();
    const idFecha = useId();
    const idZona = useId();
    const q = useDiferido(filtros.q, 350);
    const zona = useDiferido(filtros.zona, 350);
    const efectivos: FiltrosOrdenes = { ...filtros, q, zona };
    const consulta = consultaDeFiltrosOrdenes(efectivos);
    const cambiar = (parcial: Partial<FiltrosOrdenes>) => setFiltros((f) => ({ ...f, ...parcial }));
    const listaUsuarios = usuarios.estado === "listo" ? usuarios.datos : undefined;
    const tecnicos = tecnicosDe(listaUsuarios);
    const soloPropias = !puede(["ordenes.ver"]);
    const tecnicoElegidoDesconocido = typeof filtros.tecnico === "number" && filtros.tecnico !== yo.usuario.id && !tecnicos.some((t) => t.id === filtros.tecnico);

    return (
        <>
            <EncabezadoPagina
                titulo="Órdenes"
                descripcion={soloPropias ? "Las órdenes asignadas a ti." : "Instalaciones, visitas, retiros y cambios de equipo."}
                acciones={
                    puede(["ordenes.crear"]) ? (
                        <Boton asChild>
                            <Link href="/ordenes/nueva">
                                <Plus />
                                Nueva orden
                            </Link>
                        </Boton>
                    ) : null
                }
            />

            <Tarjeta className="mb-4 grid grid-cols-2 gap-3 p-4 lg:grid-cols-[minmax(12rem,1.3fr)_repeat(5,minmax(0,1fr))]">
                <div className="col-span-2 grid gap-1.5 lg:col-span-1">
                    <Etiqueta htmlFor={idBuscar}>Buscar</Etiqueta>
                    <span className="relative">
                        <Search aria-hidden className="pointer-events-none absolute top-1/2 left-3 size-4 -translate-y-1/2 text-muted-foreground" />
                        <Entrada
                            id={idBuscar}
                            type="search"
                            placeholder="Número, dirección o contacto"
                            className="pl-9 sm:text-sm"
                            maxLength={120}
                            value={filtros.q}
                            onChange={(e) => cambiar({ q: e.target.value })}
                        />
                    </span>
                </div>
                <CampoSelector etiqueta="Estado" value={filtros.estado} onChange={(e) => cambiar({ estado: e.target.value as FiltrosOrdenes["estado"] })}>
                    <option value="todos">Todos</option>
                    <option value="activas">Activas</option>
                    {ORDEN_ESTADOS_ORDEN.map((e) => (
                        <option key={e} value={e}>
                            {ESTADOS_ORDEN[e].texto}
                        </option>
                    ))}
                </CampoSelector>
                <CampoSelector etiqueta="Tipo" value={filtros.tipo} onChange={(e) => cambiar({ tipo: e.target.value as FiltrosOrdenes["tipo"] })}>
                    <option value="todos">Todos</option>
                    {ORDEN_TIPOS.map((t) => (
                        <option key={t} value={t}>
                            {TIPOS_ORDEN[t].texto}
                        </option>
                    ))}
                </CampoSelector>
                <CampoSelector
                    etiqueta="Técnico"
                    value={String(filtros.tecnico)}
                    disabled={soloPropias}
                    ayuda={usuarios.estado === "error" ? "No se pudo cargar la lista de técnicos." : undefined}
                    onChange={(e) => {
                        const v = e.target.value;
                        cambiar({ tecnico: v === "todos" || v === "sin" ? v : Number(v) });
                    }}
                >
                    <option value="todos">Todos</option>
                    <option value="sin">Sin técnico</option>
                    <option value={yo.usuario.id}>{yo.usuario.nombre} (tú)</option>
                    {tecnicoElegidoDesconocido ? <option value={String(filtros.tecnico)}>Técnico {String(filtros.tecnico)}</option> : null}
                    {tecnicos
                        .filter((t) => t.id !== yo.usuario.id)
                        .map((t) => (
                            <option key={t.id} value={t.id}>
                                {t.nombre}
                            </option>
                        ))}
                </CampoSelector>
                <div className="grid gap-1.5">
                    <Etiqueta htmlFor={idFecha}>Fecha programada</Etiqueta>
                    <Entrada id={idFecha} type="date" className="sm:text-sm" value={filtros.fecha} onChange={(e) => cambiar({ fecha: e.target.value })} />
                </div>
                <div className="grid gap-1.5">
                    <Etiqueta htmlFor={idZona}>Zona</Etiqueta>
                    <Entrada
                        id={idZona}
                        inputMode="numeric"
                        placeholder="Id de zona"
                        className="sm:text-sm"
                        maxLength={12}
                        value={filtros.zona}
                        onChange={(e) => cambiar({ zona: e.target.value.replace(/\D/g, "") })}
                    />
                </div>
            </Tarjeta>

            <div className="mb-3 flex flex-wrap items-center justify-between gap-2">
                <GrupoSegmentado
                    etiqueta="Vista"
                    opciones={[
                        { valor: "lista", etiqueta: "Lista", icono: List },
                        { valor: "columnas", etiqueta: "Columnas", icono: Columns3 },
                    ]}
                    valor={vista}
                    alCambiar={(v) => {
                        setVista(v);
                        escribirLocal(CLAVE_VISTA, v);
                    }}
                />
            </div>

            <ListaOrdenes
                key={JSON.stringify(consulta)}
                consulta={consulta}
                vista={vista}
                usuarios={listaUsuarios}
                pendientes={pendientes}
                yo={{ id: yo.usuario.id, nombre: yo.usuario.nombre }}
                hayFiltros={hayFiltrosOrdenes(efectivos)}
            />
        </>
    );
}
