"use client";

import { zodResolver } from "@hookform/resolvers/zod";
import { avisar, Aviso, AreaTexto, Boton, Cargando, cn, FormularioPagina, Interruptor, liberarProteccion, ListaEleccion, SeccionFormulario, Selector } from "@iris/ui";
import {
    BookOpen,
    CalendarDays,
    ChevronRight,
    CircleDollarSign,
    Hand,
    Handshake,
    Info,
    Moon,
    PlugZap,
    Plug,
    Receipt,
    Sparkles,
    Ticket,
    UserRound,
    WifiOff,
    Zap,
    type LucideIcon,
} from "lucide-react";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { useState } from "react";
import { useForm, useWatch, type UseFormReturn } from "react-hook-form";
import { ChipCategoria } from "@/componentes/asistente/chip-categoria";
import { CargaRegistro } from "@/componentes/carga-registro";
import { ErrorAsistente } from "@/componentes/conversaciones/error-asistente";
import { ErrorLectura } from "@/componentes/error-lectura";
import { ErrorApi, mensajeDeError } from "@/lib/api/errores";
import { leerUso } from "@/lib/asistente/api";
import { actualizarCategoria, guardarAjustesFase3, leerAjustesFase3, leerCategorias, leerEfecto, probarMensaje } from "@/lib/asistente/api-fase3";
import {
    ajustesDePrueba,
    completar,
    duracionLegible,
    entradaDeCategoria,
    entradaGuardada,
    ESPERAS_MIN,
    esquemaPrincipal,
    NOMBRE_MODO,
    proveedorConectado,
    TEXTO_PRUEBA_MAX,
    textoHaria,
    valoresPrincipal,
    type AjustesCompletos,
    type Categoria,
    type ClaveAccion,
    type ModoAutonomo,
    type ModoCategoria,
    type PruebaMensaje,
    type ValoresPrincipal,
} from "@/lib/asistente/fase3";
import { explicarErrorDeApi, nombreMes, textoPausado, type ExplicacionError } from "@/lib/asistente/presentacion";
import { useClaveIdempotencia } from "@/lib/idempotencia";
import { useLectura } from "@/lib/lectura";

const BASE = "/administracion/asistente";

const MODOS: readonly { valor: ModoAutonomo; titulo: string; detalle: string; icono: LucideIcon; recomendado?: boolean }[] = [
    { valor: "nunca", titulo: "Nunca", detalle: "Solo le sugiere respuestas a tus agentes.", icono: Hand },
    { valor: "sin_nadie", titulo: "Cuando no hay nadie", detalle: "Fuera de horario, o si ningún agente responde a tiempo.", icono: Moon, recomendado: true },
    { valor: "siempre_primero", titulo: "Siempre primero", detalle: "Responde al instante y pasa a una persona cuando hace falta.", icono: Zap },
];

const ACCIONES: readonly { clave: ClaveAccion; titulo: string; nota?: string; icono: LucideIcon }[] = [
    { clave: "responder_dudas", titulo: "Responder dudas", nota: "Con tus manuales", icono: BookOpen },
    { clave: "avisar_fallas", titulo: "Avisar de fallas en la zona", icono: WifiOff },
    { clave: "crear_ticket", titulo: "Crear tickets", icono: Ticket },
    { clave: "agendar_visita", titulo: "Agendar visitas", icono: CalendarDays },
    { clave: "recibir_boleta", titulo: "Recibir boletas de pago", nota: "Caja las aprueba en Argos", icono: Receipt },
    { clave: "dar_saldo", titulo: "Dar el saldo", nota: "Solo a clientes verificados", icono: CircleDollarSign },
    { clave: "promesa_pago", titulo: "Promesas de pago", nota: "Con las reglas de Argos", icono: Handshake },
    { clave: "reconexion_temporal", titulo: "Reconexión temporal", nota: "Con las reglas de Argos", icono: Plug },
];

type Datos = { ajustes: AjustesCompletos; categorias: Categoria[] };

/**
 * `/administracion/asistente` (`mensajeria.asistente_configurar`): el asistente en tres preguntas
 * (diseño «más amigable y práctico» del PM; contratos/conversaciones.md §17): cuándo atiende solo,
 * qué puede hacer solo y qué hace con cada tipo de mensaje, con «Pruébalo antes de guardar». Lo
 * avanzado (conexión, límites y categorías) va en sus propias rutas.
 */
export function AsistentePrincipal() {
    const lectura = useLectura<Datos>(async (signal) => {
        const [ajustes, categorias] = await Promise.all([leerAjustesFase3(signal), leerCategorias(signal)]);
        return { ajustes: completar(ajustes), categorias };
    });
    return (
        <CargaRegistro lectura={lectura} que="el asistente" noEncontrado="Sin ajustes">
            {(d) => <Formulario key={`${d.ajustes.version}:${d.categorias.map((c) => `${c.id}.${c.version}`).join(",")}`} datos={d} alGuardar={lectura.reemplazar} />}
        </CargaRegistro>
    );
}

function Formulario({ datos, alGuardar }: { datos: Datos; alGuardar: (d: Datos) => void }) {
    const router = useRouter();
    const [error, setError] = useState<unknown>(null);
    // Lo ya guardado de un intento que falló a medias (los ajustes sí, una categoría no).
    const [guardado, setGuardado] = useState<Datos>(datos);
    const claveAjustes = useClaveIdempotencia();
    // Las categorías se guardan de una en una y se para en la primera que falla: una clave basta.
    const claveCategoria = useClaveIdempotencia();
    const { ajustes } = datos;
    const categorias = datos.categorias.filter((c) => c.activa);
    const form = useForm<ValoresPrincipal>({ resolver: zodResolver(esquemaPrincipal), defaultValues: valoresPrincipal(ajustes, datos.categorias) });
    const [activo, modoAutonomo, esperaMin] = useWatch({ control: form.control, name: ["activo", "modoAutonomo", "esperaMin"] });
    const conectado = proveedorConectado(ajustes);
    const conflicto = error instanceof ErrorApi && error.estado === 409;

    async function guardar(d: ValoresPrincipal) {
        setError(null);
        let paso: "ajustes" | "categoria" = "ajustes";
        let actual = guardado;
        try {
            const a = actual.ajustes;
            const cambio = a.activo !== d.activo || a.modoAutonomo !== d.modoAutonomo || a.esperaMin !== d.esperaMin || ACCIONES.some((x) => a.acciones[x.clave] !== d.acciones[x.clave]);
            if (cambio) {
                const r = await guardarAjustesFase3({ ...entradaGuardada(a), activo: d.activo, modoAutonomo: d.modoAutonomo, esperaMin: d.esperaMin, acciones: { ...d.acciones } }, claveAjustes.actual());
                claveAjustes.renovar();
                actual = { ...actual, ajustes: completar(r) };
                setGuardado(actual);
            }
            paso = "categoria";
            for (const c of actual.categorias) {
                const modo = d.modos[String(c.id)];
                if (!modo || modo === c.modo) continue;
                const r = await actualizarCategoria(c.id, { ...entradaDeCategoria(c), modo, condicion: modo === "responde_solo" ? c.condicion : null, version: c.version }, claveCategoria.actual());
                claveCategoria.renovar();
                actual = { ...actual, categorias: actual.categorias.map((x) => (x.id === r.id ? r : x)) };
                setGuardado(actual);
            }
            avisar({ titulo: actual.ajustes.activo ? "Asistente guardado y encendido" : "Asistente guardado (apagado)" });
            await liberarProteccion();
            alGuardar(actual);
        } catch (e) {
            (paso === "ajustes" ? claveAjustes : claveCategoria).trasError(e);
            setError(e);
        }
    }

    const bloqueado = !conectado;
    return (
        <FormularioPagina
            titulo="Asistente"
            descripcion="Tres preguntas y listo. Puedes cambiarlo cuando quieras."
            accionTitulo={
                <Interruptor
                    etiqueta={
                        <>
                            <span className="sr-only">Asistente encendido</span>
                            <span aria-hidden className="hidden text-muted-foreground sm:inline">
                                {activo ? "Encendido" : "Apagado"}
                            </span>
                        </>
                    }
                    disabled={bloqueado}
                    {...form.register("activo")}
                />
            }
            alEnviar={form.handleSubmit(guardar)}
            navegar={router.push}
            volverA="/administracion"
            sucio={form.formState.isDirty}
            barra={{
                enviando: form.formState.isSubmitting,
                ...(bloqueado ? { faltan: ["conectar la IA"], motivo: "Conecta la IA para poder guardar." } : {}),
                ...(conflicto ? { faltan: ["la versión nueva"], motivo: "Alguien más cambió los ajustes: recarga la página." } : {}),
            }}
            error={
                <>
                    {bloqueado ? (
                        <Aviso
                            tono="warning"
                            icono={PlugZap}
                            titulo="Falta conectar la IA"
                            data-testid="sin-conexion"
                            accion={
                                <Boton asChild tamano="sm">
                                    <Link href={`${BASE}/conexion`}>
                                        <PlugZap />
                                        Conectar la IA
                                    </Link>
                                </Boton>
                            }
                        >
                            El asistente necesita un proveedor de IA para sugerir y responder. Conéctalo y vuelve aquí.
                        </Aviso>
                    ) : null}
                    <AvisoPausa />
                    {error ? (
                        <Aviso tono="error" titulo={conflicto ? "Alguien más cambió los ajustes" : "No se pudo guardar"}>
                            {conflicto ? "Recarga la página para ver el cambio." : `${mensajeDeError(error).titulo}${error instanceof ErrorApi && error.detalle ? `: ${error.detalle}` : ""}. Vuelve a tocar «Guardar».`}
                        </Aviso>
                    ) : null}
                </>
            }
        >
            <SeccionFormulario titulo="1 · ¿Cuándo atiende solo?">
                <ListaEleccion
                    leyenda="¿Cuándo atiende solo?"
                    leyendaOculta
                    nombre="modoAutonomo"
                    valor={modoAutonomo}
                    alCambiar={(v) => form.setValue("modoAutonomo", v as ModoAutonomo, { shouldDirty: true })}
                    opciones={MODOS.map((m) => {
                        const proximamente = m.valor !== "nunca" && !ajustes.autonomoDisponible;
                        return {
                            valor: m.valor,
                            texto: `${m.titulo}${proximamente ? " (próximamente)" : ""}`,
                            deshabilitada: bloqueado || proximamente,
                            contenido: (
                                <span className="grid min-w-0 gap-0.5">
                                    <span className="flex min-w-0 flex-wrap items-center gap-x-2 gap-y-1 text-sm font-semibold">
                                        <m.icono aria-hidden className="size-4 shrink-0" />
                                        {m.titulo}
                                        {proximamente ? (
                                            <Pildora className="bg-elevated text-muted-foreground">Próximamente</Pildora>
                                        ) : m.recomendado ? (
                                            <Pildora className="bg-success/10 text-success">Recomendado</Pildora>
                                        ) : null}
                                    </span>
                                    <span className="text-sm text-muted-foreground">{m.detalle}</span>
                                </span>
                            ),
                        };
                    })}
                />
                {modoAutonomo === "sin_nadie" ? (
                    <div className="flex min-w-0 flex-wrap items-center gap-x-3 gap-y-1.5 rounded-lg bg-primary/5 px-3 py-2.5" data-slot="espera-agente">
                        <span id="etiqueta-espera" className="text-sm font-medium">
                            Esperar a un agente
                        </span>
                        <Selector
                            aria-labelledby="etiqueta-espera"
                            tamano="sm"
                            className="w-28"
                            disabled={bloqueado || !ajustes.autonomoDisponible}
                            value={String(esperaMin)}
                            onChange={(e) => form.setValue("esperaMin", Number(e.target.value), { shouldDirty: true })}
                        >
                            {[...new Set([...ESPERAS_MIN, esperaMin])]
                                .sort((a, b) => a - b)
                                .map((n) => (
                                    <option key={n} value={String(n)}>
                                        {n} min
                                    </option>
                                ))}
                        </Selector>
                    </div>
                ) : null}
            </SeccionFormulario>

            <SeccionFormulario titulo="2 · ¿Qué puede hacer solo?">
                {ajustes.autonomoDisponible ? null : (
                    <p className="flex items-start gap-1.5 text-sm text-muted-foreground" data-slot="nota-autonomo">
                        <Info aria-hidden className="mt-0.5 size-4 shrink-0" />
                        Se aplicará cuando el asistente atienda solo.
                    </p>
                )}
                <ul aria-label="Lo que el asistente puede hacer solo" className="grid">
                    {ACCIONES.map((a) => (
                        <li key={a.clave} className="flex min-w-0 items-center gap-3 border-b border-border py-2.5 first:pt-0" data-accion={a.clave}>
                            <a.icono aria-hidden className="size-[1.125rem] shrink-0 text-muted-foreground" />
                            <Interruptor
                                alFinal
                                className="min-w-0 flex-1"
                                disabled={bloqueado}
                                etiqueta={
                                    <span className="grid min-w-0">
                                        <span className="[overflow-wrap:anywhere]">{a.titulo}</span>
                                        {a.nota ? <span className="text-xs font-normal text-muted-foreground">{a.nota}</span> : null}
                                    </span>
                                }
                                {...form.register(`acciones.${a.clave}`)}
                            />
                        </li>
                    ))}
                </ul>
                <EnlaceSeguir href={`${BASE}/limites`}>Ajustar límites</EnlaceSeguir>
                <p className="flex items-start gap-1.5 text-sm text-muted-foreground">
                    <UserRound aria-hidden className="mt-0.5 size-4 shrink-0" />
                    Siempre pasa a una persona si el cliente lo pide, se enoja o no sabe la respuesta.
                </p>
            </SeccionFormulario>

            <SeccionFormulario titulo="3 · ¿Qué hace con cada tipo de mensaje?">
                {categorias.length === 0 ? <p className="text-sm text-muted-foreground">Todavía no hay tipos de mensaje. Agrega el primero para que el asistente los clasifique.</p> : null}
                <ul aria-label="Tipos de mensaje" className="grid gap-2.5">
                    {categorias.map((c) => (
                        <FilaCategoria key={c.id} categoria={c} form={form} deshabilitado={bloqueado} />
                    ))}
                </ul>
                <p className="flex flex-wrap items-center gap-x-1.5 gap-y-1 text-sm">
                    <Link href={`${BASE}/categorias/nueva`} className="font-medium text-primary underline-offset-2 hover:underline">
                        Agregar tipo
                    </Link>
                    <span aria-hidden className="text-muted-foreground">
                        ·
                    </span>
                    <EnlaceSeguir href={`${BASE}/categorias`}>Colas y tiempos de respuesta</EnlaceSeguir>
                </p>
            </SeccionFormulario>

            <Pruebalo form={form} deshabilitado={bloqueado} />
            <Efecto />

            <nav aria-label="Ajustes avanzados" className="flex flex-wrap gap-x-5 gap-y-2 px-1 text-sm">
                <EnlaceSeguir href={`${BASE}/conexion`}>Conexión con la IA, fuentes y uso</EnlaceSeguir>
                <EnlaceSeguir href={`${BASE}/limites`}>Límites</EnlaceSeguir>
                <EnlaceSeguir href={`${BASE}/categorias`}>Categorías</EnlaceSeguir>
            </nav>
        </FormularioPagina>
    );
}

function Pildora({ className, children }: { className: string; children: string }) {
    return <span className={cn("rounded-full px-2 py-0.5 text-xs font-semibold", className)}>{children}</span>;
}

function EnlaceSeguir({ href, children }: { href: string; children: string }) {
    return (
        <Link href={href} className="inline-flex items-center gap-0.5 font-medium text-primary underline-offset-2 hover:underline">
            {children}
            <ChevronRight aria-hidden className="size-4" />
        </Link>
    );
}

function FilaCategoria({ categoria, form, deshabilitado }: { categoria: Categoria; form: UseFormReturn<ValoresPrincipal>; deshabilitado: boolean }) {
    const modo = useWatch({ control: form.control, name: `modos.${categoria.id}` }) ?? categoria.modo;
    return (
        <li className="flex min-w-0 items-center justify-between gap-3" data-categoria={categoria.id}>
            <ChipCategoria categoria={categoria} />
            <Selector
                aria-label={`Qué hace con «${categoria.nombre}»`}
                className="w-[11.5rem] shrink-0"
                disabled={deshabilitado}
                value={modo}
                onChange={(e) => form.setValue(`modos.${categoria.id}`, e.target.value as ModoCategoria, { shouldDirty: true })}
            >
                <option value="responde_solo">{NOMBRE_MODO.responde_solo}</option>
                <option value="sugiere">{NOMBRE_MODO.sugiere}</option>
            </Selector>
        </li>
    );
}

type EstadoPrueba = { tipo: "nada" } | { tipo: "probando" } | { tipo: "hecho"; prueba: PruebaMensaje } | { tipo: "error"; explicacion: ExplicacionError };

/**
 * «Pruébalo antes de guardar»: `POST /asistente/probar-mensaje` con lo que hay en el formulario,
 * **sin guardar**. Muestra la categoría, qué haría, la respuesta y las acciones que tomaría. No
 * envía nada a nadie.
 */
function Pruebalo({ form, deshabilitado }: { form: UseFormReturn<ValoresPrincipal>; deshabilitado: boolean }) {
    const [texto, setTexto] = useState("");
    const [estado, setEstado] = useState<EstadoPrueba>({ tipo: "nada" });
    const limpio = texto.trim();

    async function probar() {
        if (!limpio || estado.tipo === "probando") return;
        setEstado({ tipo: "probando" });
        try {
            setEstado({ tipo: "hecho", prueba: await probarMensaje(limpio, ajustesDePrueba(form.getValues())) });
        } catch (e) {
            const api = e instanceof ErrorApi ? e : new ErrorApi({ estado: 0, codigo: "sin_conexion", titulo: "Sin conexión" });
            setEstado({ tipo: "error", explicacion: explicarErrorDeApi(api, "prueba", Date.now()) });
        }
    }

    return (
        <section aria-labelledby="titulo-pruebalo" data-slot="pruebalo" className="grid min-w-0 gap-3 rounded-[var(--hx-radius-card)] border border-primary/30 bg-primary/[0.06] p-4 sm:p-5 [&>*]:min-w-0">
            <h2 id="titulo-pruebalo" className="flex items-center gap-2 text-base font-semibold tracking-[-0.015em]">
                <Sparkles aria-hidden className="size-[1.125rem] text-primary" />
                Pruébalo antes de guardar
            </h2>
            <div className="flex min-w-0 flex-col gap-2 sm:flex-row sm:items-start">
                <AreaTexto
                    aria-label="Mensaje de prueba"
                    rows={2}
                    maxLength={TEXTO_PRUEBA_MAX}
                    placeholder="Escribe lo que diría un cliente: ¿Cuánto cuesta el plan de 100 megas?"
                    className="min-h-11 flex-1 bg-card"
                    value={texto}
                    disabled={deshabilitado}
                    onChange={(e) => setTexto(e.target.value)}
                    onKeyDown={(e) => {
                        // Enter prueba (no guarda el formulario); Mayús + Enter baja de línea.
                        if (e.key === "Enter" && !e.shiftKey) {
                            e.preventDefault();
                            void probar();
                        }
                    }}
                />
                <Boton onClick={() => void probar()} disabled={deshabilitado || !limpio || estado.tipo === "probando"} className="shrink-0">
                    {estado.tipo === "probando" ? <Cargando texto="Probando…" /> : "Probar"}
                </Boton>
            </div>
            {estado.tipo === "probando" ? (
                <p role="status" className="text-sm text-muted-foreground">
                    El asistente está leyendo el mensaje…
                </p>
            ) : estado.tipo === "error" ? (
                <ErrorAsistente explicacion={estado.explicacion} alReintentar={() => void probar()} />
            ) : estado.tipo === "hecho" ? (
                <ResultadoPrueba prueba={estado.prueba} />
            ) : (
                <p className="text-sm text-muted-foreground">Usa los ajustes de arriba aunque no los hayas guardado. No se envía nada a ningún cliente.</p>
            )}
        </section>
    );
}

function ResultadoPrueba({ prueba }: { prueba: PruebaMensaje }) {
    return (
        <div role="status" aria-label="Resultado de la prueba" data-haria={prueba.haria} className="grid min-w-0 gap-2.5">
            <p className="flex min-w-0 flex-wrap items-center gap-x-2 gap-y-1.5 text-sm">
                {prueba.categoria ? <ChipCategoria categoria={prueba.categoria} /> : <span className="text-muted-foreground">Sin categoría</span>}
                <span className="min-w-0 font-medium [overflow-wrap:anywhere]">{textoHaria(prueba)}</span>
            </p>
            {prueba.respuesta ? (
                <p data-slot="respuesta-prueba" className="rounded-[0.875rem] rounded-bl-sm border border-border bg-card px-3 py-2 text-sm whitespace-pre-wrap [overflow-wrap:anywhere]">
                    {prueba.respuesta}
                </p>
            ) : null}
            {prueba.acciones.length > 0 ? (
                <ul aria-label="Lo que haría" className="grid gap-1 text-sm">
                    {prueba.acciones.map((a, i) => (
                        <li key={`${a.tipo}-${i}`} className="flex items-start gap-1.5" data-accion-prueba={a.tipo}>
                            <ChevronRight aria-hidden className="mt-0.5 size-4 shrink-0 text-primary" />
                            <span className="min-w-0 [overflow-wrap:anywhere]">{a.descripcion}</span>
                        </li>
                    ))}
                </ul>
            ) : null}
        </div>
    );
}

/** Con el uso en pausa por el tope, se dice aquí (el aviso de la campana trae a esta pantalla). */
function AvisoPausa() {
    const uso = useLectura((signal) => leerUso(undefined, signal));
    if (uso.estado !== "listo" || !uso.datos.pausado) return null;
    return (
        <Aviso
            tono="error"
            titulo="En pausa por el tope"
            data-testid="uso-pausado"
            accion={
                <Boton asChild variante="secundario" tamano="sm">
                    <Link href={`${BASE}/conexion#uso`}>Ver el uso y el tope</Link>
                </Boton>
            }
        >
            Se llegó al tope del mes y no sugiere ni resume. {textoPausado(uso.datos.mes)}
        </Aviso>
    );
}

/** «Efecto en el SLA (este mes)» (`GET /asistente/efecto`). Si no se pudo leer: «Reintentar», sin cifras. */
function Efecto() {
    const lectura = useLectura((signal) => leerEfecto(undefined, signal));
    return (
        <SeccionFormulario titulo="Efecto en el SLA (este mes)">
            {lectura.estado === "cargando" ? (
                <p className="text-sm text-muted-foreground">Leyendo el efecto del mes…</p>
            ) : lectura.estado === "error" ? (
                <ErrorLectura error={lectura.error} reintentar={lectura.reintentar} que="el efecto del mes" />
            ) : (
                <div className="grid gap-4 sm:grid-cols-2" data-slot="efecto-asistente">
                    <div>
                        <p className="text-2xl font-semibold tabular-nums">{lectura.datos.primeraRespuestaSeg === null ? "—" : duracionLegible(lectura.datos.primeraRespuestaSeg)}</p>
                        <p className="text-sm text-muted-foreground">
                            {lectura.datos.primeraRespuestaSeg === null
                                ? `Todavía no hay conversaciones en ${nombreMes(lectura.datos.mes)}`
                                : `primera respuesta promedio${lectura.datos.primeraRespuestaAntesSeg !== null ? ` (antes ${duracionLegible(lectura.datos.primeraRespuestaAntesSeg)})` : ""}`}
                        </p>
                    </div>
                    <div>
                        <p className="text-2xl font-semibold tabular-nums">{lectura.datos.resueltasSinPersonaPct === null ? "—" : `${Math.round(lectura.datos.resueltasSinPersonaPct)} %`}</p>
                        <p className="text-sm text-muted-foreground">resueltas por el asistente sin pasar a una persona</p>
                    </div>
                </div>
            )}
        </SeccionFormulario>
    );
}
