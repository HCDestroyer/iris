"use client";

import { zodResolver } from "@hookform/resolvers/zod";
import {
    Aviso,
    AvatarUsuario,
    Boton,
    Campo,
    CampoBusqueda,
    CampoSelector,
    cn,
    Esqueleto,
    FormularioPagina,
    liberarProteccion,
    ListaEleccion,
    SeccionFormulario,
    avisar,
} from "@iris/ui";
import { Ban, CircleCheck, Info, MessageSquareText, RotateCw } from "lucide-react";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { useEffect, useMemo, useState } from "react";
import { useForm, useWatch } from "react-hook-form";
import { ErrorApi, mensajeDeError } from "@/lib/api/errores";
import { buscarDestinatarios, iniciarConversacion, leerNumeros, leerColas, leerPlantillasNumero } from "@/lib/conversaciones/api-equipo";
import { textoMotivoDestinatario } from "@/lib/conversaciones/atencion";
import type { Destinatario, Numero, PlantillaDisponible } from "@/lib/conversaciones/tipos";
import { useIdEnvio } from "@/lib/conversaciones/id-envio";
import { esquemaNuevaConversacion, primerNombre, trozosVistaPrevia, valoresIniciales, variablesFaltantes, type ValoresNueva } from "@/lib/conversaciones/nueva";
import { CLASE_PILDORA, CLASE_PUNTO, formatearNumero, nombreUso, tonoDeCanal, usoPrincipal } from "@/lib/conversaciones/presentacion";
import { useDiferido } from "@/lib/diferido";
import { erroresACampos } from "@/lib/formularios";
import { useLectura } from "@/lib/lectura";
import { ErrorLectura } from "@/componentes/error-lectura";

/** Clave de un destinatario en el formulario («cliente:4101», «contacto_iris:12»). */
const claveDe = (d: Pick<Destinatario, "tipo" | "id">) => `${d.tipo}:${d.id}`;

const CATEGORIAS: Record<string, string> = { servicio: "Servicio", marketing: "Marketing", autenticacion: "Autenticación" };

type Busqueda = { clave: string; estado: "cargando" } | { clave: string; estado: "error"; error: unknown } | { clave: string; estado: "listo"; datos: Destinatario[] };

/**
 * `/conversaciones/nueva` (fase 1.1, contrato §14.3): escribirle primero a un cliente de Argos o a
 * un contacto de Iris que aceptó mensajes, con una plantilla aprobada por Meta. El número de salida
 * es uno de mis colas; el número del cliente lo pone Iris. «Enviar y abrir conversación» manda la
 * plantilla con un id generado aquí: tocar dos veces o reintentar tras un corte no la duplica.
 */
export function NuevaConversacion() {
    const canales = useLectura(async (signal) => {
        const [todos, colas] = await Promise.all([leerNumeros(signal), leerColas(signal)]);
        return { canales: todos.filter((c) => c.activo && c.iniciar === true), colas };
    });
    if (canales.estado === "cargando") {
        return (
            <div className="mx-auto grid w-full max-w-6xl gap-4" aria-busy="true">
                <span className="sr-only" role="status">
                    Cargando…
                </span>
                <Esqueleto className="h-8 w-64" />
                <Esqueleto className="h-48" />
            </div>
        );
    }
    if (canales.estado === "error") {
        return (
            <div className="mx-auto w-full max-w-3xl">
                <ErrorLectura error={canales.error} reintentar={canales.reintentar} que="tus números de WhatsApp" />
            </div>
        );
    }
    if (canales.datos.canales.length === 0) {
        return (
            <div className="mx-auto grid w-full max-w-3xl gap-4">
                <h1 className="text-xl font-semibold sm:text-2xl">Nueva conversación</h1>
                <Aviso tono="warning" titulo="No tienes números de salida">
                    Para escribirle primero a un cliente necesitas ser miembro o supervisor de una cola con un número de WhatsApp activo.
                </Aviso>
            </div>
        );
    }
    return <Formulario canales={canales.datos.canales} colas={canales.datos.colas} />;
}

function Formulario({ canales, colas }: { canales: Numero[]; colas: { id: number; nombre: string }[] }) {
    const router = useRouter();
    const envio = useIdEnvio();
    const [error, setError] = useState<unknown>(null);
    const form = useForm<ValoresNueva>({
        resolver: zodResolver(esquemaNuevaConversacion),
        defaultValues: { contacto: "", canalId: String(canales[0]?.id ?? ""), plantilla: "", variables: {} },
    });
    const v = useWatch({ control: form.control });
    const canalId = Number(v.canalId) || null;
    const canal = canales.find((c) => c.id === canalId);

    // Búsqueda de destinatarios (clientes de Argos y contactos de Iris juntos), con el número de salida.
    const [texto, setTexto] = useState("");
    const q = useDiferido(texto.trim(), 300);
    const claveBusqueda = `${canalId ?? 0}|${q}`;
    const [busqueda, setBusqueda] = useState<Busqueda>({ clave: "", estado: "cargando" });
    const [intento, setIntento] = useState(0);
    useEffect(() => {
        if (q.length < 2) return;
        const control = new AbortController();
        const clave = `${claveBusqueda}`;
        buscarDestinatarios(q, canalId, control.signal).then(
            (datos) => !control.signal.aborted && setBusqueda({ clave, estado: "listo", datos }),
            (e: unknown) => !control.signal.aborted && setBusqueda({ clave, estado: "error", error: e }),
        );
        return () => control.abort();
    }, [q, canalId, claveBusqueda, intento]);
    const resultados: Busqueda = busqueda.clave === claveBusqueda ? busqueda : { clave: claveBusqueda, estado: "cargando" };

    // El elegido se guarda entero: sigue a la vista aunque cambie la búsqueda.
    const [elegido, setElegido] = useState<Destinatario | null>(null);
    // Con otro número de salida, el permiso y la conversación existente se vuelven a evaluar.
    const elegidoAlDia = resultados.estado === "listo" ? (resultados.datos.find((d) => elegido && claveDe(d) === claveDe(elegido)) ?? elegido) : elegido;

    // Plantillas aprobadas del número.
    const plantillas = useLectura<PlantillaDisponible[]>((signal) => (canalId ? leerPlantillasNumero(canalId, signal) : Promise.resolve([])));
    const refrescarPlantillas = plantillas.reintentar;
    useEffect(() => refrescarPlantillas(), [canalId, refrescarPlantillas]);
    const lista = plantillas.estado === "listo" ? plantillas.datos : [];
    const plantilla = lista.find((p) => p.codigo === v.plantilla);
    const valores = (v.variables ?? {}) as Record<string, string>;

    function elegirContacto(clave: string) {
        const d = resultados.estado === "listo" ? resultados.datos.find((x) => claveDe(x) === clave) : undefined;
        if (!d) return;
        setElegido(d);
        form.setValue("contacto", clave, { shouldDirty: true, shouldValidate: form.formState.isSubmitted });
        // La variable de nombre se llena con el primer nombre si estaba vacía.
        if (plantilla) {
            const iniciales = valoresIniciales(plantilla, d.nombre);
            for (const [nombre, valor] of Object.entries(iniciales)) {
                if (valor && !(valores[nombre] ?? "").trim()) form.setValue(`variables.${nombre}`, valor, { shouldDirty: true });
            }
        }
    }

    function elegirPlantilla(codigo: string) {
        const p = lista.find((x) => x.codigo === codigo);
        form.setValue("plantilla", codigo, { shouldDirty: true, shouldValidate: form.formState.isSubmitted });
        form.setValue("variables", valoresIniciales(p, elegidoAlDia?.nombre), { shouldDirty: true });
    }

    const faltanVariables = variablesFaltantes(plantilla, valores);
    const faltan = [
        ...(!elegidoAlDia ? ["a quién escribirle"] : !elegidoAlDia.puede ? ["un contacto que acepte mensajes"] : []),
        ...(!plantilla ? ["la plantilla"] : []),
        ...(faltanVariables.length ? [`las variables ${faltanVariables.map((n) => `{{${n}}}`).join(", ")}`] : []),
    ];

    async function enviar(d: ValoresNueva) {
        if (!elegidoAlDia || !elegidoAlDia.puede || !plantilla || !canalId) return;
        setError(null);
        const id = envio.actual();
        try {
            const r = await iniciarConversacion({
                id,
                canalId,
                ...(elegidoAlDia.tipo === "cliente" ? { clienteId: elegidoAlDia.id } : { contactoIrisId: elegidoAlDia.id }),
                plantilla: { codigo: plantilla.codigo, variables: Object.fromEntries(plantilla.variables.map((x) => [x.nombre, (d.variables[x.nombre] ?? "").trim()])) },
            });
            envio.renovar();
            avisar({ titulo: r.nueva ? `Conversación con ${elegidoAlDia.nombre} abierta` : `Plantilla enviada en la conversación con ${elegidoAlDia.nombre}` });
            await liberarProteccion();
            router.replace(`/conversaciones?c=${r.conversacion.id}`);
        } catch (e) {
            // Sin respuesta o 5xx: el mismo id (el servidor no duplica). Un 4xx: id nuevo.
            envio.trasError(e);
            if (!erroresACampos(e, ["canalId", "plantilla"], form.setError)) setError(e);
        }
    }

    const nombreCola = colas.find((c) => c.id === canal?.colaId)?.nombre;
    const colaDelCanal = nombreCola ? `la cola ${nombreCola}` : "la bandeja";
    const nombreVista = elegidoAlDia ? primerNombre(elegidoAlDia.nombre) || elegidoAlDia.nombre : "el cliente";
    return (
        <FormularioPagina
            titulo="Nueva conversación"
            descripcion="Escríbele primero a un cliente o contacto que aceptó mensajes. Se envía con una plantilla aprobada por Meta."
            alEnviar={form.handleSubmit(enviar)}
            navegar={router.push}
            volverA="/conversaciones"
            sucio={form.formState.isDirty || texto.trim() !== ""}
            className="max-w-6xl"
            barra={{ textoGuardar: "Enviar y abrir conversación", textoEnviando: "Enviando…", enviando: form.formState.isSubmitting, faltan }}
            error={error ? <ErrorEnvio error={error} /> : null}
        >
            <div className="grid min-w-0 items-start gap-4 lg:grid-cols-[minmax(0,1fr)_minmax(17rem,22rem)]">
                <div className="grid min-w-0 gap-4">
                    <SeccionFormulario titulo="Para">
                        <CampoBusqueda etiqueta="Contacto" valor={texto} alCambiar={setTexto} placeholder="Nombre, código o número (clientes de Argos y contactos de Iris)" maxLength={80} />
                        {q.length < 2 ? (
                            elegidoAlDia ? null : <p className="text-sm text-muted-foreground">Escribe al menos 2 letras para buscar.</p>
                        ) : resultados.estado === "cargando" ? (
                            <p className="text-sm text-muted-foreground" role="status">
                                Buscando…
                            </p>
                        ) : resultados.estado === "error" ? (
                            <ErrorLectura error={resultados.error} reintentar={() => setIntento((n) => n + 1)} que="los contactos" />
                        ) : resultados.datos.length === 0 ? (
                            <p className="text-sm text-muted-foreground">Nadie coincide con «{q}».</p>
                        ) : (
                            <ListaEleccion
                                leyenda="Resultados"
                                leyendaOculta
                                nombre="contacto"
                                pegadas
                                valor={v.contacto || undefined}
                                alCambiar={elegirContacto}
                                error={form.formState.errors.contacto?.message}
                                opciones={resultados.datos.map((d) => ({
                                    valor: claveDe(d),
                                    texto: `${d.nombre}, ${d.puede ? "acepta mensajes" : textoMotivoDestinatario(d.motivo)}`,
                                    deshabilitada: !d.puede,
                                    contenido: <FilaDestinatario d={d} />,
                                }))}
                            />
                        )}
                        {elegidoAlDia && (q.length < 2 || (resultados.estado === "listo" && !resultados.datos.some((d) => claveDe(d) === claveDe(elegidoAlDia)))) ? (
                            <div className="flex min-w-0 items-center gap-3 rounded-lg border border-primary bg-primary/5 px-3 py-2.5 text-sm" data-slot="elegido">
                                <FilaDestinatario d={elegidoAlDia} />
                            </div>
                        ) : null}
                        {elegidoAlDia?.conversacionId && elegidoAlDia.puede ? (
                            <Aviso
                                tono="info"
                                titulo={`Ya hay una conversación con ${elegidoAlDia.nombre} en este número`}
                                accion={
                                    <Boton asChild variante="secundario" tamano="sm">
                                        <Link href={`/conversaciones?c=${elegidoAlDia.conversacionId}`}>
                                            <MessageSquareText />
                                            Abrir conversación
                                        </Link>
                                    </Boton>
                                }
                            >
                                Si envías la plantilla, va en esa misma conversación.
                            </Aviso>
                        ) : null}
                    </SeccionFormulario>
                    <SeccionFormulario titulo="Mensaje">
                        <div className="grid gap-4 sm:grid-cols-2">
                            <CampoSelector
                                etiqueta="Número de WhatsApp de salida"
                                ayuda="Solo los números de tus colas."
                                error={form.formState.errors.canalId?.message}
                                {...form.register("canalId", { onChange: () => form.setValue("plantilla", "") })}
                            >
                                {canales.map((c) => (
                                    <option key={c.id} value={String(c.id)}>
                                        {`${c.nombre} · ${formatearNumero(c.numero)}`}
                                    </option>
                                ))}
                            </CampoSelector>
                            <div className="grid content-start gap-1.5">
                                {plantillas.estado === "error" ? (
                                    <ErrorLectura error={plantillas.error} reintentar={plantillas.reintentar} que="las plantillas" />
                                ) : (
                                    <CampoSelector
                                        etiqueta="Plantilla aprobada"
                                        value={v.plantilla ?? ""}
                                        onChange={(e) => elegirPlantilla(e.target.value)}
                                        error={form.formState.errors.plantilla?.message}
                                        placeholder={plantillas.estado === "cargando" ? "Cargando…" : lista.length === 0 ? "Este número no tiene plantillas aprobadas" : "Elegir…"}
                                        disabled={plantillas.estado !== "listo" || lista.length === 0}
                                    >
                                        <option value="">Elegir…</option>
                                        {lista.map((p) => (
                                            <option key={p.codigo} value={p.codigo}>
                                                {p.nombre}
                                            </option>
                                        ))}
                                    </CampoSelector>
                                )}
                                {plantilla ? (
                                    <p className="flex items-center gap-1.5 text-sm">
                                        <CircleCheck aria-hidden className="size-4 text-success" />
                                        <span className="font-medium text-success">Aprobada</span>
                                        {plantilla.categoria ? <span className="text-muted-foreground">· {CATEGORIAS[plantilla.categoria] ?? plantilla.categoria}</span> : null}
                                    </p>
                                ) : null}
                            </div>
                        </div>
                        {plantilla && plantilla.variables.length > 0 ? (
                            <fieldset className="grid gap-3">
                                <legend className="mb-1 text-sm font-medium">Variables de la plantilla</legend>
                                {plantilla.variables.map((x) => (
                                    <Campo
                                        key={`${plantilla.codigo}-${x.nombre}`}
                                        etiqueta={
                                            <span className="flex items-center gap-2">
                                                <code className="rounded bg-elevated px-1.5 py-0.5 font-mono text-xs">{`{{${x.nombre}}}`}</code>
                                                {x.obligatoria ? null : <span className="text-xs font-normal text-muted-foreground">(opcional)</span>}
                                            </span>
                                        }
                                        maxLength={1024}
                                        error={faltanVariables.includes(x.nombre) && form.formState.isSubmitted ? "Escribe el valor" : undefined}
                                        {...form.register(`variables.${x.nombre}`)}
                                    />
                                ))}
                            </fieldset>
                        ) : null}
                    </SeccionFormulario>
                </div>
                <VistaPrevia plantilla={plantilla} valores={valores} nombre={nombreVista} canal={canal} cola={colaDelCanal} />
            </div>
            <p className="flex items-center gap-1.5 text-xs text-muted-foreground">
                <Info aria-hidden className="size-3.5 shrink-0" />
                Se envía una sola vez aunque toques dos veces.
            </p>
        </FormularioPagina>
    );
}

/** Fila de un destinatario: iniciales, nombre, de dónde es y si acepta mensajes. */
function FilaDestinatario({ d }: { d: Destinatario }) {
    const origen = d.tipo === "cliente" ? ["Cliente de Argos", d.codigo] : ["Contacto de Iris", d.detalle];
    const linea = [...origen, d.numero ? formatearNumero(d.numero) : null].filter(Boolean).join(" · ");
    return (
        <>
            <AvatarUsuario nombre={d.nombre} />
            <span className="grid min-w-0">
                <span className="truncate font-semibold">{d.nombre}</span>
                <span className="truncate text-xs text-muted-foreground">{linea}</span>
                <span className={cn("flex min-w-0 items-center gap-1 text-xs", d.puede ? "text-success" : "text-muted-foreground")}>
                    {d.puede ? <CircleCheck aria-hidden className="size-3.5 shrink-0" /> : <Ban aria-hidden className="size-3.5 shrink-0" />}
                    <span className="truncate">{textoMotivoDestinatario(d.motivo)}</span>
                </span>
            </span>
        </>
    );
}

/** Cómo lo verá el cliente en WhatsApp: la plantilla con las variables resaltadas. Texto plano. */
function VistaPrevia({ plantilla, valores, nombre, canal, cola }: { plantilla: PlantillaDisponible | undefined; valores: Record<string, string>; nombre: string; canal: Numero | undefined; cola: string }) {
    const trozos = useMemo(() => (plantilla ? trozosVistaPrevia(plantilla.vistaPrevia, valores) : []), [plantilla, valores]);
    const uso = canal ? usoPrincipal(canal.usos ?? []) : undefined;
    const tono = canal ? tonoDeCanal(canal) : "neutro";
    return (
        <section aria-label="Vista previa" className="surface-card grid min-w-0 gap-3 p-4 sm:p-5 lg:sticky lg:top-20">
            <div className="grid gap-1">
                <h2 className="text-base font-semibold">Vista previa</h2>
                <p className="text-sm text-muted-foreground">Así lo verá {nombre} en WhatsApp.</p>
            </div>
            <div className="grid min-h-40 content-start gap-2 rounded-xl border border-border bg-background-secondary p-3" data-slot="vista-previa">
                {canal ? (
                    <span className={cn("inline-flex h-5 w-fit items-center gap-1 rounded-full px-2 text-[0.6875rem] font-semibold", CLASE_PILDORA[tono])}>
                        <span aria-hidden className={cn("size-1.5 rounded-full", CLASE_PUNTO[tono])} />
                        {uso ? nombreUso(uso) : canal.nombre}
                    </span>
                ) : null}
                {plantilla ? (
                    <p className="max-w-[95%] justify-self-end rounded-[0.875rem] rounded-br-sm bg-primary/10 px-3 py-2 text-sm whitespace-pre-wrap [overflow-wrap:anywhere]">
                        {trozos.map((t, i) =>
                            t.tipo === "texto" ? (
                                <span key={i}>{t.texto}</span>
                            ) : (
                                <mark key={i} data-variable={t.nombre} className="rounded bg-primary/15 px-0.5 text-foreground">
                                    {t.valor}
                                </mark>
                            ),
                        )}
                    </p>
                ) : (
                    <p className="text-sm text-muted-foreground">Elige una plantilla para ver el mensaje.</p>
                )}
            </div>
            <p className="flex gap-1.5 text-xs text-muted-foreground">
                <Info aria-hidden className="mt-0.5 size-3.5 shrink-0" />
                <span>
                    La conversación se abre a tu nombre en {cola}. Si {nombre} responde, tienes 24 h para escribir libre.
                </span>
            </p>
        </section>
    );
}

/** Error del envío con lo que se puede hacer. */
function ErrorEnvio({ error }: { error: unknown }) {
    const e = error instanceof ErrorApi ? error : null;
    if (e?.codigo === "conversacion_de_otra_cola") {
        return (
            <Aviso tono="warning" titulo="La atiende otra cola">
                {e.detalle ?? "Ese contacto ya tiene una conversación en este número que atiende otra cola. Pídele a su supervisor que te la pase."}
            </Aviso>
        );
    }
    // Cambiar a quién está asociada la conversación existente exige `conversaciones.asociar`: el
    // servidor responde `prohibido` y lo nombra. Otro 403 va con el mensaje genérico.
    if (e?.estado === 403 && e.codigo === "prohibido" && /conversaciones\.asociar/.test(e.detalle ?? "")) {
        return (
            <Aviso tono="error" titulo="No puedes cambiar el contacto de esa conversación">
                Ya hay una conversación con este número asociada a otro cliente o contacto, y cambiarla exige el permiso para asociar contactos. Pídeselo a un supervisor.
            </Aviso>
        );
    }
    if (e?.codigo === "conversacion_de_otro") {
        return (
            <Aviso tono="warning" titulo="Ese contacto ya tiene una conversación con otra persona">
                {e.detalle ?? "Pídele que te la pase, o ábrela desde Conversaciones."}
            </Aviso>
        );
    }
    const titulos: Record<string, string> = {
        baja: "Ese contacto se dio de baja: no se le pueden mandar mensajes",
        sin_permiso: "Ese contacto no aceptó mensajes",
        sin_contacto: "El cliente no tiene número de WhatsApp en Argos",
        numero_no_permitido: "Ese número de salida no es de tus colas",
        plantilla_no_aprobada: "La plantilla ya no está aprobada en ese número",
    };
    const reintentable = !e || e.estado === 0 || e.estado >= 500;
    return (
        <Aviso tono="error" titulo={(e?.codigo && titulos[e.codigo]) || "No se pudo enviar"}>
            {reintentable ? (
                <span className="inline-flex items-center gap-1">
                    <RotateCw aria-hidden className="size-3.5" />
                    Vuelve a tocar «Enviar y abrir conversación»: se envía una sola vez.
                </span>
            ) : (
                (mensajeDeError(error).detalle ?? mensajeDeError(error).titulo)
            )}
        </Aviso>
    );
}
