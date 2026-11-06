"use client";

import type { Schema } from "@iris/contratos";
import { avisar, Aviso, Boton, CampoBusqueda, Cargando, Dialogo, DialogoContenido } from "@iris/ui";
import { BookOpen, Eye, PencilLine, RotateCw, Send, Tag, Ticket, Wrench } from "lucide-react";
import Link from "next/link";
import { useState } from "react";
import { MarkdownSeguro, markdownATexto } from "@/componentes/manuales/markdown";
import { api, resolver } from "@/lib/api/cliente";
import { ErrorApi, mensajeDeError } from "@/lib/api/errores";
import { cabeceraIdem, nuevoIdMensaje } from "@/lib/conversaciones/api";
import { esSesionCambiada, verificarSesion, type Identidad } from "@/lib/conversaciones/sesion";
import type { Conversacion } from "@/lib/conversaciones/tipos";
import { useDiferido } from "@/lib/diferido";
import { useLectura } from "@/lib/lectura";
import { useSesion } from "@/lib/sesion";

type Manual = Schema<"Manual">;
type Sugerido = Schema<"ManualSugerido">;

const MOTIVO: Record<string, { texto: string; icono: typeof Tag }> = {
    etiqueta: { texto: "Comparte una etiqueta", icono: Tag },
    equipo: { texto: "Equipo del servicio", icono: Wrench },
    ticket_abierto: { texto: "Ticket abierto del cliente", icono: Ticket },
};

/**
 * Pestaña «Ayuda» de la ficha (§11): manuales sugeridos para la conversación (con su motivo) y la
 * búsqueda en la biblioteca. «Ver» muestra el artículo con la vista segura; «Usar en la respuesta»
 * lo pasa como texto al compositor; «Enviar al cliente» (solo artículos para el cliente) manda el
 * enlace firmado, dentro de la ventana de 24 h.
 */
export function AyudaManuales({ conv, identidad, alUsarTexto }: { conv: Conversacion; identidad: Identidad; alUsarTexto: (t: string) => void }) {
    const [texto, setTexto] = useState("");
    const q = useDiferido(texto.trim(), 300);
    const sugeridos = useLectura(async (signal) => (await resolver(api().GET("/api/v1/mensajeria/conversaciones/{id}/manuales-sugeridos", { params: { path: { id: conv.id } }, signal }))).data ?? []);
    const [ver, setVer] = useState<Manual | null>(null);
    return (
        <div className="grid gap-3 px-4 py-3" data-slot="ayuda-manuales">
            <CampoBusqueda etiqueta="Buscar en los manuales" etiquetaVisible={false} tamano="sm" placeholder="Buscar en los manuales" valor={texto} alCambiar={setTexto} maxLength={100} />
            {q ? (
                <Busqueda q={q} conv={conv} identidad={identidad} alVer={setVer} alUsarTexto={alUsarTexto} />
            ) : (
                <section className="grid gap-2" aria-label="Sugeridos para esta conversación">
                    <h3 className="text-[0.6875rem] font-semibold tracking-wide text-muted-foreground uppercase">Sugeridos para esta conversación</h3>
                    {sugeridos.estado === "cargando" ? <Cargando texto="Buscando manuales…" /> : null}
                    {sugeridos.estado === "error" ? (
                        <Aviso
                            tono="error"
                            titulo="No se pudieron leer las sugerencias"
                            accion={
                                <Boton variante="secundario" tamano="sm" onClick={sugeridos.reintentar}>
                                    <RotateCw />
                                    Reintentar
                                </Boton>
                            }
                        />
                    ) : null}
                    {sugeridos.estado === "listo" && sugeridos.datos.length === 0 ? <p className="text-[0.8125rem] text-muted-foreground">Nada sugerido: busca arriba o pon una etiqueta a la conversación.</p> : null}
                    {sugeridos.estado === "listo"
                        ? sugeridos.datos.map((s: Sugerido) => <Articulo key={s.manual.id} manual={s.manual} motivo={s.motivo} conv={conv} identidad={identidad} alVer={setVer} alUsarTexto={alUsarTexto} />)
                        : null}
                </section>
            )}
            <Boton asChild variante="secundario" tamano="sm" className="w-full">
                <Link href="/administracion/manuales">
                    <BookOpen />
                    Abrir la biblioteca completa
                </Link>
            </Boton>
            <Dialogo open={ver !== null} onOpenChange={(a) => !a && setVer(null)}>
                {ver ? (
                    <DialogoContenido titulo={ver.titulo} descripcion={ver.resumen} className="sm:max-w-2xl">
                        <MarkdownSeguro fuente={ver.cuerpo} />
                    </DialogoContenido>
                ) : null}
            </Dialogo>
        </div>
    );
}

function Busqueda({ q, conv, identidad, alVer, alUsarTexto }: { q: string; conv: Conversacion; identidad: Identidad; alVer: (m: Manual) => void; alUsarTexto: (t: string) => void }) {
    const lectura = useLectura(async (signal) => (await resolver(api().GET("/api/v1/mensajeria/manuales", { params: { query: { q } }, signal }))).data ?? []);
    if (lectura.estado === "cargando") return <Cargando texto="Buscando…" />;
    if (lectura.estado === "error") {
        return (
            <Aviso
                tono="error"
                titulo="No se pudo buscar"
                accion={
                    <Boton variante="secundario" tamano="sm" onClick={lectura.reintentar}>
                        <RotateCw />
                        Reintentar
                    </Boton>
                }
            />
        );
    }
    if (lectura.datos.length === 0) return <p className="text-[0.8125rem] text-muted-foreground">Nada coincide con «{q}».</p>;
    return (
        <section className="grid gap-2" aria-label="Resultados de la búsqueda">
            {lectura.datos.map((m) => (
                <Articulo key={m.id} manual={m} conv={conv} identidad={identidad} alVer={alVer} alUsarTexto={alUsarTexto} />
            ))}
        </section>
    );
}

function Articulo({ manual, motivo, conv, identidad, alVer, alUsarTexto }: { manual: Manual; motivo?: string; conv: Conversacion; identidad: Identidad; alVer: (m: Manual) => void; alUsarTexto: (t: string) => void }) {
    const { puede } = useSesion();
    const [enviando, setEnviando] = useState(false);
    const [error, setError] = useState<string | null>(null);
    const m = motivo ? MOTIVO[motivo] : undefined;
    const IconoMotivo = m?.icono ?? Tag;
    const paraCliente = manual.visibilidad === "cliente";

    async function enviar() {
        setEnviando(true);
        setError(null);
        const id = nuevoIdMensaje();
        try {
            await verificarSesion(identidad);
            await resolver(
                api().POST("/api/v1/mensajeria/conversaciones/{id}/enviar-manual", {
                    params: { path: { id: conv.id }, header: cabeceraIdem(id) },
                    body: { id, manualId: manual.id, modo: "enlace" },
                }),
            );
            avisar({ titulo: `Enviaste «${manual.titulo}» a ${conv.titulo}` });
        } catch (e) {
            setError(
                esSesionCambiada(e)
                    ? "La sesión de este navegador cambió: no se envió."
                    : e instanceof ErrorApi && e.codigo === "fuera_de_ventana"
                      ? "La ventana de 24 h está cerrada: solo se pueden mandar plantillas."
                      : mensajeDeError(e).titulo,
            );
        } finally {
            setEnviando(false);
        }
    }

    return (
        <article className="flex min-w-0 items-start gap-2.5 rounded-[0.625rem] border border-border p-2.5" data-manual={manual.id}>
            <BookOpen aria-hidden className="mt-0.5 size-4 shrink-0 text-muted-foreground" />
            <div className="grid min-w-0 flex-1 gap-0.5">
                <h4 className="truncate text-[0.8125rem] font-semibold">{manual.titulo}</h4>
                <p className="line-clamp-2 text-xs text-muted-foreground">{manual.resumen}</p>
                {m ? (
                    <span className="flex min-w-0 items-center gap-1 text-[0.71875rem] text-muted-foreground">
                        <IconoMotivo aria-hidden className="size-3.5 shrink-0" />
                        <span className="truncate">{m.texto}</span>
                    </span>
                ) : null}
                <div className="mt-1.5 flex flex-wrap gap-1.5">
                    <Boton variante="secundario" tamano="sm" className="h-7 px-2 text-xs" onClick={() => alVer(manual)}>
                        <Eye />
                        Ver
                    </Boton>
                    {puede(["conversaciones.responder"]) ? (
                        paraCliente ? (
                            <Boton variante="secundario" tamano="sm" className="h-7 px-2 text-xs" onClick={() => void enviar()} disabled={enviando || !conv.ventana.abierta}>
                                <Send />
                                {enviando ? "Enviando…" : "Enviar al cliente"}
                            </Boton>
                        ) : (
                            <Boton variante="secundario" tamano="sm" className="h-7 px-2 text-xs" onClick={() => alUsarTexto(markdownATexto(manual.cuerpo))}>
                                <PencilLine />
                                Usar en la respuesta
                            </Boton>
                        )
                    ) : null}
                </div>
                {error ? (
                    <p role="alert" className="text-xs text-error">
                        {error}
                    </p>
                ) : null}
            </div>
        </article>
    );
}
