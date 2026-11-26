"use client";

import { Boton, Casilla, Esqueleto, EstadoVacio, Etiqueta, FilaLista, ListaFilas, MenuAcciones, Selector } from "@iris/ui";
import { ClipboardCheck, ClipboardList, Plus, SquareArrowOutUpRight } from "lucide-react";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { useId, useState } from "react";
import { EncabezadoPagina } from "@/componentes/encabezado-pagina";
import { ErrorLectura } from "@/componentes/error-lectura";
import { piePaginado } from "@/componentes/pie-lista";
import { InsigniasPlantilla, textoTipoPlantilla } from "@/componentes/plantillas/insignias";
import { api, resolver } from "@/lib/api/cliente";
import { usePaginado } from "@/lib/paginado";
import { TIPOS_ORDEN } from "@/lib/ordenes/presentacion";
import type { Plantilla, TipoOrdenPlantilla } from "@/lib/plantillas/esquemas";
import { useSesion } from "@/lib/sesion";

const TAMANO_PAGINA = 25;

/** Plantillas de formulario: filtro por tipo de orden y activas o todas, con cursor. */
export function ListaPlantillas() {
    const { puede } = useSesion();
    const id = useId();
    const [tipo, setTipo] = useState<TipoOrdenPlantilla | "">("");
    const [todas, setTodas] = useState(false);
    return (
        <>
            <EncabezadoPagina
                titulo="Plantillas de formulario"
                descripcion="Checklists que el técnico llena en cada orden. Editar una plantilla que ya se usó crea una versión nueva."
                acciones={
                    puede(["plantillas.crear"]) ? (
                        <Boton asChild>
                            <Link href="/administracion/plantillas/nueva">
                                <Plus />
                                Nueva plantilla
                            </Link>
                        </Boton>
                    ) : undefined
                }
            />
            <div className="mb-4 flex flex-wrap items-end gap-4" role="group" aria-label="Filtros">
                <div className="grid min-w-48 gap-1.5">
                    <Etiqueta htmlFor={`${id}-tipo`}>Tipo de orden</Etiqueta>
                    <Selector id={`${id}-tipo`} value={tipo} onChange={(e) => setTipo(e.target.value as TipoOrdenPlantilla | "")}>
                        <option value="">Todos</option>
                        {(Object.keys(TIPOS_ORDEN) as TipoOrdenPlantilla[]).map((t) => (
                            <option key={t} value={t}>
                                {TIPOS_ORDEN[t].texto}
                            </option>
                        ))}
                    </Selector>
                </div>
                <Casilla etiqueta="Incluir las inactivas" checked={todas} onChange={(e) => setTodas(e.target.checked)} className="pb-2" />
            </div>
            <Resultados key={`${tipo}|${todas}`} tipo={tipo} todas={todas} />
        </>
    );
}

function Resultados({ tipo, todas }: { tipo: TipoOrdenPlantilla | ""; todas: boolean }) {
    // Paginada por nombre (`nextCursor`): «Cargar más» como el resto de las listas.
    const paginado = usePaginado<Plantilla>((cursor, signal) =>
        resolver(
            api().GET("/api/v1/plantillas", {
                params: { query: { limit: TAMANO_PAGINA, ...(tipo ? { tipo } : {}), ...(todas ? { todas: true } : {}), ...(cursor ? { cursor } : {}) } },
                ...(signal ? { signal } : {}),
            }),
        ),
    );
    const router = useRouter();
    const lectura = paginado.primera;
    if (lectura.estado === "cargando") {
        return (
            <ListaFilas
                etiqueta="Plantillas"
                vacio={
                    <div className="grid gap-3" aria-busy="true">
                        <span className="sr-only" role="status">
                            Cargando plantillas…
                        </span>
                        {Array.from({ length: 3 }, (_, i) => (
                            <Esqueleto key={i} className="h-12" />
                        ))}
                    </div>
                }
            >
                {[]}
            </ListaFilas>
        );
    }
    if (lectura.estado === "error") return <ErrorLectura error={lectura.error} que="las plantillas" reintentar={lectura.reintentar} />;
    return (
        <ListaFilas
            etiqueta="Plantillas"
            {...piePaginado(paginado, "plantillas")}
            vacio={<EstadoVacio icono={ClipboardList} titulo="No hay plantillas con estos filtros" descripcion={todas ? undefined : "Prueba incluir las inactivas."} />}
        >
            {paginado.items.map((p) => (
                <FilaLista
                    key={p.id}
                    data-plantilla={p.codigo}
                    icono={ClipboardCheck}
                    tonoIcono={p.activa ? "marca" : "neutro"}
                    titulo={p.nombre}
                    detalle={
                        <>
                            <span className="font-mono">{p.codigo}</span> · v{p.versionActual} · {textoTipoPlantilla(p.tipo)}
                        </>
                    }
                    href={`/administracion/plantillas/${p.id}`}
                    extremo={<InsigniasPlantilla p={p} />}
                    extremoTelefono="debajo"
                    menu={
                        <MenuAcciones
                            etiqueta={`Acciones de ${p.nombre}`}
                            acciones={[{ id: "abrir", texto: "Abrir plantilla", icono: SquareArrowOutUpRight, alElegir: () => router.push(`/administracion/plantillas/${p.id}`) }]}
                        />
                    }
                />
            ))}
        </ListaFilas>
    );
}
