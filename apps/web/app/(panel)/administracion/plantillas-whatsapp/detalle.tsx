"use client";

import { avisar, Aviso, Boton, Casilla, Dialogo, DialogoCerrar, DialogoContenido, DialogoPie, Tarjeta } from "@iris/ui";
import { Building2, CircleAlert, Pencil, Play, Send, Trash2, TriangleAlert } from "lucide-react";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { useState } from "react";
import { EncabezadoPagina } from "@/componentes/encabezado-pagina";
import { useClaveIdempotencia } from "@/lib/idempotencia";
import * as P from "@/lib/plantillas-whatsapp/api";
import {
    calidadVisible,
    CODIGOS,
    cuandoCorto,
    estadoVisible,
    PERMISO_BORRAR,
    PERMISO_CREAR,
    PERMISO_EDITAR,
    RUTA,
    textoAtender,
    textoCategoria,
    textoCodigo,
    textoCodigos,
    textoIdioma,
    textoUso,
    USOS,
    type PlantillaWA,
    type PlantillaWADetalle,
} from "@/lib/plantillas-whatsapp/tipos";
import { useSesion } from "@/lib/sesion";
import { EstadoPlantilla, ErrorPlantilla, VistaPrevia } from "./comunes";

type Historial = PlantillaWADetalle["historial"][number];

const USABLES: readonly string[] = ["APPROVED", "FLAGGED"];
const CORREGIBLES: readonly string[] = ["APPROVED", "REJECTED", "PAUSED", "FLAGGED"];

/** Pantalla c de la maqueta: la ficha de una plantilla. */
export function DetallePlantillaWA({ plantilla: p, alCambiar, recargar }: { plantilla: PlantillaWADetalle; alCambiar: (p: PlantillaWADetalle) => void; recargar: () => void }) {
    const { puede } = useSesion();
    const router = useRouter();
    const editar = puede([PERMISO_EDITAR]);
    const clave = useClaveIdempotencia();
    const [accion, setAccion] = useState<"enviar" | "reanudar" | null>(null);
    const [error, setError] = useState<unknown>(null);
    const [borrando, setBorrando] = useState(false);
    const atender = textoAtender(p);
    const ejemplo = (n: string) => p.definicion.variables.find((v) => v.nombre === n)?.ejemplo ?? n;

    async function ejecutar(cual: "enviar" | "reanudar") {
        setAccion(cual);
        setError(null);
        try {
            const r = cual === "enviar" ? await P.enviar(p.id, p.version, clave.actual()) : await P.reanudar(p.id, p.version, clave.actual());
            clave.renovar();
            avisar({ titulo: cual === "enviar" ? `«${p.etiqueta}» enviada a Meta` : `«${p.etiqueta}» reanudada` });
            alCambiar({ ...p, ...r });
            recargar();
        } catch (e) {
            clave.trasError(e);
            setError(e);
        } finally {
            setAccion(null);
        }
    }

    const acciones = (
        <div className="flex flex-wrap gap-2">
            {p.estado === "BORRADOR" && editar ? (
                <Boton asChild variante="contorno">
                    <Link href={`${RUTA}/${p.id}/editar`}>
                        <Pencil />
                        Editar
                    </Link>
                </Boton>
            ) : null}
            {p.estado === "BORRADOR" && puede([PERMISO_CREAR]) ? (
                <Boton onClick={() => void ejecutar("enviar")} disabled={accion !== null}>
                    <Send />
                    {accion === "enviar" ? "Enviando…" : "Enviar a Meta"}
                </Boton>
            ) : null}
            {p.estado === "PAUSED" && editar ? (
                <Boton variante="contorno" onClick={() => void ejecutar("reanudar")} disabled={accion !== null}>
                    <Play />
                    {accion === "reanudar" ? "Reanudando…" : "Reanudar"}
                </Boton>
            ) : null}
            {CORREGIBLES.includes(p.estado) && p.estado !== "REJECTED" && editar ? (
                <Boton asChild variante="contorno">
                    <Link href={`${RUTA}/${p.id}/editar`}>
                        <Pencil />
                        Corregir y volver a enviar
                    </Link>
                </Boton>
            ) : null}
            {p.estado !== "DELETED" && puede([PERMISO_BORRAR]) ? (
                <Boton variante="contorno" className="text-error" onClick={() => setBorrando(true)}>
                    <Trash2 />
                    Borrar
                </Boton>
            ) : null}
        </div>
    );

    return (
        <>
            <EncabezadoPagina
                titulo={p.etiqueta}
                descripcion={
                    <span className="flex flex-wrap items-center gap-x-2 gap-y-1">
                        <EstadoPlantilla plantilla={p} />
                        <span>
                            <code className="font-mono">{p.nombre}</code> · {textoCategoria(p.categoria)} · {textoIdioma(p.idioma)}
                        </span>
                    </span>
                }
                acciones={acciones}
            />
            <div className="grid gap-4 lg:grid-cols-[minmax(0,1fr)_20rem] lg:items-start">
                <div className="grid min-w-0 gap-4">
                    {error ? <ErrorPlantilla error={error} /> : null}
                    {p.estado === "REJECTED" ? (
                        <Aviso tono="error" titulo={`Meta la rechazó ${cuandoCorto(p.estadoEn).toLowerCase()}`} data-slot="rechazo">
                            {p.rechazo?.motivo ? <p>{p.rechazo.motivo}</p> : <p>Meta no dijo el motivo en detalle.</p>}
                            {p.rechazo?.recomendacion ? (
                                <p className="mt-1">
                                    <strong>Cómo arreglarlo:</strong> {p.rechazo.recomendacion}
                                </p>
                            ) : null}
                            <div className="mt-2 flex flex-wrap gap-2">
                                {editar ? (
                                    <Boton asChild tamano="sm">
                                        <Link href={`${RUTA}/${p.id}/editar`}>
                                            <Pencil />
                                            Corregir y volver a enviar
                                        </Link>
                                    </Boton>
                                ) : null}
                            </div>
                            {p.motivo ? (
                                <p className="mt-2 rounded border border-dashed border-error/30 bg-background/60 p-2 text-xs">
                                    Lo que dijo Meta: <code className="font-mono">{p.motivo}</code>
                                </p>
                            ) : null}
                        </Aviso>
                    ) : atender ? (
                        <Aviso tono={estadoVisible(p).tono === "error" ? "error" : "warning"} icono={CircleAlert} titulo={atender} data-slot="atender" />
                    ) : null}
                    {p.codigos.length > 0 && !USABLES.includes(p.estado) && p.estado !== "DELETED" ? (
                        <Aviso tono="warning" icono={TriangleAlert} titulo={`Mientras no esté aprobada, Iris no manda ${textoCodigos(p.codigos)} con esta plantilla`}>
                            Argos los sigue pidiendo: quedan como «canal no disponible» y Argos usa su otro medio. Al aprobarse, salen solos los nuevos.
                        </Aviso>
                    ) : null}

                    <Tarjeta className="p-0">
                        <h2 className="border-b border-border px-4 py-3 text-base font-semibold">Datos de la plantilla</h2>
                        <dl className="grid gap-x-4 gap-y-2 p-4 text-sm sm:grid-cols-[10rem_minmax(0,1fr)]">
                            <dt className="text-muted-foreground">Categoría</dt>
                            <dd>
                                {textoCategoria(p.categoria)} <span className="text-muted-foreground">(Meta: {p.categoria})</span>
                                {p.categoriaAnterior ? <span className="text-muted-foreground"> · antes {textoCategoria(p.categoriaAnterior)}</span> : null}
                            </dd>
                            <dt className="text-muted-foreground">Idioma</dt>
                            <dd>
                                {textoIdioma(p.idioma)} <code className="font-mono text-xs text-muted-foreground">{p.idioma}</code>
                            </dd>
                            <dt className="text-muted-foreground">Calidad</dt>
                            <dd>{calidadVisible(p)?.texto ?? "Sin datos hasta que se apruebe y se use"}</dd>
                            <dt className="text-muted-foreground">Ediciones</dt>
                            <dd>
                                {p.estado === "APPROVED"
                                    ? `${p.ediciones.ultimas24h} hoy, ${p.ediciones.ultimos30d} en 30 días (Meta deja 1 por día y 10 por mes).`
                                    : "Sin límite mientras no esté aprobada. Ya aprobada: 1 por día y 10 por mes."}
                            </dd>
                            <dt className="text-muted-foreground">Creada</dt>
                            <dd>
                                {cuandoCorto(p.createdAt)}
                                {p.sugerida ? " desde «Sugeridas»" : p.origen === "meta" ? " en Meta" : p.origen === "migrada" ? " (venía del número)" : ""}
                            </dd>
                            {p.metaId ? (
                                <>
                                    <dt className="text-muted-foreground">Id en Meta</dt>
                                    <dd className="font-mono text-xs break-all text-muted-foreground">{p.metaId}</dd>
                                </>
                            ) : null}
                        </dl>
                    </Tarjeta>

                    <UsosPlantilla p={p} puedeEditar={editar && p.estado !== "DELETED"} alCambiar={(x) => alCambiar({ ...p, ...x })} />

                    <Tarjeta className="p-0">
                        <h2 className="border-b border-border px-4 py-3 text-base font-semibold">Historial</h2>
                        <ol className="grid gap-3 p-4" data-slot="historial">
                            {p.historial.map((h, i) => (
                                <li key={i} className="grid gap-0.5 text-sm">
                                    <span className="font-medium">{textoHistorial(h)}</span>
                                    <span className="text-xs text-muted-foreground">
                                        {cuandoCorto(h.ocurridoEn)} · {h.origen === "webhook" ? "aviso de Meta" : h.origen === "sincronizacion" ? "sincronización" : h.origen === "sistema" ? "Iris" : (h.actorNombre ?? "alguien")}
                                    </span>
                                </li>
                            ))}
                        </ol>
                    </Tarjeta>
                </div>
                <aside className="grid gap-2 lg:sticky lg:top-4" aria-label="Lo que se envió a Meta">
                    <VistaPrevia definicion={p.definicion} categoria={p.categoria} valor={ejemplo} titulo="Lo que se envió a Meta · con los ejemplos" />
                </aside>
            </div>
            {borrando ? (
                <DialogoBorrar
                    p={p}
                    alCerrar={() => setBorrando(false)}
                    alBorrar={() => {
                        setBorrando(false);
                        router.push(RUTA);
                    }}
                />
            ) : null}
        </>
    );
}

function textoHistorial(h: Historial): string {
    const estado = (e: string | null) => (e ? estadoVisible({ estado: e as PlantillaWA["estado"], pausadaHasta: null, borradaPor: null }).texto : "");
    switch (h.tipo) {
        case "creada":
            return h.a === "BORRADOR" ? "Creada como borrador" : "Creada";
        case "enviada":
            return "Enviada a revisión";
        case "editada":
            return "Mensaje cambiado";
        case "estado":
            return `${estado(h.de)} → ${estado(h.a)}`;
        case "categoria":
            return `Categoría: ${textoCategoria(h.de)} → ${textoCategoria(h.a)}`;
        case "calidad":
            return `Calidad: ${h.a ?? ""}`;
        case "usos":
            return h.a ? `Atiende «${textoCodigo(h.a)}»` : h.de ? `Ya no atiende «${textoCodigo(h.de)}»` : "Usos cambiados";
        case "borrada":
            return "Borrada";
        case "reanudada":
            return "Reanudada";
    }
    return h.tipo;
}

/** «Dónde se usa en Iris»: usos y códigos del catálogo (sin pasar por Meta). */
function UsosPlantilla({ p, puedeEditar, alCambiar }: { p: PlantillaWADetalle; puedeEditar: boolean; alCambiar: (p: PlantillaWA) => void }) {
    const clave = useClaveIdempotencia();
    const [usos, setUsos] = useState<string[]>(p.usos);
    const [codigos, setCodigos] = useState<string[]>(p.codigos);
    const [guardando, setGuardando] = useState(false);
    const [error, setError] = useState<unknown>(null);
    const cambio = usos.slice().sort().join() !== p.usos.slice().sort().join() || codigos.slice().sort().join() !== p.codigos.slice().sort().join();
    const alternar = (lista: string[], x: string, si: boolean) => (si ? [...lista.filter((y) => y !== x), x] : lista.filter((y) => y !== x));

    async function guardar() {
        setGuardando(true);
        setError(null);
        try {
            const r = await P.cambiarUsos(p.id, { usos, codigos, version: p.version }, clave.actual());
            clave.renovar();
            avisar({ titulo: "Usos guardados" });
            alCambiar(r);
        } catch (e) {
            clave.trasError(e);
            setError(e);
        } finally {
            setGuardando(false);
        }
    }

    return (
        <Tarjeta className="p-0" data-slot="usos">
            <h2 className="border-b border-border px-4 py-3 text-base font-semibold">Dónde se usa en Iris</h2>
            <div className="grid gap-4 p-4">
                {puedeEditar ? (
                    <>
                        <fieldset className="grid gap-2">
                            <legend className="mb-1 text-sm font-medium">Usos</legend>
                            {USOS.map((u) => (
                                <Casilla key={u.valor} etiqueta={u.texto} ayuda={u.detalle} checked={usos.includes(u.valor)} onChange={(e) => setUsos(alternar(usos, u.valor, e.target.checked))} />
                            ))}
                        </fieldset>
                        <fieldset className="grid gap-2">
                            <legend className="mb-1 text-sm font-medium">Avisos que atiende</legend>
                            <p className="text-xs text-muted-foreground">Si otra plantilla de la cuenta atendía uno, lo suelta.</p>
                            <div className="grid gap-1.5 sm:grid-cols-2">
                                {Object.keys(CODIGOS).map((c) => (
                                    <Casilla key={c} etiqueta={textoCodigo(c)} checked={codigos.includes(c)} onChange={(e) => setCodigos(alternar(codigos, c, e.target.checked))} />
                                ))}
                            </div>
                        </fieldset>
                        {error ? <ErrorPlantilla error={error} /> : null}
                        <div>
                            <Boton onClick={() => void guardar()} disabled={!cambio || guardando}>
                                {guardando ? "Guardando…" : "Guardar usos"}
                            </Boton>
                        </div>
                    </>
                ) : (
                    <p className="text-sm">
                        {p.usos.length ? p.usos.map(textoUso).join(", ") : "Sin usar en Iris"}
                        {p.codigos.length ? ` · atiende ${textoCodigos(p.codigos)}` : ""}
                    </p>
                )}
                {p.prestatarias.length > 0 ? (
                    <div className="flex items-start gap-2 rounded-lg border border-border p-3 text-sm">
                        <Building2 aria-hidden className="mt-0.5 size-4 shrink-0 text-muted-foreground" />
                        <span>
                            <strong>Empresas a las que prestas el número:</strong> {p.prestatarias.map((x) => x.nombre).join(", ")} también la usan.
                        </span>
                    </div>
                ) : null}
            </div>
        </Tarjeta>
    );
}

/** Confirmación de borrar (avisa el bloqueo de 30 días del nombre si estuvo aprobada). */
function DialogoBorrar({ p, alCerrar, alBorrar }: { p: PlantillaWADetalle; alCerrar: () => void; alBorrar: () => void }) {
    const clave = useClaveIdempotencia();
    const [borrando, setBorrando] = useState(false);
    const [error, setError] = useState<unknown>(null);
    const enMeta = p.metaId !== null && p.estado !== "BORRADOR";
    async function borrar() {
        setBorrando(true);
        setError(null);
        try {
            await P.borrar(p.id, p.version, clave.actual());
            clave.renovar();
            avisar({ titulo: `«${p.etiqueta}» borrada` });
            alBorrar();
        } catch (e) {
            clave.trasError(e);
            setError(e);
            setBorrando(false);
        }
    }
    return (
        <Dialogo open onOpenChange={(a) => !a && !borrando && alCerrar()}>
            <DialogoContenido titulo={`¿Borrar «${p.etiqueta}»?`} descripcion={enMeta ? "Se borra también en Meta." : "Es un borrador: solo se borra en Iris."}>
                {enMeta ? (
                    <ul className="grid list-disc gap-1 pl-5 text-sm">
                        <li>Meta no deja usar el nombre «{p.nombre}» otra vez durante 30 días.</li>
                        {p.codigos.length > 0 ? <li>Iris deja de mandar {textoCodigos(p.codigos)} por WhatsApp hasta que elijas otra.</li> : null}
                    </ul>
                ) : null}
                {error ? <ErrorPlantilla error={error} /> : null}
                <DialogoPie>
                    <DialogoCerrar asChild>
                        <Boton variante="contorno" disabled={borrando}>
                            Cancelar
                        </Boton>
                    </DialogoCerrar>
                    <Boton variante="peligro" onClick={() => void borrar()} disabled={borrando}>
                        {borrando ? "Borrando…" : "Borrar"}
                    </Boton>
                </DialogoPie>
            </DialogoContenido>
        </Dialogo>
    );
}
