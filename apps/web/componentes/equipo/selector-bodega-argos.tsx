"use client";

import type { Schema } from "@iris/contratos";
import { Autocompletar, Aviso, Boton, Campo, Esqueleto } from "@iris/ui";
import { Link2, Pencil, RotateCw, Unplug, Warehouse } from "lucide-react";
import { useEffect, useState } from "react";
import type { UseFormRegisterReturn } from "react-hook-form";
import type { Tecnico } from "@/lib/agenda/esquemas";
import { bodegaEnEmpresa, opcionesBodegaArgos, SIN_INVENTARIO_POR_TECNICO, sinListaDeArgos, textoBodegaTecnico } from "@/lib/agenda/argos";
import { api, resolver } from "@/lib/api/cliente";
import { ErrorApi, mensajeDeError } from "@/lib/api/errores";
import { usePaginado } from "@/lib/paginado";
import { useSesion } from "@/lib/sesion";

type TecnicoArgos = Schema<"TecnicoDeArgos">;

/** Lo elegido en el selector: el técnico de Argos y su bodega en la empresa activa (o `null`). */
export type EleccionArgos = { idExterno: string; bodegaId: number | null; texto: string };

type Props = {
    /** Ficha que se edita (su vínculo actual se muestra fijo con «Cambiar»). */
    tecnico?: Tecnico | undefined;
    /** `argosIdExterno` del formulario. */
    valor: string;
    alElegir: (e: EleccionArgos | null) => void;
    /** Respaldo a mano (`bodegaId`), solo si Argos no puede dar la lista. */
    registroBodega: UseFormRegisterReturn<"bodegaId">;
    errorBodega: string | undefined;
    /** Técnico de Argos a dejar elegido al aparecer en la lista («Crear ficha» desde Argos). */
    preferido?: string | null;
};

const LIMITE = 100;

/**
 * Bodega de Argos del técnico, por nombre: lista los técnicos de Argos aún sin ficha
 * (`GET /integraciones/argosisp/tecnicos?sinVincular=true`) como «Bodega Norte · Juan Pérez», con
 * búsqueda. Lo elegido se vincula al guardar (`vincular-argos`) y deja su bodega en la ficha. El ya
 * vinculado se muestra fijo con «Cambiar». Si Argos no puede dar la lista (apagado, caído o
 * anterior a 1.6.0), el error con «Reintentar» y el número de bodega a mano como respaldo.
 */
export function SelectorBodegaArgos({ tecnico, valor, alElegir, registroBodega, errorBodega, preferido = null }: Props) {
    const { puede, yo } = useSesion();
    const empresa = yo.empresaActiva?.id ?? null;
    const vinculado = tecnico?.argos ?? null;
    const [cambiando, setCambiando] = useState(false);

    if (!puede(["agenda.vincular_argos"])) {
        return (
            <div className="grid gap-1 text-sm" data-testid="bodega-argos">
                <span className="font-medium">Bodega en Argos</span>
                <span>
                    {vinculado ? `${vinculado.nombre} · ` : ""}
                    {tecnico?.bodegaId ? `Bodega ${tecnico.bodegaId}` : <span className="text-muted-foreground">Sin bodega</span>}
                </span>
                <span className="text-xs text-muted-foreground">La elige de la lista de Argos quien vincula técnicos (administración).</span>
            </div>
        );
    }

    if (vinculado && !cambiando) {
        return (
            <div className="grid gap-1.5" data-testid="bodega-argos">
                <span className="text-sm font-medium">Bodega en Argos</span>
                <div className="flex flex-wrap items-center justify-between gap-2 rounded-lg border border-border px-3 py-2">
                    <span className="flex min-w-0 items-center gap-2 text-sm">
                        <Link2 className="size-4 shrink-0 text-primary" aria-hidden />
                        <span className="min-w-0">
                            <span className="block truncate font-medium">
                                {tecnico?.bodegaId ? `Bodega ${tecnico.bodegaId}` : "Su bodega en Argos"} · {vinculado.nombre}
                            </span>
                            <span className="block truncate text-xs text-muted-foreground">Vinculado · {vinculado.correo}</span>
                        </span>
                    </span>
                    <Boton variante="secundario" tamano="sm" onClick={() => setCambiando(true)}>
                        <Pencil />
                        Cambiar
                    </Boton>
                </div>
            </div>
        );
    }

    return (
        <Lista
            tecnico={tecnico}
            valor={valor}
            alElegir={alElegir}
            registroBodega={registroBodega}
            errorBodega={errorBodega}
            preferido={preferido}
            empresa={empresa}
            {...(vinculado
                ? {
                      alMantener: () => {
                          alElegir(null);
                          setCambiando(false);
                      },
                  }
                : {})}
        />
    );
}

function Lista({
    tecnico,
    valor,
    alElegir,
    registroBodega,
    errorBodega,
    preferido,
    empresa,
    alMantener,
}: Omit<Props, "preferido"> & { preferido: string | null; empresa: number | null; alMantener?: () => void }) {
    const lista = usePaginado<TecnicoArgos>((cursor, signal) =>
        resolver(
            api().GET("/api/v1/integraciones/argosisp/tecnicos", {
                params: { query: { limit: LIMITE, sinVincular: true, ...(cursor ? { cursor } : {}) } },
                ...(signal ? { signal } : {}),
            }),
        ),
    );
    const { primera, items } = lista;
    const encontrado = preferido !== null ? items.find((a) => a.idExterno === preferido) : undefined;
    useEffect(() => {
        if (encontrado && encontrado.enEmpresa) elegir(encontrado);
        // Solo al aparecer en la lista: elegir otro después no se pisa.
        // eslint-disable-next-line react-hooks/exhaustive-deps
    }, [encontrado?.idExterno]);

    function elegir(a: TecnicoArgos | undefined) {
        if (!a) {
            alElegir(null);
            return;
        }
        alElegir({ idExterno: a.idExterno, bodegaId: bodegaEnEmpresa(a, empresa)?.id ?? null, texto: textoBodegaTecnico(a, empresa) });
    }

    const mantener = alMantener ? (
        <Boton variante="fantasma" tamano="sm" className="justify-self-start" onClick={alMantener}>
            Mantener {tecnico?.argos?.nombre ?? "la actual"}
        </Boton>
    ) : null;

    if (primera.estado === "cargando") {
        return (
            <div className="grid gap-1.5" data-testid="bodega-argos">
                <span className="text-sm font-medium">Bodega en Argos</span>
                <Esqueleto className="h-10" />
            </div>
        );
    }
    if (primera.estado === "error") {
        const sinCapacidad = primera.error instanceof ErrorApi && primera.error.codigo === "argos_sin_inventario_tecnico";
        const respaldo = sinListaDeArgos(primera.error);
        return (
            <div className="grid gap-3" data-testid="bodega-argos">
                <Aviso
                    tono={sinCapacidad ? "warning" : "error"}
                    icono={sinCapacidad ? Unplug : undefined}
                    titulo={sinCapacidad ? SIN_INVENTARIO_POR_TECNICO : "No se pudo leer la lista de bodegas de Argos"}
                    accion={
                        sinCapacidad ? undefined : (
                            <Boton variante="secundario" tamano="sm" onClick={primera.reintentar}>
                                <RotateCw />
                                Reintentar
                            </Boton>
                        )
                    }
                >
                    {sinCapacidad
                        ? "Actualiza Argos a 1.6.0 o más para elegir la bodega por nombre."
                        : [mensajeDeError(primera.error).titulo, mensajeDeError(primera.error).detalle].filter(Boolean).join(". ")}
                    {respaldo ? <span className="block">Mientras tanto puedes escribir el número de bodega.</span> : null}
                </Aviso>
                {respaldo ? (
                    <Campo
                        etiqueta="Número de bodega en Argos"
                        inputMode="numeric"
                        placeholder="Opcional"
                        ayuda="Solo como respaldo: cópialo exacto de Argos."
                        error={errorBodega}
                        {...registroBodega}
                    />
                ) : null}
                {mantener}
            </div>
        );
    }

    const opciones = opcionesBodegaArgos(items, empresa);
    const vacio = items.length === 0;
    const manual = !tecnico?.argos && tecnico?.bodegaId ? tecnico.bodegaId : null;
    return (
        <div className="grid gap-2" data-testid="bodega-argos">
            <Autocompletar
                etiqueta="Bodega en Argos"
                opciones={opciones}
                valor={valor === "" ? null : valor}
                alCambiar={(v) => elegir(v === null ? undefined : items.find((a) => a.idExterno === v))}
                placeholder="Busca por bodega, nombre o correo"
                sinResultados="Ningún técnico de Argos sin ficha coincide."
                disabled={vacio}
                ayuda={
                    vacio
                        ? "Argos no tiene técnicos con bodega sin ficha en Iris. Crea su bodega técnico en Argos."
                        : manual !== null
                          ? `Hoy: bodega ${manual}, escrita a mano. Elige su técnico de Argos para dejarla por nombre y vinculada.`
                          : "Técnicos de Argos con bodega y aún sin ficha. Al guardar queda vinculado: sus consumos salen de esa bodega."
                }
            />
            <div className="flex flex-wrap items-center gap-2">
                {lista.siguiente ? (
                    <Boton variante="fantasma" tamano="sm" onClick={() => void lista.cargarMas()} disabled={lista.cargandoMas}>
                        {lista.cargandoMas ? "Cargando…" : "Cargar más de Argos"}
                    </Boton>
                ) : null}
                {valor !== "" && valor !== tecnico?.argos?.idExterno ? (
                    <Boton variante="fantasma" tamano="sm" onClick={() => elegir(undefined)}>
                        <Warehouse />
                        Quitar selección
                    </Boton>
                ) : null}
                {mantener}
            </div>
            {lista.errorMas ? (
                <Aviso tono="error" titulo="No se pudieron leer más técnicos de Argos">
                    {mensajeDeError(lista.errorMas).titulo}
                </Aviso>
            ) : null}
        </div>
    );
}
