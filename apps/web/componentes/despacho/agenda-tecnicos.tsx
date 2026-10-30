"use client";

import { Aviso, Esqueleto } from "@iris/ui";
import { CircleCheck, TriangleAlert } from "lucide-react";
import { useState } from "react";
import { ErrorLectura } from "@/componentes/error-lectura";
import { conflictosDe, textoConflicto } from "@/lib/agenda/mes";
import { filasDeTecnicos, type Cuadrilla, type Zona } from "@/lib/despacho/calendario";
import { diaDesdeIso, etiquetaDia, horaDe, hoyGuatemala, type Dia } from "@/lib/despacho/fechas";
import { movibleDeCita, movibleDeOrden, type DatosMover, type Movible } from "@/lib/despacho/mover";
import type { Orden } from "@/lib/ordenes/presentacion";
import { BandejaSinProgramar, CalendarioTecnicos, Seccion, type Vista } from "./calendario";
import { useAhora, type DatosAgenda } from "./datos";
import { DialogoMoverOrden, type Destino } from "./mover-orden";

type Abierto = { ordenId: number; destino: Destino | undefined; foto: Movible };

/**
 * Calendario por técnico con sus lecturas: si los técnicos o la agenda no se pudieron leer, se
 * muestra el error con «Reintentar» y no se dibuja nada (un hueco vacío parecería tiempo libre).
 */
export function AgendaTecnicos({
    datos,
    vista,
    desde,
    zonas,
    cuadrillas,
    bandeja = true,
}: {
    datos: DatosAgenda;
    vista: Vista;
    desde: Dia;
    zonas: readonly Zona[] | undefined;
    cuadrillas: readonly Cuadrilla[] | undefined;
    bandeja?: boolean;
}) {
    const [arrastre, setArrastre] = useState<Movible | null>(null);
    const [abierto, setAbierto] = useState<Abierto | null>(null);
    const [hecho, setHecho] = useState<string | null>(null);
    const ahora = useAhora(60_000);
    const { tecnicos, agenda, sinProgramar } = datos;

    if (tecnicos.estado === "error" || agenda.estado === "error") {
        const fallo = tecnicos.estado === "error" ? tecnicos : agenda.estado === "error" ? agenda : null;
        return (
            <ErrorLectura
                error={fallo?.estado === "error" ? fallo.error : null}
                que={tecnicos.estado === "error" ? "los técnicos" : "la agenda"}
                reintentar={() => {
                    if (tecnicos.estado === "error") tecnicos.reintentar();
                    if (agenda.estado === "error") agenda.reintentar();
                }}
            />
        );
    }
    if (tecnicos.estado === "cargando" || agenda.estado === "cargando") {
        return (
            <div className="grid gap-2" aria-busy="true" aria-label="Cargando la agenda">
                <Esqueleto className="h-14" />
                <Esqueleto className="h-14" />
                <Esqueleto className="h-14" />
            </div>
        );
    }

    const calendario = agenda.datos;
    const filas = filasDeTecnicos(tecnicos.datos.lista, calendario.citas ?? [], calendario.ausencias ?? []);
    const pendientes = sinProgramar.estado === "listo" ? (sinProgramar.datos?.ordenes ?? []) : [];

    function abrir(m: Movible, destino?: Destino) {
        setHecho(null);
        setArrastre(null);
        setAbierto({ ordenId: m.ordenId, destino, foto: m });
    }

    // La orden del diálogo, con la versión de la última lectura (tras «Recargar» por un 409).
    const actual = abierto
        ? (() => {
              const cita = (calendario.citas ?? []).find((c) => c.ordenId === abierto.ordenId);
              if (cita) return movibleDeCita(cita);
              const o = pendientes.find((x) => x.id === abierto.ordenId);
              return o ? movibleDeOrden(o) : undefined;
          })()
        : undefined;

    function alHecho(o: Orden, d: DatosMover) {
        const nombre = filas.find((f) => f.usuarioId === o.tecnicoId)?.nombre ?? (o.tecnicoId !== null ? `Técnico ${o.tecnicoId}` : "sin técnico");
        const cuando = o.programadaInicio ? `${etiquetaDia(diaDesdeIso(o.programadaInicio) ?? desde)} ${horaDe(o.programadaInicio)}` : null;
        const verbo = d.accion === "asignar" ? "asignada" : d.accion === "programar" ? "programada" : "reprogramada";
        setHecho(`${o.numero} ${verbo}: ${[nombre, cuando].filter(Boolean).join(", ")}.`);
        setAbierto(null);
        void datos.refrescar();
    }

    const conflictos = conflictosDe(calendario.citas ?? [], calendario.ausencias ?? []);
    const conConflicto = (calendario.citas ?? []).filter((c) => conflictos.has(c.ordenId)).sort((a, b) => a.inicio.localeCompare(b.inicio));
    const comun = { filas, calendario, zonas, cuadrillas, alMover: abrir, arrastre, setArrastre, ahora, conflictos };
    return (
        <div className="grid gap-4 [&>*]:min-w-0">
            {hecho ? (
                <Aviso tono="success" titulo="Listo" icono={CircleCheck} data-testid="movida">
                    {hecho}
                </Aviso>
            ) : null}
            {tecnicos.datos.cortada ? (
                <Aviso tono="info" titulo="Hay más técnicos">
                    Se muestran los primeros {tecnicos.datos.lista.length}. Filtra por cuadrilla o zona para ver el resto.
                </Aviso>
            ) : null}
            {conConflicto.length > 0 ? (
                <Aviso
                    tono="error"
                    icono={TriangleAlert}
                    titulo={conConflicto.length === 1 ? "1 orden con conflicto" : `${conConflicto.length} órdenes con conflicto`}
                    data-testid="conflictos"
                >
                    <ul className="grid gap-0.5">
                        {conConflicto.map((c) => (
                            <li key={c.ordenId}>
                                <span className="font-medium tabular-nums">{c.numero}</span> ({filas.find((f) => f.usuarioId === c.tecnicoId)?.nombre ?? `Técnico ${c.tecnicoId}`},{" "}
                                {etiquetaDia(diaDesdeIso(c.inicio) ?? desde)} {horaDe(c.inicio)}): {(conflictos.get(c.ordenId) ?? []).map(textoConflicto).join("; ")}.
                            </li>
                        ))}
                    </ul>
                    <p className="mt-1 text-muted-foreground">Reprográmalas con «Mover a…» o asígnalas a otro técnico.</p>
                </Aviso>
            ) : null}
            <CalendarioTecnicos vista={vista} desde={desde} {...comun} />
            {bandeja && sinProgramar.estado !== "listo" ? (
                sinProgramar.estado === "error" ? (
                    <ErrorLectura error={sinProgramar.error} reintentar={sinProgramar.reintentar} que="las órdenes sin programar" />
                ) : (
                    <Esqueleto className="h-16" />
                )
            ) : null}
            {bandeja && sinProgramar.estado === "listo" && sinProgramar.datos !== null ? (
                <Seccion
                    titulo="Sin programar"
                    descripcion="Arrastra una orden a la línea de un técnico para darle hora, o a su nombre para solo asignarla."
                    data-testid="sin-programar"
                >
                    <BandejaSinProgramar
                        ordenes={sinProgramar.datos.ordenes}
                        cortada={sinProgramar.datos.cortada}
                        zonas={zonas}
                        alMover={(m) => abrir(m, vista === "dia" ? { tecnicoId: null, dia: desde, minutos: null } : undefined)}
                        arrastre={arrastre}
                        setArrastre={setArrastre}
                    />
                </Seccion>
            ) : null}
            {abierto ? (
                <DialogoMoverOrden
                    key={`${abierto.ordenId}:${abierto.destino ? `${abierto.destino.tecnicoId}:${abierto.destino.dia}:${abierto.destino.minutos}` : ""}`}
                    movible={actual ?? abierto.foto}
                    desaparecida={actual === undefined}
                    destino={abierto.destino}
                    filas={filas}
                    hoy={hoyGuatemala(ahora)}
                    alCerrar={() => setAbierto(null)}
                    alHecho={alHecho}
                    recargar={datos.refrescar}
                />
            ) : null}
        </div>
    );
}
