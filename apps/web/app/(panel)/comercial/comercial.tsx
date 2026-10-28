"use client";

import { Boton, Entrada, Esqueleto, Etiqueta, GrupoSegmentado, Selector } from "@iris/ui";
import { Columns3, List, Map as IconoMapa, Search, X } from "lucide-react";
import { usePathname, useRouter, useSearchParams } from "next/navigation";
import { useEffect, useId, useState, type ReactNode } from "react";
import { useCoincide, useZonas } from "@/componentes/comercial/comun";
import { EncabezadoPagina } from "@/componentes/encabezado-pagina";
import { useUsuarios } from "@/componentes/tickets/catalogos";
import { filtrosDesdeUrl, hayFiltros, urlDe, vistaDesdeUrl, type Filtros, type Responsable, type Vista } from "@/lib/comercial/filtros";
import { ESTADOS_PROSPECTO, ORDEN_ESTADOS, ORIGENES, type EstadoProspecto, type OrigenProspecto } from "@/lib/comercial/presentacion";
import { useDiferido } from "@/lib/diferido";
import { useSesion } from "@/lib/sesion";
import { AltaProspecto } from "./alta-prospecto";
import { MapaProspectos } from "./mapa-prospectos";
import { Embudo, ListaProspectos, type Catalogos } from "./prospectos";

const VISTAS = [
    { valor: "embudo", etiqueta: "Embudo", icono: Columns3 },
    { valor: "lista", etiqueta: "Lista", icono: List },
    { valor: "mapa", etiqueta: "Mapa", icono: IconoMapa },
] as const;

/**
 * `/comercial`: embudo por estado, lista con cursor y mapa, con filtros en la URL (estado,
 * vendedor, zona, origen y texto). En el teléfono, una lista con el filtro de estado.
 */
export function Comercial() {
    const { yo, puede } = useSesion();
    const router = useRouter();
    const ruta = usePathname();
    const params = useSearchParams();
    const ancho = useCoincide("(min-width: 768px)");
    const filtros = filtrosDesdeUrl(params);
    const vistaUrl = vistaDesdeUrl(params);
    const vista: Vista = ancho === false ? "lista" : vistaUrl;
    const zonas = useZonas();
    const usuarios = useUsuarios();
    const catalogos: Catalogos = {
        zonas: zonas.estado === "listo" ? zonas.datos : null,
        usuarios: usuarios.estado === "listo" ? usuarios.datos : null,
    };

    const [texto, setTexto] = useState(filtros.q);
    const q = useDiferido(texto, 350);
    const ir = (f: Filtros, v: Vista = vistaUrl) => router.replace(urlDe(f, v), { scroll: false });
    useEffect(() => {
        if (q.trim() !== filtros.q.trim() && ruta === "/comercial") {
            router.replace(urlDe({ ...filtros, q }, vistaUrl), { scroll: false });
        }
        // Solo al cambiar el texto (diferido): el resto de los filtros ya está en la URL.
        // eslint-disable-next-line react-hooks/exhaustive-deps
    }, [q]);

    const cambiar = (parcial: Partial<Filtros>) => ir({ ...filtros, ...parcial });
    // La clave remonta la vista con cada filtro: cursor y columnas empiezan de cero.
    const clave = `${vista}|${JSON.stringify(filtros)}`;

    return (
        <>
            <EncabezadoPagina
                titulo="Comercial"
                descripcion="Prospectos, factibilidad, cotizaciones y contratos."
                acciones={puede(["prospectos.crear"]) ? <AltaProspecto zonas={catalogos.zonas} /> : undefined}
            />
            {ancho === null ? (
                <Esqueleto className="h-64" />
            ) : (
                <>
                    {ancho ? (
                        <div className="mb-4 grid gap-3">
                            <GrupoSegmentado etiqueta="Vista" opciones={VISTAS} valor={vistaUrl} alCambiar={(v) => ir(filtros, v)} className="justify-self-start" />
                            <BarraFiltros filtros={filtros} cambiar={cambiar} texto={texto} setTexto={setTexto} vista={vista} catalogos={catalogos} yoId={yo.usuario.id} limpiar={() => {
                                setTexto("");
                                ir({ estado: "", responsable: "", zonaId: null, origen: "", q: "" });
                            }} />
                        </div>
                    ) : (
                        <FiltroEstadoTelefono estado={filtros.estado} cambiar={(estado) => cambiar({ estado })} />
                    )}
                    <div key={clave}>
                        {vista === "embudo" ? <Embudo filtros={filtros} catalogos={catalogos} /> : null}
                        {vista === "lista" ? <ListaProspectos filtros={filtros} catalogos={catalogos} telefono={!ancho} /> : null}
                        {vista === "mapa" ? <MapaProspectos filtros={filtros} /> : null}
                    </div>
                </>
            )}
        </>
    );
}

function BarraFiltros({
    filtros,
    cambiar,
    texto,
    setTexto,
    vista,
    catalogos,
    yoId,
    limpiar,
}: {
    filtros: Filtros;
    cambiar: (p: Partial<Filtros>) => void;
    texto: string;
    setTexto: (t: string) => void;
    vista: Vista;
    catalogos: Catalogos;
    yoId: number;
    limpiar: () => void;
}) {
    const id = useId();
    const usuarios = (catalogos.usuarios ?? []).filter((u) => u.id !== yoId && u.tipo === "persona");
    return (
        <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-[minmax(12rem,2fr)_repeat(4,minmax(8rem,1fr))_auto] lg:items-end" role="group" aria-label="Filtros">
            <div className="grid gap-1.5">
                <Etiqueta htmlFor={`${id}-q`}>Buscar</Etiqueta>
                <div className="relative">
                    <Search aria-hidden className="pointer-events-none absolute top-1/2 left-3 size-4 -translate-y-1/2 text-muted-foreground" />
                    <Entrada id={`${id}-q`} type="search" value={texto} onChange={(e) => setTexto(e.target.value)} placeholder="Nombre, teléfono, dirección…" className="pl-9" />
                </div>
            </div>
            {vista !== "embudo" ? (
                <Filtro id={`${id}-estado`} etiqueta="Estado" valor={filtros.estado} alCambiar={(v) => cambiar({ estado: v as EstadoProspecto | "" })}>
                    <option value="">Todos</option>
                    {ORDEN_ESTADOS.map((e) => (
                        <option key={e} value={e}>
                            {ESTADOS_PROSPECTO[e].texto}
                        </option>
                    ))}
                </Filtro>
            ) : null}
            <Filtro id={`${id}-resp`} etiqueta="Vendedor" valor={filtros.responsable} alCambiar={(v) => cambiar({ responsable: v as Responsable })}>
                <option value="">Todos</option>
                <option value="mios">Míos</option>
                <option value="sin">Sin vendedor</option>
                {usuarios.map((u) => (
                    <option key={u.id} value={`u${u.id}`}>
                        {u.nombre}
                    </option>
                ))}
            </Filtro>
            <Filtro
                id={`${id}-zona`}
                etiqueta="Zona"
                valor={filtros.zonaId === null ? "" : String(filtros.zonaId)}
                alCambiar={(v) => cambiar({ zonaId: v ? Number(v) : null })}
                disabled={catalogos.zonas === null}
            >
                <option value="">Todas</option>
                {(catalogos.zonas ?? []).map((z) => (
                    <option key={z.id} value={String(z.id)}>
                        {z.nombre}
                    </option>
                ))}
            </Filtro>
            <Filtro id={`${id}-origen`} etiqueta="Origen" valor={filtros.origen} alCambiar={(v) => cambiar({ origen: v as OrigenProspecto | "" })}>
                <option value="">Todos</option>
                {(Object.keys(ORIGENES) as OrigenProspecto[]).map((o) => (
                    <option key={o} value={o}>
                        {ORIGENES[o]}
                    </option>
                ))}
            </Filtro>
            {hayFiltros(filtros) ? (
                <Boton variante="fantasma" onClick={limpiar} className="justify-self-start">
                    <X />
                    Limpiar
                </Boton>
            ) : null}
        </div>
    );
}

function Filtro({ id, etiqueta, valor, alCambiar, disabled, children }: { id: string; etiqueta: string; valor: string; alCambiar: (v: string) => void; disabled?: boolean; children: ReactNode }) {
    return (
        <div className="grid gap-1.5">
            <Etiqueta htmlFor={id}>{etiqueta}</Etiqueta>
            <Selector id={id} value={valor} onChange={(e) => alCambiar(e.target.value)} disabled={disabled}>
                {children}
            </Selector>
        </div>
    );
}

function FiltroEstadoTelefono({ estado, cambiar }: { estado: EstadoProspecto | ""; cambiar: (e: EstadoProspecto | "") => void }) {
    const id = useId();
    return (
        <div className="mb-4 grid gap-1.5">
            <Etiqueta htmlFor={id}>Estado</Etiqueta>
            <Selector id={id} value={estado} onChange={(e) => cambiar(e.target.value as EstadoProspecto | "")}>
                <option value="">Todos</option>
                {ORDEN_ESTADOS.map((e) => (
                    <option key={e} value={e}>
                        {ESTADOS_PROSPECTO[e].texto}
                    </option>
                ))}
            </Selector>
        </div>
    );
}
