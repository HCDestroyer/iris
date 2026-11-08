"use client";

import { zodResolver } from "@hookform/resolvers/zod";
import type { Schema } from "@iris/contratos";
import { avisar, Aviso, Boton, Campo, CampoSelector, Casilla, cn, FormularioPagina, liberarProteccion, SeccionFormulario } from "@iris/ui";
import { Clock, Contact, History, MessageCircle, QrCode, RotateCw, type LucideIcon } from "lucide-react";
import Link from "next/link";
import { useParams, useRouter } from "next/navigation";
import { useEffect, useState } from "react";
import { useForm, useWatch } from "react-hook-form";
import { z } from "zod";
import { CargaRegistro } from "@/componentes/carga-registro";
import { TarjetaEstado } from "@/componentes/meta/tarjeta-estado";
import { useNombreEnMigas } from "@/componentes/migas-panel";
import { api, nuevaClaveIdempotencia, resolver } from "@/lib/api/cliente";
import { mensajeDeError } from "@/lib/api/errores";
import { formatearNumero, nombreUso } from "@/lib/conversaciones/presentacion";
import { erroresACampos } from "@/lib/formularios";
import { useLectura } from "@/lib/lectura";
import { leerCanalMeta, sincronizarCanal } from "@/lib/meta/api";
import { MINUTOS_REPETIR_PEDIDO, plazoRestante, presentarSincronizacion, sePuedePedir, textoDesconexion } from "@/lib/meta/presentacion";
import type { CanalMeta, Sincronizacion } from "@/lib/meta/tipos";
import { actualizarCanal, cuerpoActualizar } from "@/lib/numeros/api";
import { cuando } from "@/lib/numeros/conexion";
import { USOS } from "@/lib/numeros/esquemas";
import { idDeParametro } from "@/lib/parametros";
import { useSesion } from "@/lib/sesion";

const LISTA = "/administracion/numeros";
/** Mientras Meta manda el historial, se relee el número cada tanto. */
const RELEER_MS = 10_000;

const esquema = z.object({
    nombre: z.string().trim().min(1, "Escribe un nombre").max(120, "Hasta 120 caracteres"),
    colaId: z.string(),
    usos: z.array(z.enum(USOS)).min(1, "Elige al menos un uso"),
});
type Entrada = z.input<typeof esquema>;
type Valores = z.output<typeof esquema>;

/**
 * `/administracion/numeros/[id]/conectado`: un número conectado con QR (contrato §15.2). Dice si
 * sigue conectado, deja traer contactos e historial (una vez, dentro de 24 h desde que se conectó)
 * y terminar de configurar nombre, usos y cola.
 */
export function NumeroConectado() {
    const id = idDeParametro(useParams<{ id: string }>().id);
    const { puede } = useSesion();
    const verColas = puede(["conversaciones.ver"]);
    const lectura = useLectura(async (signal) => {
        if (id === null) return null;
        const [canal, colas] = await Promise.all([leerCanalMeta(id, signal), verColas ? resolver(api().GET("/api/v1/mensajeria/colas", { signal })).then((r) => r.data ?? []) : Promise.resolve(null)]);
        return { canal, colas: colas ? colas.filter((c) => c.activa) : null };
    });
    useNombreEnMigas(lectura.estado === "listo" ? lectura.datos?.canal.nombre : null);
    return (
        <CargaRegistro lectura={lectura} que="el número" noEncontrado="Número no encontrado">
            {(d) => <Pantalla inicial={d.canal} colas={d.colas} />}
        </CargaRegistro>
    );
}

function Pantalla({ inicial, colas }: { inicial: CanalMeta; colas: Schema<"ColaConversaciones">[] | null }) {
    const router = useRouter();
    const { puede } = useSesion();
    const configurar = puede(["canales.configurar"]);
    const [canal, setCanal] = useState(inicial);
    const [error, setError] = useState<unknown>(null);
    const form = useForm<Entrada, unknown, Valores>({
        resolver: zodResolver(esquema),
        defaultValues: { nombre: inicial.nombre, colaId: inicial.colaId ? String(inicial.colaId) : "", usos: (inicial.usos ?? []).filter((u): u is (typeof USOS)[number] => (USOS as readonly string[]).includes(u)) },
    });
    const usos = useWatch({ control: form.control, name: "usos" }) ?? [];
    const errores = form.formState.errors;

    async function guardar(d: Valores) {
        setError(null);
        try {
            let actual = canal;
            if (d.nombre !== actual.nombre) {
                // Un número QR solo cambia el nombre aquí (token, número y Phone number ID son de Meta).
                const r = await actualizarCanal(actual.id, cuerpoActualizar(actual, { nombre: d.nombre }), nuevaClaveIdempotencia());
                actual = { ...actual, nombre: r.canal.nombre, version: r.canal.version };
            }
            const colaId = d.colaId ? Number(d.colaId) : null;
            const cambioUso = colaId !== actual.colaId || d.usos.join() !== (actual.usos ?? []).join();
            if (configurar && cambioUso) {
                const r = await resolver(
                    api().POST("/api/v1/mensajeria/canales/{id}/configurar", {
                        params: { path: { id: actual.id }, header: { "Idempotency-Key": nuevaClaveIdempotencia() } },
                        body: { version: actual.version, usos: d.usos, avisosEmpresa: actual.avisosEmpresa && d.usos.includes("avisos"), colaId },
                    }),
                );
                actual = { ...actual, usos: r.usos, colaId: r.colaId, version: r.version };
            }
            setCanal(actual);
            avisar({ titulo: `Número «${actual.nombre}» guardado` });
            await liberarProteccion();
            router.replace(LISTA);
        } catch (e) {
            if (!erroresACampos(e, ["nombre", "colaId", "usos"], form.setError)) setError(e);
        }
    }

    const desconectado = canal.conexion === "desconectado";
    return (
        <FormularioPagina
            titulo={desconectado ? "Número desconectado" : "Número conectado"}
            descripcion={
                <>
                    <span className="font-mono">{formatearNumero(canal.numero)}</span> en WhatsApp Business
                </>
            }
            alEnviar={form.handleSubmit(guardar)}
            navegar={router.push}
            volverA={LISTA}
            sucio={form.formState.isDirty}
            barra={{ textoGuardar: "Guardar", textoCancelar: "Después", enviando: form.formState.isSubmitting }}
            error={
                error ? (
                    <Aviso tono="error" titulo="No se pudo guardar">
                        {mensajeDeError(error).titulo}
                    </Aviso>
                ) : null
            }
        >
            {desconectado ? (
                <TarjetaEstado
                    etiqueta="Conectar con QR"
                    titulo="Desconectado por Meta"
                    texto={`${textoDesconexion(canal.desconectadoMotivo)} No envía ni recibe hasta que lo vuelvas a conectar.`}
                    tono="error"
                    data-estado="desconectado"
                    accion={
                        <Boton asChild>
                            <Link href="/administracion/numeros/nuevo/qr">
                                <QrCode aria-hidden />
                                Reconectar
                            </Link>
                        </Boton>
                    }
                />
            ) : (
                <TarjetaEstado
                    etiqueta="Conectar con QR"
                    titulo="Conectado · QR · WhatsApp Business"
                    texto="La app sigue en el teléfono. Lo que mandes desde ahí también aparece en Iris, marcado «Desde el teléfono»."
                    tono="success"
                    data-estado="conectado"
                />
            )}
            {canal.sincronizacion ? <TraerHistorial canal={canal} alCambiar={setCanal} /> : null}
            <SeccionFormulario titulo="Terminar de configurar">
                <div className="grid gap-4 sm:grid-cols-2">
                    <Campo etiqueta="Nombre" maxLength={120} ayuda="Así se ve el número en la bandeja." error={errores.nombre?.message} {...form.register("nombre")} />
                    {colas && configurar ? (
                        <CampoSelector etiqueta="Cola de las conversaciones nuevas" error={errores.colaId?.message} {...form.register("colaId")}>
                            <option value="">Sin cola</option>
                            {colas.map((c) => (
                                <option key={c.id} value={String(c.id)}>
                                    {c.nombre}
                                </option>
                            ))}
                        </CampoSelector>
                    ) : null}
                </div>
                {configurar ? (
                    <fieldset className="grid gap-2">
                        <legend className="mb-1 text-sm font-medium">Usos</legend>
                        <div className="grid grid-cols-2 gap-2 sm:grid-cols-4">
                            {USOS.map((u) => (
                                <Casilla
                                    key={u}
                                    etiqueta={nombreUso(u)}
                                    checked={usos.includes(u)}
                                    onChange={(e) => form.setValue("usos", e.target.checked ? [...usos, u] : usos.filter((x) => x !== u), { shouldDirty: true, shouldValidate: form.formState.isSubmitted })}
                                />
                            ))}
                        </div>
                        {errores.usos?.message ? (
                            <p role="alert" className="text-sm text-error">
                                {errores.usos.message}
                            </p>
                        ) : null}
                    </fieldset>
                ) : null}
            </SeccionFormulario>
        </FormularioPagina>
    );
}

/** «Traer historial»: contactos y mensajes del teléfono, una vez y dentro de 24 h (§15.2). */
function TraerHistorial({ canal, alCambiar }: { canal: CanalMeta; alCambiar: (c: CanalMeta) => void }) {
    const s = canal.sincronizacion as Sincronizacion;
    const [ahora, setAhora] = useState(() => Date.now());
    const [pedir, setPedir] = useState({ contactos: sePuedePedir(s, "contactos"), mensajes: sePuedePedir(s, "mensajes") });
    const [enviando, setEnviando] = useState(false);
    const [error, setError] = useState<unknown>(null);
    const plazo = plazoRestante(s.plazo, ahora);
    const activo = [s.contactos.estado, s.mensajes.estado].some((e) => e === "pedido" || e === "recibiendo");

    // El plazo baja solo; y mientras Meta manda, se relee el número.
    useEffect(() => {
        const t = window.setInterval(() => setAhora(Date.now()), 30_000);
        return () => window.clearInterval(t);
    }, []);
    useEffect(() => {
        if (!activo) return undefined;
        const t = window.setInterval(() => {
            void leerCanalMeta(canal.id)
                .then(alCambiar)
                .catch(() => undefined);
        }, RELEER_MS);
        return () => window.clearInterval(t);
    }, [activo, canal.id, alCambiar]);

    async function traer() {
        setEnviando(true);
        setError(null);
        try {
            const c = await sincronizarCanal(canal.id, { contactos: pedir.contactos && sePuedePedir(s, "contactos"), mensajes: pedir.mensajes && sePuedePedir(s, "mensajes") }, nuevaClaveIdempotencia());
            alCambiar(c);
            avisar({ titulo: "Historial pedido: acepta el aviso de Meta en el teléfono" });
        } catch (e) {
            setError(e);
        } finally {
            setEnviando(false);
        }
    }

    // §15.2: un «pedido» sin respuesta de Meta en 10 minutos se puede volver a pedir.
    const sinRespuesta = (tipo: "contactos" | "mensajes") => s[tipo].estado === "pedido" && sePuedePedir(s, tipo, ahora);
    const puedeAlguno = sePuedePedir(s, "contactos", ahora) || sePuedePedir(s, "mensajes", ahora);
    const elegido = (pedir.contactos && sePuedePedir(s, "contactos", ahora)) || (pedir.mensajes && sePuedePedir(s, "mensajes", ahora));
    const pedidoEn = s.contactos.pedidoEn ?? s.mensajes.pedidoEn;
    return (
        <SeccionFormulario
            titulo={
                <span className="flex items-center gap-2">
                    <History aria-hidden className="size-5" />
                    Traer historial
                </span>
            }
            descripcion="Meta pide permiso en el teléfono. Solo chats 1:1; los grupos no pasan."
        >
            {pedidoEn ? <p className="text-sm text-muted-foreground">Pedido {cuando(pedidoEn)}.</p> : null}
            <Aviso tono={plazo.vencido ? "neutral" : "warning"} icono={Clock} titulo={plazo.vencido ? "El plazo para traer el historial ya venció" : `Plazo: ${cuando(s.plazo)}`}>
                {plazo.vencido
                    ? "Meta solo entrega el historial si se pide en las 24 horas después de conectar."
                    : `Meta solo entrega el historial si se pide en las 24 horas después de conectar. Si algo falla, puedes volver a pedirlo hasta entonces (quedan ${plazo.texto}).`}
            </Aviso>
            <ul className="divide-y divide-border overflow-hidden rounded-xl border border-border">
                <FilaSincronizacion
                    clave="contactos"
                    icono={Contact}
                    titulo="Contactos"
                    estado={s.contactos.estado}
                    marcado={pedir.contactos}
                    alMarcar={(m) => setPedir((p) => ({ ...p, contactos: m }))}
                    elegible={sePuedePedir(s, "contactos", ahora)}
                    texto={
                        s.contactos.estado === "recibiendo" || s.contactos.estado === "listo"
                            ? `${s.contactos.total} contactos. Los que coinciden con un cliente de Argos se asocian solos (${s.contactos.asociados}).`
                            : "Los del teléfono. Los que coinciden con un cliente de Argos se asocian solos; los datos de Argos no cambian."
                    }
                    pie={sinRespuesta("contactos") ? SIN_RESPUESTA : null}
                    detalle={s.contactos.detalle}
                />
                <FilaSincronizacion
                    clave="mensajes"
                    icono={MessageCircle}
                    titulo="Mensajes"
                    estado={s.mensajes.estado}
                    marcado={pedir.mensajes}
                    alMarcar={(m) => setPedir((p) => ({ ...p, mensajes: m }))}
                    elegible={sePuedePedir(s, "mensajes", ahora)}
                    texto="De los últimos 180 días (lo más que da Meta)."
                    progreso={s.mensajes.estado === "recibiendo" || s.mensajes.estado === "listo" ? s.mensajes.progreso : null}
                    pie={s.mensajes.estado === "recibiendo" ? `${s.mensajes.mensajes.toLocaleString("es-GT")} mensajes hasta ahora. Puedes salir: sigue solo.` : sinRespuesta("mensajes") ? SIN_RESPUESTA : null}
                    detalle={s.mensajes.detalle}
                />
            </ul>
            {error ? (
                <Aviso
                    tono="error"
                    titulo="No se pudo pedir el historial"
                    accion={
                        <Boton variante="contorno" tamano="sm" onClick={() => void traer()}>
                            <RotateCw aria-hidden />
                            Reintentar
                        </Boton>
                    }
                >
                    {mensajeDeError(error).titulo}
                </Aviso>
            ) : null}
            {puedeAlguno ? (
                <Boton variante="contorno" className="justify-self-start" onClick={() => void traer()} disabled={!elegido || enviando}>
                    <History aria-hidden />
                    {enviando ? "Pidiendo…" : "Traer historial"}
                </Boton>
            ) : null}
            <p className="text-sm text-muted-foreground">Lo traído entra cerrado a la bandeja y no manda avisos ni encuestas.</p>
        </SeccionFormulario>
    );
}

const SIN_RESPUESTA = `Meta no respondió en ${MINUTOS_REPETIR_PEDIDO} minutos. Puedes pedirlo otra vez.`;

const COLOR: Record<string, string> = { success: "text-success", info: "text-info", warning: "text-warning", error: "text-error", neutral: "text-muted-foreground" };

function FilaSincronizacion({
    clave,
    icono: Icono,
    titulo,
    texto,
    estado,
    elegible,
    marcado,
    alMarcar,
    progreso = null,
    pie = null,
    detalle,
}: {
    clave: string;
    icono: LucideIcon;
    titulo: string;
    texto: string;
    estado: Sincronizacion["contactos"]["estado"];
    elegible: boolean;
    marcado: boolean;
    alMarcar: (m: boolean) => void;
    progreso?: number | null;
    pie?: string | null;
    detalle: string | null;
}) {
    const p = presentarSincronizacion(estado);
    return (
        <li className="grid gap-2 p-3 sm:p-4" data-sincronizacion={clave} data-estado={estado}>
            <div className="flex flex-wrap items-center justify-between gap-2">
                <span className="flex min-w-0 items-center gap-2 font-medium">
                    {elegible ? <Casilla etiqueta={<span className="sr-only">Traer {titulo.toLowerCase()}</span>} checked={marcado} onChange={(e) => alMarcar(e.target.checked)} /> : null}
                    <Icono aria-hidden className="size-4 shrink-0 text-muted-foreground" />
                    {titulo}
                </span>
                <span className={cn("text-sm font-medium", COLOR[p.tono])}>
                    {p.texto}
                    {estado === "recibiendo" && progreso !== null ? ` · ${progreso} %` : ""}
                </span>
            </div>
            <p className="text-sm text-muted-foreground">{texto}</p>
            {progreso !== null ? (
                <div role="progressbar" aria-label={`Progreso de ${titulo.toLowerCase()}`} aria-valuemin={0} aria-valuemax={100} aria-valuenow={progreso} className="h-1.5 overflow-hidden rounded-full bg-background-secondary">
                    <div className="h-full rounded-full bg-primary" style={{ width: `${Math.max(0, Math.min(100, progreso))}%` }} />
                </div>
            ) : null}
            {pie ? <p className="text-sm text-muted-foreground">{pie}</p> : null}
            {detalle ? <p className="text-sm text-error">{detalle}</p> : null}
        </li>
    );
}
