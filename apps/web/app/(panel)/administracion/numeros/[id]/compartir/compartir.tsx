"use client";

import { zodResolver } from "@hookform/resolvers/zod";
import { avisar, Aviso, Boton, CampoArea, CampoSelector, Casilla, DialogoConfirmar, FormularioPagina, liberarProteccion, SeccionFormulario, Selector } from "@iris/ui";
import { ArrowRight, Building2, CircleCheck, Clock, Megaphone, MessageCircle, Plus, Search, Send, Share2, SlidersHorizontal, Split, Unlink, X } from "lucide-react";
import Link from "next/link";
import { useParams, useRouter } from "next/navigation";
import { useState, type FormEvent } from "react";
import { useForm } from "react-hook-form";
import { CargaRegistro } from "@/componentes/carga-registro";
import { useNombreEnMigas } from "@/componentes/migas-panel";
import { nuevaClaveIdempotencia } from "@/lib/api/cliente";
import { ErrorApi, mensajeDeError } from "@/lib/api/errores";
import { formatearNumero, nombreUso } from "@/lib/conversaciones/presentacion";
import { useLectura } from "@/lib/lectura";
import { guardarPrestamo, leerCanalMeta, leerOpcionesPrestamo, leerPrestamos, revocarPrestamo } from "@/lib/meta/api";
import { esquemaRevocar } from "@/lib/meta/esquemas";
import { etiquetaTipoConexion, iniciales } from "@/lib/meta/presentacion";
import type { CanalMeta, OpcionesPrestamo, Prestamo } from "@/lib/meta/tipos";
import { cuando, fechaCorta } from "@/lib/numeros/conexion";
import { USOS } from "@/lib/numeros/esquemas";
import { idDeParametro } from "@/lib/parametros";
import { useSesion } from "@/lib/sesion";

const LISTA = "/administracion/numeros";

/** Lo que se edita de cada empresa (los préstamos activos y los que se agregan sin guardar). */
type Borrador = { empresaId: number; nombre: string; usos: string[]; colaId: string; version: number | null; prestadoEn: string | null; original: string };
const firma = (b: Pick<Borrador, "usos" | "colaId">) => `${[...b.usos].sort().join(",")}|${b.colaId}`;

/**
 * `/administracion/numeros/[id]/compartir` (`canales.configurar`, en la empresa dueña): con qué
 * otras empresas de la instalación se comparte el número, con qué usos y en qué cola caen sus
 * conversaciones (contrato §15.3). «Dejar de compartir» confirma con el motivo.
 */
export function CompartirNumero() {
    const id = idDeParametro(useParams<{ id: string }>().id);
    const lectura = useLectura(async (signal) => {
        if (id === null) return null;
        const [canal, prestamos, opciones] = await Promise.all([leerCanalMeta(id, signal), leerPrestamos(id, signal), leerOpcionesPrestamo(id, signal)]);
        return { canal, prestamos, opciones };
    });
    useNombreEnMigas(lectura.estado === "listo" ? lectura.datos?.canal.nombre : null);
    return (
        <CargaRegistro lectura={lectura} que="el número" noEncontrado="Número no encontrado">
            {(d) => <Pantalla {...d} recargar={lectura.reintentar} />}
        </CargaRegistro>
    );
}

function aBorrador(p: Prestamo): Borrador {
    const b = { empresaId: p.empresaId, nombre: p.empresaNombre, usos: [...p.usos], colaId: p.colaId ? String(p.colaId) : "", version: p.version, prestadoEn: p.prestadoEn };
    return { ...b, original: firma(b) };
}

function Pantalla({ canal, prestamos, opciones, recargar }: { canal: CanalMeta; prestamos: Prestamo[]; opciones: OpcionesPrestamo; recargar: () => void }) {
    const router = useRouter();
    const { yo, puede } = useSesion();
    const [activos, setActivos] = useState(() => prestamos.filter((p) => p.estado === "activo"));
    const [borradores, setBorradores] = useState<Borrador[]>(() => prestamos.filter((p) => p.estado === "activo").map(aBorrador));
    const [agregando, setAgregando] = useState("");
    const [errores, setErrores] = useState<Record<number, string>>({});
    const [error, setError] = useState<unknown>(null);
    const [enviando, setEnviando] = useState(false);
    const [revocar, setRevocar] = useState<Prestamo | null>(null);
    const duena = yo.empresaActiva?.nombre ?? "tu empresa";
    const disponibles = opciones.empresas.filter((e) => !borradores.some((b) => b.empresaId === e.id));
    const cambios = borradores.filter((b) => b.version === null || firma(b) !== b.original);
    const sucio = cambios.length > 0;

    function cambiar(empresaId: number, c: Partial<Borrador>) {
        setBorradores((bs) => bs.map((b) => (b.empresaId === empresaId ? { ...b, ...c } : b)));
        setErrores((e) => Object.fromEntries(Object.entries(e).filter(([k]) => Number(k) !== empresaId)));
    }

    async function guardar(evento: FormEvent<HTMLFormElement>) {
        evento.preventDefault();
        const faltan: Record<number, string> = {};
        for (const b of cambios) if (b.usos.length === 0) faltan[b.empresaId] = "Elige al menos un uso";
        setErrores(faltan);
        if (Object.keys(faltan).length) return;
        setEnviando(true);
        setError(null);
        try {
            // Uno por empresa, en orden: si uno falla, lo anterior ya quedó y se dice cuál.
            for (const b of cambios) {
                try {
                    const p = await guardarPrestamo(canal.id, { empresaId: b.empresaId, usos: b.usos, colaId: b.colaId ? Number(b.colaId) : null, ...(b.version ? { version: b.version } : {}) }, nuevaClaveIdempotencia());
                    setBorradores((bs) => bs.map((x) => (x.empresaId === b.empresaId ? aBorrador(p) : x)));
                    setActivos((as) => [...as.filter((x) => x.empresaId !== p.empresaId), p]);
                } catch (e) {
                    setErrores((r) => ({ ...r, [b.empresaId]: e instanceof ErrorApi && e.errores.length ? e.errores.map((x) => x.message).join(". ") : mensajeDeError(e).titulo }));
                    throw e;
                }
            }
            avisar({ titulo: `Se guardó con quién se comparte «${canal.nombre}»` });
            await liberarProteccion();
            router.replace(LISTA);
        } catch (e) {
            setError(e);
        } finally {
            setEnviando(false);
        }
    }

    const conflicto = error instanceof ErrorApi && error.estado === 409;
    return (
        <FormularioPagina
            titulo={`Compartir ${canal.nombre}`}
            descripcion={
                <>
                    <span className="font-mono">{formatearNumero(canal.numero)}</span> · {etiquetaTipoConexion(canal.tipoConexion)} · de {duena}
                </>
            }
            alEnviar={(e) => void guardar(e)}
            navegar={router.push}
            volverA={LISTA}
            sucio={sucio}
            barra={{ textoGuardar: "Guardar", enviando, ...(sucio ? {} : { faltan: ["un cambio"], motivo: "No hay cambios por guardar." }) }}
            error={
                error ? (
                    <Aviso
                        tono="error"
                        titulo={conflicto ? "Alguien más cambió estos préstamos" : "No se pudo guardar todo"}
                        accion={
                            conflicto ? (
                                <Boton variante="contorno" tamano="sm" onClick={recargar}>
                                    Recargar
                                </Boton>
                            ) : undefined
                        }
                    >
                        {conflicto ? "Recarga para ver el cambio; lo que escribiste se pierde." : "Lo que está marcado abajo no se guardó; lo demás sí."}
                    </Aviso>
                ) : null
            }
        >
            {puede(["canales.configurar"]) ? (
                <div className="-mt-2 flex justify-end">
                    <Boton asChild variante="contorno" tamano="sm" className="w-full sm:w-auto">
                        <Link href={`${LISTA}/${canal.id}/configurar`}>
                            <SlidersHorizontal aria-hidden />
                            Uso, cola y horario
                        </Link>
                    </Boton>
                </div>
            ) : null}
            <SeccionFormulario
                titulo={
                    <span className="flex items-center gap-2">
                        <Share2 aria-hidden className="size-5" />
                        Compartir con otras empresas
                    </span>
                }
                descripcion="Otras empresas de este Iris pueden usar el número. No ven ni cambian sus credenciales, y cada una ve solo sus conversaciones."
            >
                {borradores.length === 0 ? <p className="text-sm text-muted-foreground">Todavía no se comparte con ninguna empresa.</p> : null}
                <ul className="grid gap-3" aria-label="Empresas que usan el número">
                    {borradores.map((b) => {
                        const empresa = opciones.empresas.find((e) => e.id === b.empresaId);
                        const prestamo = activos.find((p) => p.empresaId === b.empresaId);
                        return (
                            <li key={b.empresaId} className="overflow-hidden rounded-xl border border-border" data-empresa={b.empresaId}>
                                <div className="flex flex-wrap items-center gap-3 border-b border-border p-3 sm:p-4">
                                    <span aria-hidden className="grid size-9 shrink-0 place-items-center rounded-lg bg-info/10 text-sm font-semibold text-info">
                                        {iniciales(b.nombre)}
                                    </span>
                                    <div className="grid min-w-0 flex-1 gap-0.5">
                                        <p className="font-medium">{b.nombre}</p>
                                        {b.version === null ? (
                                            <p className="flex items-center gap-1 text-sm text-warning">
                                                <Clock aria-hidden className="size-3.5" />
                                                Sin guardar
                                            </p>
                                        ) : (
                                            <p className="flex items-center gap-1.5 text-sm text-success">
                                                <span aria-hidden className="size-2 rounded-full bg-current" />
                                                Compartido desde el {fechaCorta(b.prestadoEn ?? "")}
                                            </p>
                                        )}
                                    </div>
                                    {b.version === null ? (
                                        <Boton variante="fantasma" tamano="sm" onClick={() => setBorradores((bs) => bs.filter((x) => x.empresaId !== b.empresaId))} aria-label={`Quitar ${b.nombre}`}>
                                            <X aria-hidden />
                                            Quitar
                                        </Boton>
                                    ) : prestamo ? (
                                        <Boton variante="contorno" tamano="sm" className="border-error/60 text-error" onClick={() => setRevocar(prestamo)} aria-label={`Dejar de compartir con ${b.nombre}`}>
                                            <Unlink aria-hidden />
                                            Dejar de compartir
                                        </Boton>
                                    ) : null}
                                </div>
                                <div className="grid gap-4 p-3 sm:p-4 lg:grid-cols-[minmax(0,1.6fr)_minmax(0,1fr)]">
                                    <fieldset className="grid gap-2">
                                        <legend className="mb-1 text-sm font-medium">Usos permitidos</legend>
                                        <div className="grid grid-cols-2 gap-2 sm:grid-cols-4">
                                            {USOS.map((u) => (
                                                <Casilla
                                                    key={u}
                                                    etiqueta={nombreUso(u)}
                                                    checked={b.usos.includes(u)}
                                                    onChange={(e) => cambiar(b.empresaId, { usos: e.target.checked ? [...b.usos, u] : b.usos.filter((x) => x !== u) })}
                                                />
                                            ))}
                                        </div>
                                        <p className="text-sm text-muted-foreground">Para {b.nombre} sí limitan: solo puede mandar y contestar de esos usos.</p>
                                    </fieldset>
                                    <CampoSelector
                                        etiqueta={`Cola en ${b.nombre}`}
                                        ayuda="Ahí caen sus conversaciones."
                                        value={b.colaId}
                                        onChange={(e) => cambiar(b.empresaId, { colaId: e.target.value })}
                                    >
                                        <option value="">Sin cola</option>
                                        {(empresa?.colas ?? []).map((c) => (
                                            <option key={c.id} value={String(c.id)}>
                                                {c.nombre}
                                            </option>
                                        ))}
                                    </CampoSelector>
                                </div>
                                {errores[b.empresaId] ? (
                                    <p role="alert" className="border-t border-border px-3 py-2 text-sm text-error sm:px-4">
                                        {errores[b.empresaId]}
                                    </p>
                                ) : null}
                            </li>
                        );
                    })}
                </ul>
                <div className="flex flex-wrap items-end gap-2">
                    {disponibles.length > 0 ? (
                        <>
                            <Selector aria-label="Empresa para agregar" value={agregando} onChange={(e) => setAgregando(e.target.value)} className="min-w-48">
                                <option value="">Elige una empresa…</option>
                                {disponibles.map((e) => (
                                    <option key={e.id} value={String(e.id)}>
                                        {e.nombre}
                                    </option>
                                ))}
                            </Selector>
                            <Boton
                                variante="contorno"
                                disabled={!agregando}
                                onClick={() => {
                                    const e = disponibles.find((x) => String(x.id) === agregando);
                                    if (!e) return;
                                    setBorradores((bs) => [...bs, { empresaId: e.id, nombre: e.nombre, usos: [], colaId: "", version: null, prestadoEn: null, original: "" }]);
                                    setAgregando("");
                                }}
                            >
                                <Plus aria-hidden />
                                Agregar empresa
                            </Boton>
                        </>
                    ) : (
                        <p className="text-sm text-muted-foreground">Todas las empresas de este Iris ya están en la lista.</p>
                    )}
                </div>
            </SeccionFormulario>
            <AdondeVa canal={canal} duena={duena} empresas={borradores.map((b) => b.nombre)} />
            {revocar ? (
                <DialogoRevocar
                    canal={canal}
                    prestamo={revocar}
                    duena={duena}
                    cerrar={() => setRevocar(null)}
                    alHecho={(p) => {
                        setActivos((as) => as.filter((x) => x.empresaId !== p.empresaId));
                        setBorradores((bs) => bs.filter((x) => x.empresaId !== p.empresaId));
                    }}
                />
            ) : null}
        </FormularioPagina>
    );
}

/** Confirmación con el motivo (5–300, §15.3), los números que se cierran y los avisos que se cancelan. */
function DialogoRevocar({ canal, prestamo, duena, cerrar, alHecho }: { canal: CanalMeta; prestamo: Prestamo; duena: string; cerrar: () => void; alHecho: (p: Prestamo) => void }) {
    const form = useForm({ resolver: zodResolver(esquemaRevocar), defaultValues: { motivo: "" } });
    const [error, setError] = useState<unknown>(null);
    const [clave] = useState(() => nuevaClaveIdempotencia());
    async function confirmar() {
        setError(null);
        const ok = await form.trigger();
        if (!ok) return;
        try {
            const p = await revocarPrestamo(canal.id, prestamo.empresaId, { version: prestamo.version, motivo: form.getValues("motivo").trim() }, clave);
            alHecho(p);
            avisar({ titulo: `«${canal.nombre}» ya no se comparte con ${prestamo.empresaNombre}` });
            cerrar();
        } catch (e) {
            setError(e);
        }
    }
    const n = prestamo.conversacionesAbiertas;
    return (
        <DialogoConfirmar
            open
            onOpenChange={(a) => !a && cerrar()}
            titulo={`¿Dejar de compartir con ${prestamo.empresaNombre}?`}
            descripcion={`${prestamo.empresaNombre} deja de usar ${formatearNumero(canal.numero)} desde ya.`}
            textoConfirmar="Dejar de compartir"
            textoOcupado="Dejando de compartir…"
            peligro
            alConfirmar={confirmar}
        >
            {n > 0 ? (
                <Aviso tono="warning" titulo={`Tiene ${n} ${n === 1 ? "conversación abierta" : "conversaciones abiertas"} por este número`}>
                    Se cierran en {prestamo.empresaNombre} y quedan en su historial, de solo lectura. Si esos clientes vuelven a escribir, llegan a {duena}.
                </Aviso>
            ) : null}
            <ul className="grid list-disc gap-1 pl-5 text-sm">
                {prestamo.avisosProgramados > 0 ? (
                    <li>
                        Los {prestamo.avisosProgramados} avisos programados de {prestamo.empresaNombre} por este número se cancelan.
                    </li>
                ) : null}
                <li>Lo que ya se envió no cambia.</li>
                <li>Puedes volver a compartirlo después.</li>
            </ul>
            <CampoArea etiqueta="Motivo" rows={2} maxLength={300} placeholder="Por qué deja de compartirse" error={form.formState.errors.motivo?.message} {...form.register("motivo")} />
            {error ? (
                <Aviso tono="error" titulo="No se pudo dejar de compartir">
                    {mensajeDeError(error).titulo}
                </Aviso>
            ) : null}
        </DialogoConfirmar>
    );
}

/** Cómo decide Iris a qué empresa va una conversación que llega (§15.3), en palabras. */
function AdondeVa({ canal, duena, empresas }: { canal: CanalMeta; duena: string; empresas: string[] }) {
    const todas = [duena, ...empresas];
    const lista = todas.length > 1 ? `${todas.slice(0, -1).join(", ")} y ${todas.at(-1)}` : duena;
    return (
        <SeccionFormulario
            titulo={
                <span className="flex items-center gap-2">
                    <Split aria-hidden className="size-5" />
                    Adónde va una conversación que llega
                </span>
            }
            descripcion="Iris busca el teléfono del cliente en los clientes de cada empresa que usa el número."
        >
            <div className="grid items-center gap-3 lg:grid-cols-[1fr_auto_1fr_auto_1.2fr]">
                <Paso icono={MessageCircle} titulo="Llega un mensaje" texto={`Al ${formatearNumero(canal.numero)}, de un teléfono que no tiene conversación abierta.`} />
                <ArrowRight aria-hidden className="hidden size-4 text-muted-foreground lg:block" />
                <Paso icono={Search} titulo="¿De quién es el cliente?" texto={`Se compara el teléfono con los clientes de ${lista}.`} />
                <ArrowRight aria-hidden className="hidden size-4 text-muted-foreground lg:block" />
                <div className="grid gap-2">
                    <Paso icono={CircleCheck} titulo="Coincide con una sola empresa" texto="Va a esa empresa, a la cola que elegiste arriba." destacado />
                    <Paso icono={Building2} titulo="Ninguna o más de una" texto={`Va a ${duena}, la dueña del número. Desde ahí se puede pasar a otra empresa.`} />
                </div>
            </div>
            <ul className="divide-y divide-border overflow-hidden rounded-xl border border-border text-sm">
                <li className="flex items-start gap-3 p-3 sm:p-4">
                    <Send aria-hidden className="mt-0.5 size-4 shrink-0 text-muted-foreground" />
                    <div className="grid gap-0.5">
                        <p className="font-medium">Si la conversación la empezó una empresa, se queda en esa empresa</p>
                        <p className="text-muted-foreground">Por ejemplo, un aviso de otra empresa: si el cliente contesta en las 24 horas siguientes, cae en su cola.</p>
                    </div>
                </li>
                <li className="flex items-start gap-3 p-3 sm:p-4">
                    <Megaphone aria-hidden className="mt-0.5 size-4 shrink-0 text-warning" />
                    <div className="grid gap-0.5">
                        <p className="font-medium">La calidad y el límite de Meta son del número</p>
                        <p className="text-muted-foreground">Si los avisos de una empresa se bloquean o se marcan como spam, el número baja para todas.</p>
                    </div>
                </li>
            </ul>
            {canal.ultimoMensajeEn ? <p className="text-xs text-muted-foreground">Último mensaje de Meta a este número {cuando(canal.ultimoMensajeEn)}.</p> : null}
        </SeccionFormulario>
    );
}

function Paso({ icono: Icono, titulo, texto, destacado = false }: { icono: typeof Search; titulo: string; texto: string; destacado?: boolean }) {
    return (
        <div className={destacado ? "grid gap-1 rounded-xl border border-primary/40 bg-primary/5 p-3" : "grid gap-1 rounded-xl border border-border p-3"}>
            <p className="flex items-center gap-1.5 text-sm font-medium">
                <Icono aria-hidden className="size-4 shrink-0" />
                {titulo}
            </p>
            <p className="text-sm text-muted-foreground">{texto}</p>
        </div>
    );
}
