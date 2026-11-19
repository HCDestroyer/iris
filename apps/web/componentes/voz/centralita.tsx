"use client";

import { avisar, Aviso, Boton, CampoArea, Dialogo, DialogoContenido, DialogoPie, GrupoOpciones } from "@iris/ui";
import { Phone, Search, Ticket, X } from "lucide-react";
import { useRouter } from "next/navigation";
import { createContext, use, useCallback, useEffect, useRef, useState, useSyncExternalStore, type ReactNode } from "react";
import { SelectorCliente, type SeleccionCliente } from "@/componentes/clientes/selector-cliente";
import { mensajeDeError } from "@/lib/api/errores";
import { anotarLlamadaCentralita, asociarClienteCentralita, leerMiExtension, vincularTicketCentralita } from "@/lib/centralita/api";
import { quienCentralita, RESULTADOS_CENTRALITA } from "@/lib/centralita/presentacion";
import { abrirFlujo, leerCambios, leerEventoFlujo } from "@/lib/conversaciones/api";
import { esSesionCambiada, verificarSesion } from "@/lib/conversaciones/sesion";
import { iniciarFeed } from "@/lib/conversaciones/tiempo-real";
import type { EventoBandeja } from "@/lib/conversaciones/tipos";
import { useClaveIdempotencia } from "@/lib/idempotencia";
import { useSesion } from "@/lib/sesion";
import { anunciarFuente, cuantasFuentes, escucharEventos, escucharFuente, publicarEvento } from "@/lib/voz/canal";
import { reloj } from "@/lib/voz/presentacion";
import type { Llamada, ResultadoLlamada } from "@/lib/voz/tipos";
import { CabeceraLlamada, MARCO_AVISO, Senales } from "./llamada";

/*
 * Centralita Issabel, E4a (contratos/voz.md §3.12): cuando suena la extensión de esta persona, el
 * aviso de llamada (el mismo de WhatsApp: `CabeceraLlamada`, `Senales`, `MARCO_AVISO`) en modo «suena
 * en tu extensión», sin «Contestar»: se contesta en el teléfono. E4c le sumará el audio.
 *
 * Llega por el feed de la bandeja (eventos `llamada_*` con `llamada.origen = centralita`, dirigidos a
 * quien le sonó). Un solo flujo por pestaña: si la bandeja (u otra pantalla) ya lo tiene abierto, se
 * escuchan sus eventos; si no, este proveedor abre el suyo y lo anuncia.
 */

type Contexto = { extension: string | null };
const ContextoCentralita = createContext<Contexto>({ extension: null });

/** Tu extensión de la centralita (null si no tienes). */
export function useMiExtension(): string | null {
    return use(ContextoCentralita).extension;
}

/** Cada cuánto se vuelve a mirar si la persona tiene extensión (se la pueden asignar o quitar). */
const REVISAR_EXTENSION_MS = 120_000;

export function ProveedorCentralita({ children }: { children: ReactNode }) {
    const { yo } = useSesion();
    const yoId = yo.usuario.id;
    const empresaId = yo.empresaActiva?.id;
    const [extension, setExtension] = useState<string | null>(null);

    useEffect(() => {
        const control = new AbortController();
        const leer = () =>
            leerMiExtension(control.signal).then(
                (r) => !control.signal.aborted && setExtension(r.extension ?? null),
                () => undefined,
            );
        void leer();
        const t = window.setInterval(() => void leer(), REVISAR_EXTENSION_MS);
        return () => {
            control.abort();
            window.clearInterval(t);
        };
    }, [empresaId]);

    // Un solo flujo por pestaña: el propio solo si nadie más lo tiene abierto.
    const propio = useRef(false);
    const otras = useSyncExternalStore(escucharFuente, () => cuantasFuentes() - (propio.current ? 1 : 0), () => 0);
    const abrir = extension !== null && otras === 0;
    useEffect(() => {
        if (!abrir) return;
        const fuente = anunciarFuente();
        propio.current = true;
        const cerrar = iniciarFeed<EventoBandeja>({
            abrirFlujo,
            leerCambios,
            verificarSesion: () => verificarSesion({ usuarioId: yoId, empresaId }),
            esSesionAjena: esSesionCambiada,
            leerEvento: leerEventoFlujo,
            alEvento: publicarEvento,
            alEstado: fuente.estado,
        });
        return () => {
            cerrar();
            propio.current = false;
            fuente.retirar();
        };
    }, [abrir, yoId, empresaId]);

    return (
        <ContextoCentralita value={{ extension }}>
            {children}
            {extension !== null ? <AvisoCentralita yoId={yoId} /> : null}
        </ContextoCentralita>
    );
}

type Vista = { llamada: Llamada; cerrada: boolean; notaPendiente: boolean };

/** Lo que muestra el aviso, a partir de los eventos de la centralita. */
function useLlamadaCentralita(yoId: number) {
    const [vista, setVista] = useState<Vista | null>(null);
    useEffect(
        () =>
            escucharEventos((evento) => {
                const l = evento.llamada;
                if (!l || l.origen !== "centralita") return;
                setVista((antes) => {
                    if (evento.tipo === "llamada_entrante") return { llamada: l, cerrada: false, notaPendiente: false };
                    if (!antes || antes.llamada.id !== l.id) return antes;
                    if (evento.tipo === "llamada_terminada") {
                        // Si la contestaste tú, queda la nota (opcional); si no, el aviso se va.
                        const mia = l.usuario?.id === yoId && l.estado === "terminada";
                        return { llamada: l, cerrada: !mia, notaPendiente: mia };
                    }
                    return { ...antes, llamada: l };
                });
            }),
        [yoId],
    );
    return [vista, setVista] as const;
}

function useSegundos(desde: string | null | undefined): number {
    const [ahora, setAhora] = useState(() => Date.now());
    useEffect(() => {
        if (!desde) return;
        const t = window.setInterval(() => setAhora(Date.now()), 1000);
        return () => window.clearInterval(t);
    }, [desde]);
    return desde ? Math.max(0, (ahora - Date.parse(desde)) / 1000) : 0;
}

function AvisoCentralita({ yoId }: { yoId: number }) {
    const [vista, setVista] = useLlamadaCentralita(yoId);
    const cerrar = useCallback(() => setVista(null), [setVista]);
    const llamada = vista?.llamada;
    const otro = llamada?.estado === "en_curso" && llamada.usuario !== undefined && llamada.usuario.id !== yoId;
    // «Contestó …» (otra persona) o cerrada: se va sola.
    useEffect(() => {
        if (!vista || (!otro && !vista.cerrada)) return;
        const t = window.setTimeout(cerrar, vista.cerrada ? 0 : 8000);
        return () => window.clearTimeout(t);
    }, [vista, otro, cerrar]);
    const segundos = useSegundos(llamada?.estado === "en_curso" ? llamada.contestadaEn : null);
    if (!vista || !llamada) return null;
    if (vista.notaPendiente) return <NotaCentralita llamada={llamada} alTerminar={cerrar} />;
    if (vista.cerrada) return null;
    const quien = quienCentralita(llamada);
    const ext = llamada.centralita?.tuExtension;
    const sonando = llamada.estado === "sonando";
    const linea = otro
        ? `Contestó ${llamada.usuario?.nombre ?? "otra persona"}${llamada.centralita?.extension ? ` (ext. ${llamada.centralita.extension})` : ""}`
        : sonando
          ? `Suena tu extensión ${ext ?? ""} · ${llamada.numero || "sin número"}`
          : `En llamada · ${reloj(segundos)}${ext ? ` · ext. ${ext}` : ""}`;
    return (
        <section
            role={sonando ? "alertdialog" : "status"}
            aria-label={`Llamada de la centralita: ${quien.nombre}`}
            data-slot="aviso-centralita"
            data-estado={otro ? "otro" : llamada.estado}
            className={MARCO_AVISO + " border-primary/50 bg-card ring-2 ring-primary/25"}
        >
            <CabeceraLlamada
                conocido={quien.conocido}
                nombre={quien.nombre}
                linea={linea}
                sonando={sonando}
                accion={
                    <Boton variante="fantasma" tamano="icono-sm" aria-label="Cerrar" onClick={cerrar}>
                        <X />
                    </Boton>
                }
            />
            {otro ? <p className="text-xs text-muted-foreground">Esta ventana se cierra sola.</p> : <Acciones llamada={llamada} alCambiar={(l) => setVista({ llamada: l, cerrada: false, notaPendiente: false })} />}
        </section>
    );
}

/** Lo que se puede hacer con la llamada mientras suena o se habla: cliente, ticket. */
function Acciones({ llamada, alCambiar }: { llamada: Llamada; alCambiar: (l: Llamada) => void }) {
    const router = useRouter();
    const [buscando, setBuscando] = useState(false);
    const [elegido, setElegido] = useState<number | undefined>(undefined);
    const [ocupado, setOcupado] = useState(false);
    const c = llamada.centralita;
    const puede = llamada.puede.anotar;
    const ticket = llamada.senales?.ticketAbierto;
    const crearTicket = () => {
        const q = new URLSearchParams({ llamada: llamada.id });
        if (llamada.contacto.clienteId) q.set("clienteId", String(llamada.contacto.clienteId));
        if (llamada.contacto.servicioId) q.set("servicioId", String(llamada.contacto.servicioId));
        if (llamada.numero) q.set("telefono", llamada.numero);
        router.push(`/tickets/nuevo?${q.toString()}`);
    };
    const hacer = async (f: () => Promise<Llamada>, ok: string) => {
        setOcupado(true);
        try {
            alCambiar(await f());
            avisar({ titulo: ok });
        } catch (e) {
            avisar({ tono: "error", titulo: mensajeDeError(e).titulo });
        } finally {
            setOcupado(false);
        }
    };
    const varios = llamada.contacto.tipo !== "cliente" && (c?.candidatos?.length ?? 0) > 1;
    return (
        <>
            {llamada.contacto.tipo === "cliente" ? <Senales llamada={llamada} saldoVencido={false} /> : null}
            {varios && c ? (
                <div className="grid gap-2" data-slot="candidatos">
                    <GrupoOpciones
                        leyenda="¿Quién llama?"
                        nombre={`candidato-${llamada.id}`}
                        apiladas
                        opciones={(c.candidatos ?? []).map((x) => ({ valor: x.clienteId, etiqueta: x.nombre, detalle: `Código ${x.codigo}` }))}
                        valor={elegido}
                        alCambiar={setElegido}
                        disabled={!c.asociar}
                    />
                    <div className="flex flex-wrap gap-2">
                        <Boton disabled={elegido === undefined || ocupado || !c.asociar} onClick={() => elegido !== undefined && void hacer(() => asociarClienteCentralita(llamada.id, elegido), "La llamada quedó en el cliente")}>
                            Es este
                        </Boton>
                    </div>
                </div>
            ) : null}
            {llamada.contacto.tipo !== "cliente" && !varios ? (
                <p className="text-sm text-muted-foreground">{llamada.numero ? "No está en Argos ni en tus contactos." : "La centralita no recibió el número."}</p>
            ) : null}
            {buscando ? (
                <SelectorCliente
                    valor={null}
                    etiqueta="Buscar cliente"
                    alCambiar={(s: SeleccionCliente | null) => {
                        if (!s) return;
                        setBuscando(false);
                        void hacer(() => asociarClienteCentralita(llamada.id, s.cliente.id, s.servicio?.id), "La llamada quedó en el cliente");
                    }}
                />
            ) : null}
            <p className="flex items-start gap-1.5 text-xs text-muted-foreground">
                <Phone aria-hidden className="mt-0.5 size-3.5 shrink-0" />
                Contesta en tu teléfono. La llamada queda guardada en el cliente.
            </p>
            {puede ? (
                <div className="flex flex-wrap gap-2">
                    {ticket && !llamada.ticketId ? (
                        <Boton disabled={ocupado} onClick={() => void hacer(() => vincularTicketCentralita(llamada.id, ticket.id), `Llamada agregada al ticket ${ticket.numero}`)}>
                            <Ticket />
                            Agregar a {ticket.numero}
                        </Boton>
                    ) : null}
                    {llamada.contacto.tipo !== "cliente" && c?.asociar && !buscando ? (
                        <Boton variante="secundario" onClick={() => setBuscando(true)}>
                            <Search />
                            Buscar cliente
                        </Boton>
                    ) : null}
                    {!llamada.ticketId ? (
                        <Boton variante={ticket ? "secundario" : "primario"} onClick={crearTicket}>
                            Crear ticket
                        </Boton>
                    ) : null}
                </div>
            ) : null}
        </>
    );
}

/** Al colgar (si la contestaste tú): «¿Cómo quedó?» y una nota opcional. Diálogo corto (2 campos). */
function NotaCentralita({ llamada, alTerminar }: { llamada: Llamada; alTerminar: () => void }) {
    const [resultado, setResultado] = useState<ResultadoLlamada | undefined>(undefined);
    const [nota, setNota] = useState("");
    const [error, setError] = useState<unknown>(null);
    const [enviando, setEnviando] = useState(false);
    const clave = useClaveIdempotencia();
    const quien = quienCentralita(llamada);
    if (!llamada.puede.anotar) {
        alTerminar();
        return null;
    }
    return (
        <Dialogo open onOpenChange={(a) => !a && alTerminar()}>
            <DialogoContenido titulo={`Llamada con ${quien.nombre}`} descripcion="¿Cómo quedó? La nota es opcional y se guarda en la llamada.">
                <div className="grid gap-3" data-slot="nota-centralita">
                    <GrupoOpciones
                        leyenda="¿Cómo quedó?"
                        nombre={`resultado-${llamada.id}`}
                        opciones={RESULTADOS_CENTRALITA.map((r) => ({ valor: r.valor, etiqueta: r.texto }))}
                        valor={resultado}
                        alCambiar={setResultado}
                    />
                    <CampoArea etiqueta="Nota (opcional)" value={nota} maxLength={2000} rows={3} onChange={(e) => setNota(e.target.value)} />
                    {error ? (
                        <Aviso tono="error" titulo="No se pudo guardar">
                            {mensajeDeError(error).titulo}
                        </Aviso>
                    ) : null}
                </div>
                <DialogoPie>
                    <Boton variante="fantasma" onClick={alTerminar}>
                        Ahora no
                    </Boton>
                    <Boton
                        disabled={enviando || (!resultado && nota.trim() === "")}
                        onClick={async () => {
                            setEnviando(true);
                            setError(null);
                            try {
                                await anotarLlamadaCentralita(llamada.id, { version: llamada.version ?? 1, nota: nota.trim() || undefined, resultado }, clave.actual());
                                clave.renovar();
                                avisar({ titulo: "Nota guardada" });
                                alTerminar();
                            } catch (e) {
                                clave.trasError(e);
                                setError(e);
                            } finally {
                                setEnviando(false);
                            }
                        }}
                    >
                        Guardar
                    </Boton>
                </DialogoPie>
            </DialogoContenido>
        </Dialogo>
    );
}
