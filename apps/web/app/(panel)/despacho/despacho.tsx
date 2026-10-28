"use client";

import { Aviso, Tarjeta } from "@iris/ui";
import { Lock } from "lucide-react";
import { useMemo, useState } from "react";
import { AgendaTecnicos } from "@/componentes/despacho/agenda-tecnicos";
import { Seccion } from "@/componentes/despacho/calendario";
import { AvisoCatalogos } from "@/componentes/despacho/catalogos";
import { FiltrosAgenda, NavegacionDias } from "@/componentes/despacho/controles";
import { useAhora, useCatalogosAgenda, useDatosAgenda, useOrdenesDelTecnico, usePosiciones, type DatosAgenda, type FiltrosDespacho } from "@/componentes/despacho/datos";
import { MapaDespacho, type Enfoque, type LineaRuta, type PuntoOrden, type PuntoTecnico } from "@/componentes/despacho/mapa";
import { RutaComparada } from "@/componentes/despacho/ruta-comparada";
import { TecnicosEnVivo, type PosicionVista } from "@/componentes/despacho/tecnicos-en-vivo";
import { EncabezadoPagina } from "@/componentes/encabezado-pagina";
import { ErrorLectura } from "@/componentes/error-lectura";
import { filasDeTecnicos, type Cuadrilla, type Zona } from "@/lib/despacho/calendario";
import { diaDesdeIso, etiquetaDiaLarga, horaDe, hoyGuatemala, type Dia } from "@/lib/despacho/fechas";
import { INTERVALO_POSICIONES_MS, antiguedadAhora, frescura, iniciales, textoPosicion } from "@/lib/despacho/posiciones";
import { paradasReales, type ParadaReal } from "@/lib/despacho/rutas";
import { useSesion } from "@/lib/sesion";

export function PantallaDespacho() {
    const [dia, setDia] = useState<Dia>(() => hoyGuatemala());
    const [filtros, setFiltros] = useState<FiltrosDespacho>({ cuadrillaId: null, zonaId: null });
    const catalogos = useCatalogosAgenda();
    return (
        <>
            <EncabezadoPagina titulo="Despacho" descripcion="Órdenes del día, técnicos en vivo y su ruta. Arrastra una cita para reasignarla o reprogramarla." />
            <Tarjeta className="mb-4 flex flex-wrap items-end justify-between gap-3 p-4">
                <NavegacionDias dia={dia} paso={1} alCambiar={setDia} />
                <FiltrosAgenda filtros={filtros} alCambiar={setFiltros} zonas={catalogos.zonas} cuadrillas={catalogos.cuadrillas} />
            </Tarjeta>
            <AvisoCatalogos {...catalogos} />
            <Consola
                key={`${dia}|${filtros.cuadrillaId}|${filtros.zonaId}`}
                dia={dia}
                filtros={filtros}
                zonas={catalogos.zonas.estado === "listo" ? catalogos.zonas.datos : undefined}
                cuadrillas={catalogos.cuadrillas.estado === "listo" ? catalogos.cuadrillas.datos : undefined}
            />
        </>
    );
}

function Consola({ dia, filtros, zonas, cuadrillas }: { dia: Dia; filtros: FiltrosDespacho; zonas: readonly Zona[] | undefined; cuadrillas: readonly Cuadrilla[] | undefined }) {
    const { puede } = useSesion();
    const datos = useDatosAgenda(dia, 1, filtros);
    const [enfoque, setEnfoque] = useState<Enfoque | null>(null);
    const [tecnicoRuta, setTecnicoRuta] = useState<number | null>(null);
    const verPosiciones = puede(["campo.ver_posiciones"]);
    // Una sola lectura de posiciones y una del recorrido: el mapa y las listas nunca difieren.
    const posiciones = usePosicionesVista(filtros, verPosiciones);
    const recorrido = useOrdenesDelTecnico(tecnicoRuta, dia);
    const reales = recorrido.estado === "listo" && recorrido.datos ? paradasReales(recorrido.datos) : [];
    const filas = datos.tecnicos.estado === "listo" && datos.agenda.estado === "listo" ? filasDeTecnicos(datos.tecnicos.datos.lista, datos.agenda.datos.citas ?? [], datos.agenda.datos.ausencias ?? []) : [];

    return (
        <div className="grid gap-4 [&>*]:min-w-0">
            <div className="grid gap-4 [&>*]:min-w-0 lg:grid-cols-[minmax(0,1.6fr)_minmax(18rem,1fr)]">
                <Seccion titulo={`Mapa · ${etiquetaDiaLarga(dia)}`} descripcion="Órdenes del día con coordenadas y la última posición de cada técnico." data-testid="seccion-mapa">
                    <MapaConDatos datos={datos} dia={dia} posiciones={verPosiciones ? posiciones.vista : null} enfoque={enfoque} reales={reales} />
                </Seccion>
                <Seccion titulo="Técnicos en vivo" descripcion="Última posición enviada por la app de campo." data-testid="seccion-en-vivo">
                    {verPosiciones ? (
                        <EnVivo posiciones={posiciones} cuadrillas={cuadrillas} alEnfocar={(p) => setEnfoque({ lat: p.lat, lon: p.lon, n: Date.now() })} />
                    ) : (
                        <Aviso tono="neutral" icono={Lock} titulo="Sin permiso para ver ubicaciones">
                            Hace falta el permiso «campo.ver_posiciones».
                        </Aviso>
                    )}
                </Seccion>
            </div>
            <Seccion titulo="Ruta propuesta frente a la real" descripcion="Paradas del técnico en el orden en que llegó." data-testid="seccion-ruta">
                <RutaComparada filas={filas} tecnicoId={tecnicoRuta} alElegir={setTecnicoRuta} ordenes={puede(["ordenes.ver"]) ? recorrido : null} propuesta={null} reales={reales} />
            </Seccion>
            <Seccion titulo="Calendario del día" descripcion="Arrastra una cita a otra hora o a otro técnico; en el teléfono, «Mover a…»." data-testid="seccion-calendario">
                <AgendaTecnicos datos={datos} vista="dia" desde={dia} zonas={zonas} cuadrillas={cuadrillas} />
            </Seccion>
        </div>
    );
}

/**
 * Posiciones con su antigüedad al momento. Si la lectura falla: error con «Reintentar» y ningún
 * técnico en el mapa (nunca posiciones viejas como si fueran actuales).
 */
function usePosicionesVista(filtros: FiltrosDespacho, activo: boolean) {
    const lectura = usePosiciones(filtros, activo, INTERVALO_POSICIONES_MS);
    const ahora = useAhora(5_000);
    const leida = lectura.estado === "listo" ? lectura.datos : null;
    const vista: PosicionVista[] | null = leida ? (leida.respuesta.data ?? []).map((p) => ({ ...p, antiguedad: antiguedadAhora(p, leida.leidaEn, ahora) })) : null;
    const leidaHace = leida ? Math.max(0, (ahora - leida.leidaEn) / 1000) : 0;
    return { lectura, vista, leidaHace };
}

function EnVivo({ posiciones, cuadrillas, alEnfocar }: { posiciones: ReturnType<typeof usePosicionesVista>; cuadrillas: readonly Cuadrilla[] | undefined; alEnfocar: (p: PosicionVista) => void }) {
    const { lectura, vista, leidaHace } = posiciones;
    const [actualizando, setActualizando] = useState(false);
    if (lectura.estado === "error") {
        return <ErrorLectura error={lectura.error} reintentar={lectura.reintentar} que="las posiciones de los técnicos" />;
    }
    if (vista === null) {
        return <p className="text-sm text-muted-foreground">Cargando posiciones…</p>;
    }
    return (
        <TecnicosEnVivo
            posiciones={vista}
            cuadrillas={cuadrillas}
            actualizadaHace={leidaHace}
            actualizando={actualizando}
            alActualizar={async () => {
                setActualizando(true);
                await lectura.refrescar();
                setActualizando(false);
            }}
            alEnfocar={alEnfocar}
        />
    );
}

function MapaConDatos({
    datos,
    dia,
    posiciones,
    enfoque,
    reales,
}: {
    datos: DatosAgenda;
    dia: Dia;
    posiciones: readonly PosicionVista[] | null;
    enfoque: Enfoque | null;
    reales: readonly ParadaReal[];
}) {
    const agenda = datos.agenda;
    const citas = useMemo(() => (agenda.estado === "listo" ? (agenda.datos.citas ?? []).filter((c) => diaDesdeIso(c.inicio) === dia) : []), [agenda, dia]);
    const ordenes: PuntoOrden[] = useMemo(
        () =>
            citas
                .filter((c): c is typeof c & { lat: number; lon: number } => c.lat !== null && c.lon !== null)
                .map((c) => ({ ordenId: c.ordenId, numero: c.numero, estado: c.estado, lat: c.lat, lon: c.lon, detalle: `${horaDe(c.inicio)}–${horaDe(c.fin)} · ${c.direccion}` })),
        [citas],
    );
    const tecnicos: PuntoTecnico[] = useMemo(
        () => (posiciones ?? []).map((p) => ({ tecnicoId: p.tecnicoId, nombre: p.nombre, iniciales: iniciales(p.nombre), lat: p.lat, lon: p.lon, frescura: frescura(p.antiguedad), texto: textoPosicion(p.antiguedad) })),
        [posiciones],
    );
    const lineas: LineaRuta[] = useMemo(
        () => [{ id: "real", coordenadas: reales.filter((p) => p.lat !== null && p.lon !== null).map((p): [number, number] => [p.lon ?? 0, p.lat ?? 0]) }],
        [reales],
    );
    const sinCoordenadas = citas.length - ordenes.length;

    return (
        <div className="grid gap-2 [&>*]:min-w-0">
            <MapaDespacho ordenes={ordenes} tecnicos={tecnicos} lineas={lineas} enfoque={enfoque} className="h-[22rem] lg:h-[28rem]" />
            <p className="text-xs text-muted-foreground" data-testid="leyenda-mapa">
                {agenda.estado === "error"
                    ? "Las órdenes del día no se pudieron leer (ver el calendario)."
                    : agenda.estado === "cargando"
                      ? "Cargando órdenes…"
                      : `${ordenes.length} ${ordenes.length === 1 ? "orden" : "órdenes"} en el mapa${sinCoordenadas > 0 ? ` · ${sinCoordenadas} sin coordenadas` : ""}`}
                {posiciones ? ` · ${tecnicos.length} ${tecnicos.length === 1 ? "técnico" : "técnicos"} con posición` : ""}
            </p>
        </div>
    );
}
