"use client";

import type { Schema } from "@iris/contratos";
import { Aviso, Boton, Cargando, Entrada, Etiqueta, InsigniaEstado } from "@iris/ui";
import { History, PlugZap, Search, UserRound, X } from "lucide-react";
import { useEffect, useId, useState } from "react";
import { api, resolver } from "@/lib/api/cliente";
import { ErrorApi, mensajeDeError } from "@/lib/api/errores";
import { useDiferido } from "@/lib/diferido";

export type ClienteResumen = Schema<"ClienteResumen">;

type Resultado =
    | { q: string; estado: "listo"; datos: Schema<"ResultadoBusqueda"> }
    | { q: string; estado: "error"; error: unknown };

/**
 * Búsqueda de cliente en Argos (`GET /clientes?q=`). Sin conector (503 `conector_apagado`) avisa
 * para capturar el contacto a mano; con `stale`, deja claro que son datos de la copia local.
 */
export function BuscadorCliente({
    elegido,
    alElegir,
    alConectorApagado,
}: {
    elegido: ClienteResumen | null;
    alElegir: (cliente: ClienteResumen | null) => void;
    alConectorApagado: (apagado: boolean) => void;
}) {
    const id = useId();
    const [texto, setTexto] = useState("");
    const q = useDiferido(texto.trim(), 350);
    const [resultado, setResultado] = useState<Resultado | null>(null);

    useEffect(() => {
        if (q.length < 2 || elegido) {
            return;
        }
        const control = new AbortController();
        resolver(api().GET("/api/v1/clientes", { params: { query: { q: q.slice(0, 100), limit: 8 } }, signal: control.signal })).then(
            (datos) => {
                if (!control.signal.aborted) {
                    setResultado({ q, estado: "listo", datos });
                    alConectorApagado(false);
                }
            },
            (error: unknown) => {
                if (!control.signal.aborted) {
                    setResultado({ q, estado: "error", error });
                    alConectorApagado(error instanceof ErrorApi && error.codigo === "conector_apagado");
                }
            },
        );
        return () => control.abort();
    }, [q, elegido, alConectorApagado]);

    if (elegido) {
        return (
            <div className="grid gap-1.5">
                <span className="text-sm font-medium">Cliente</span>
                <div className="flex items-center gap-3 rounded-md border bg-elevated p-3" data-testid="cliente-elegido">
                    <UserRound aria-hidden className="size-5 shrink-0 text-muted-foreground" />
                    <span className="grid min-w-0 flex-1">
                        <span className="truncate font-medium">{elegido.nombre}</span>
                        <span className="truncate text-sm text-muted-foreground">
                            <span className="font-mono">{elegido.codigo}</span> · {elegido.direccion}
                        </span>
                    </span>
                    <Boton variante="fantasma" tamano="icono-sm" aria-label="Quitar cliente" title="Quitar cliente" onClick={() => alElegir(null)}>
                        <X />
                    </Boton>
                </div>
            </div>
        );
    }

    const vigente = resultado && resultado.q === q ? resultado : null;
    const buscando = q.length >= 2 && !vigente;
    const apagado = vigente?.estado === "error" && vigente.error instanceof ErrorApi && vigente.error.codigo === "conector_apagado";

    return (
        <div className="grid gap-1.5">
            <Etiqueta htmlFor={id}>Cliente en Argos</Etiqueta>
            <span className="relative">
                <Search aria-hidden className="pointer-events-none absolute top-1/2 left-3 size-4 -translate-y-1/2 text-muted-foreground" />
                <Entrada
                    id={id}
                    type="search"
                    className="pl-9"
                    placeholder="Nombre, código, teléfono o dirección"
                    maxLength={100}
                    value={texto}
                    onChange={(e) => setTexto(e.target.value)}
                    aria-describedby={`${id}-ayuda`}
                />
            </span>
            <p id={`${id}-ayuda`} className="text-sm text-muted-foreground">
                Escribe al menos 2 caracteres. Si no es cliente, deja esto vacío y captura el contacto.
            </p>
            {buscando ? <Cargando texto="Buscando clientes…" visible /> : null}
            {apagado ? (
                <Aviso tono="neutral" titulo="Conector con Argos apagado" icono={PlugZap}>
                    No se puede buscar el cliente. Captura los datos de contacto a mano.
                </Aviso>
            ) : vigente?.estado === "error" ? (
                <Aviso tono="error" titulo="No se pudo buscar en Argos">
                    {mensajeDeError(vigente.error).titulo}. Puedes capturar el contacto a mano.
                </Aviso>
            ) : vigente?.estado === "listo" ? (
                <div className="grid gap-1.5">
                    {vigente.datos.stale ? (
                        <InsigniaEstado
                            tono="warning"
                            icono={History}
                            texto="Desactualizado: Argos no respondió, resultados de la copia local"
                            className="justify-self-start whitespace-normal"
                        />
                    ) : null}
                    {(vigente.datos.data ?? []).length === 0 ? (
                        <p className="text-sm text-muted-foreground">Ningún cliente coincide con «{q}».</p>
                    ) : (
                        <ul className="grid gap-1" aria-label="Clientes encontrados">
                            {(vigente.datos.data ?? []).map((c) => (
                                <li key={c.id}>
                                    <Boton variante="fantasma" className="h-auto w-full justify-start px-3 py-2 text-left whitespace-normal" onClick={() => alElegir(c)}>
                                        <span className="grid min-w-0">
                                            <span className="font-medium">{c.nombre}</span>
                                            <span className="text-sm font-normal text-muted-foreground">
                                                <span className="font-mono">{c.codigo}</span> · {c.direccion}
                                            </span>
                                        </span>
                                    </Boton>
                                </li>
                            ))}
                        </ul>
                    )}
                </div>
            ) : null}
        </div>
    );
}
