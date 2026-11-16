"use client";

import { zodResolver } from "@hookform/resolvers/zod";
import { avisar, Aviso, Boton, CampoHora, CampoNumero, Casilla, FormularioPagina, Interruptor, liberarProteccion, SeccionFormulario } from "@iris/ui";
import { CalendarClock, ChevronDown, Plus, Trash2 } from "lucide-react";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { useState } from "react";
import { useFieldArray, useForm, useWatch } from "react-hook-form";
import { CargaRegistro } from "@/componentes/carga-registro";
import { ErrorApi, mensajeDeError } from "@/lib/api/errores";
import { guardarAjustesAsistente, leerAjustesAsistente } from "@/lib/asistente/api";
import { perfilDefecto } from "@/lib/asistente/fase3";
import type { AjustesAsistente } from "@/lib/asistente/tipos";
import {
    campoDeErrorVisitas,
    CAMPOS_AVANZADOS,
    conVisitas,
    entradaVisitas,
    esquemaVisitas,
    MAX_BLOQUES,
    NOMBRE_TIPO_VISITA,
    textoAnticipacion,
    TIPOS_VISITA,
    valoresVisitas,
    type ValoresVisitas,
    type Visitas,
} from "@/lib/asistente/visitas";
import { useClaveIdempotencia } from "@/lib/idempotencia";
import { useLectura } from "@/lib/lectura";

const PRINCIPAL = "/administracion/asistente";

/**
 * `/administracion/asistente/visitas` (`mensajeria.asistente_configurar`): cómo agenda el asistente
 * una visita técnica (contratos/conversaciones.md §17.10): qué agenda, en qué horarios lo ofrece,
 * con qué reglas y, plegado, los topes. El bloque `visitas` se guarda completo; si el servidor no
 * lo manda, no hay formulario (no se inventan valores).
 */
export function VisitasAsistente() {
    const lectura = useLectura((signal) => leerAjustesAsistente(signal));
    return (
        <CargaRegistro lectura={lectura} que="cómo agenda el asistente" noEncontrado="Sin ajustes">
            {(ajustes) =>
                conVisitas(ajustes) ? (
                    <Formulario ajustes={ajustes} visitas={ajustes.visitas} />
                ) : (
                    <Aviso
                        tono="info"
                        icono={CalendarClock}
                        titulo="Todavía no disponible"
                        data-testid="sin-visitas"
                        accion={
                            <Boton asChild variante="secundario" tamano="sm">
                                <Link href={PRINCIPAL}>Volver al asistente</Link>
                            </Boton>
                        }
                    >
                        Esta versión del servidor todavía no deja ajustar cómo agenda el asistente.
                    </Aviso>
                )
            }
        </CargaRegistro>
    );
}

function Formulario({ ajustes, visitas }: { ajustes: AjustesAsistente; visitas: Visitas }) {
    const router = useRouter();
    const clave = useClaveIdempotencia();
    const [error, setError] = useState<unknown>(null);
    const form = useForm<ValoresVisitas>({ resolver: zodResolver(esquemaVisitas), defaultValues: valoresVisitas(ajustes, visitas) });
    const bloques = useFieldArray({ control: form.control, name: "bloques" });
    const [anticipacion, dias, maxAbiertas, visitasEmpresa] = useWatch({ control: form.control, name: ["anticipacionHoras", "diasAdelante", "maxAbiertasPorCliente", "visitasPorDiaEmpresa"] });
    const errores = form.formState.errors;
    const conflicto = error instanceof ErrorApi && error.estado === 409;
    // Lo avanzado va plegado; se abre solo si tiene un error que corregir.
    const [abierto, setAbierto] = useState(false);
    const avanzado = abierto || CAMPOS_AVANZADOS.some((c) => errores[c]);
    const encendido = perfilDefecto(ajustes).acciones.agendar_visita;
    const topePerfil = perfilDefecto(ajustes).limites.diasAdelanteMax;
    const acortado = /^\d+$/.test(dias) && Number(dias) > topePerfil;

    async function guardar(d: ValoresVisitas) {
        setError(null);
        try {
            await guardarAjustesAsistente(entradaVisitas({ ...ajustes, visitas }, d), clave.actual());
            clave.renovar();
            avisar({ titulo: "Cómo agenda el asistente: guardado" });
            await liberarProteccion();
            router.replace(PRINCIPAL);
        } catch (e) {
            clave.trasError(e);
            // El 422 del servidor (sus topes) va junto al campo que nombra.
            const deCampo = e instanceof ErrorApi && e.estado === 422 ? e.errores.map((x) => ({ campo: campoDeErrorVisitas(x.field), mensaje: x.message })) : [];
            if (deCampo.length > 0 && deCampo.every((x) => x.campo !== null)) {
                deCampo.forEach((x, i) => {
                    if (x.campo) form.setError(x.campo, { message: x.mensaje }, { shouldFocus: i === 0 && x.campo !== "bloques" && x.campo !== "tipos" });
                });
            } else setError(e);
        }
    }

    return (
        <FormularioPagina
            titulo="Cómo agenda visitas"
            descripcion="El asistente le ofrece al cliente hasta tres horarios libres de la agenda y, cuando elige uno, crea la orden. Nunca le dice qué técnico va."
            alEnviar={form.handleSubmit(guardar)}
            navegar={router.push}
            volverA={PRINCIPAL}
            sucio={form.formState.isDirty}
            barra={{
                enviando: form.formState.isSubmitting,
                ...(conflicto ? { faltan: ["la versión nueva"], motivo: "Alguien más cambió los ajustes: vuelve a abrirlos." } : {}),
            }}
            error={
                <>
                    {encendido ? null : (
                        <Aviso
                            tono="info"
                            titulo="«Agendar visitas» está apagado"
                            data-testid="visitas-apagado"
                            accion={
                                <Boton asChild variante="secundario" tamano="sm">
                                    <Link href={PRINCIPAL}>Ir a encenderlo</Link>
                                </Boton>
                            }
                        >
                            Puedes dejar esto listo; se usará cuando lo enciendas en la pregunta 2.
                        </Aviso>
                    )}
                    {error ? (
                        <Aviso tono="error" titulo={conflicto ? "Alguien más cambió los ajustes" : "No se pudo guardar"}>
                            {conflicto ? "Recarga la página para ver el cambio." : `${mensajeDeError(error).titulo}${error instanceof ErrorApi && error.detalle ? `: ${error.detalle}` : ""}`}
                        </Aviso>
                    ) : null}
                </>
            }
        >
            <SeccionFormulario titulo="Qué visitas agenda">
                <fieldset className="grid gap-2">
                    <legend className="sr-only">Tipos de visita que puede agendar</legend>
                    {TIPOS_VISITA.map((t) => (
                        <Casilla key={t} etiqueta={NOMBRE_TIPO_VISITA[t].titulo} ayuda={NOMBRE_TIPO_VISITA[t].detalle} {...form.register(`tipos.${t}`, { deps: ["tipos"] })} />
                    ))}
                </fieldset>
                {errores.tipos?.message || errores.tipos?.root?.message ? (
                    <p role="alert" className="text-sm text-error">
                        {errores.tipos.message ?? errores.tipos.root?.message}
                    </p>
                ) : null}
                <p className="text-sm text-muted-foreground">Instalaciones, retiros y cambios de equipo los agenda siempre una persona.</p>
            </SeccionFormulario>

            <SeccionFormulario titulo="Horarios que ofrece" descripcion="La hora de llegada que se le dice al cliente («entre las 8 y las 12»). Solo ofrece los que tienen un técnico libre.">
                <ul className="grid gap-3" aria-label="Horarios que ofrece">
                    {bloques.fields.map((campo, i) => {
                        const e = errores.bloques?.[i];
                        return (
                            <li key={campo.id} className="flex min-w-0 flex-wrap items-end gap-3 rounded-lg border border-border p-3" data-bloque={i + 1}>
                                <CampoHora etiqueta={`Horario ${i + 1}: desde`} className="w-36" error={e?.inicio?.message} {...form.register(`bloques.${i}.inicio`)} />
                                <CampoHora etiqueta="Hasta" aria-label={`Horario ${i + 1}: hasta`} className="w-36" error={e?.fin?.message} {...form.register(`bloques.${i}.fin`)} />
                                {bloques.fields.length > 1 ? (
                                    <Boton variante="fantasma" tamano="sm" className="ml-auto text-error" onClick={() => bloques.remove(i)} aria-label={`Quitar el horario ${i + 1}`}>
                                        <Trash2 />
                                        Quitar
                                    </Boton>
                                ) : null}
                            </li>
                        );
                    })}
                </ul>
                {errores.bloques?.root?.message || errores.bloques?.message ? (
                    <p role="alert" className="text-sm text-error">
                        {errores.bloques.root?.message ?? errores.bloques.message}
                    </p>
                ) : null}
                {bloques.fields.length < MAX_BLOQUES ? (
                    <Boton variante="secundario" tamano="sm" className="justify-self-start" onClick={() => bloques.append({ inicio: "", fin: "" })}>
                        <Plus />
                        Agregar horario
                    </Boton>
                ) : null}
                <div className="grid gap-4 sm:grid-cols-2">
                    <CampoNumero
                        etiqueta="Horas de anticipación"
                        minimo={0}
                        maximo={168}
                        ayuda={/^\d+$/.test(anticipacion) ? `Ofrece ${textoAnticipacion(Number(anticipacion))}: da tiempo a organizar al técnico.` : "Tiempo mínimo entre que el cliente pide la visita y la visita."}
                        error={errores.anticipacionHoras?.message}
                        {...form.register("anticipacionHoras")}
                    />
                    <CampoNumero
                        etiqueta="Días adelante, como mucho"
                        minimo={1}
                        maximo={30}
                        ayuda={
                            acortado
                                ? `Hoy ofrece hasta ${topePerfil === 1 ? "1 día" : `${topePerfil} días`}: lo acorta «Días adelante» de Límites.`
                                : "Hasta qué día ofrece horarios, contando desde hoy."
                        }
                        error={errores.diasAdelante?.message}
                        {...form.register("diasAdelante")}
                    />
                </div>
            </SeccionFormulario>

            <SeccionFormulario titulo="Reglas">
                <Interruptor alFinal etiqueta={<Regla titulo="Crear un ticket antes si no hay" detalle="La visita nace de un ticket, para que quede el motivo." />} {...form.register("exigeTicket")} />
                <Interruptor
                    alFinal
                    etiqueta={<Regla titulo="Agendar aunque el servicio esté cortado por mora" detalle="Apagado: si está cortado por falta de pago, lo pasa a una persona." />}
                    {...form.register("permitirConMora")}
                />
                <Interruptor
                    alFinal
                    etiqueta={<Regla titulo="Pedir DPI o NIT antes de agendar" detalle="Apagado: basta con que el cliente escriba desde su número." />}
                    {...form.register("pedirDocumento")}
                />
                <p className="text-sm text-muted-foreground">
                    Si el cliente ya tiene una visita abierta, le recuerda su número y su fecha en vez de agendar otra. Siempre pasa a una persona si no hay horarios, si la zona no tiene técnicos o si el cliente quiere
                    cambiar o cancelar una visita.
                </p>
            </SeccionFormulario>

            <section aria-labelledby="titulo-avanzado-visitas" data-slot="avanzado" className="surface-card grid min-w-0 gap-4 p-4 sm:p-5">
                <h2 id="titulo-avanzado-visitas" className="min-w-0">
                    <Boton
                        variante="fantasma"
                        className="h-auto w-full justify-between gap-3 px-0 py-0 text-left hover:bg-transparent"
                        aria-expanded={avanzado}
                        aria-controls="campos-avanzados-visitas"
                        onClick={() => setAbierto(!avanzado)}
                    >
                        <span className="grid min-w-0 gap-0.5">
                            <span className="text-base font-semibold tracking-[-0.015em]">Avanzado</span>
                            <span className="text-sm font-normal text-muted-foreground [overflow-wrap:anywhere] whitespace-normal">
                                Hasta {maxAbiertas || "—"} {maxAbiertas === "1" ? "visita abierta" : "visitas abiertas"} por cliente · {visitasEmpresa || "—"} al día en toda la empresa
                            </span>
                        </span>
                        <ChevronDown aria-hidden className={avanzado ? "size-5 shrink-0 rotate-180 transition-transform" : "size-5 shrink-0 transition-transform"} />
                    </Boton>
                </h2>
                {avanzado ? (
                    <div id="campos-avanzados-visitas" className="grid gap-4 sm:grid-cols-2">
                        <CampoNumero
                            etiqueta="Visitas abiertas por cliente"
                            minimo={1}
                            maximo={5}
                            ayuda="Si el cliente ya tiene estas visitas sin terminar, le dice el número y la fecha de la que tiene y no agenda otra. De fábrica: 1."
                            error={errores.maxAbiertasPorCliente?.message}
                            {...form.register("maxAbiertasPorCliente")}
                        />
                        <CampoNumero
                            etiqueta="Veces que ofrece horarios en una conversación, por día"
                            minimo={1}
                            maximo={10}
                            ayuda="Después de estas veces sin acuerdo, lo pasa a una persona. De fábrica: 3."
                            error={errores.ofertasPorDia?.message}
                            {...form.register("ofertasPorDia")}
                        />
                        <CampoNumero
                            etiqueta="Visitas que agenda en una conversación, por día"
                            minimo={1}
                            maximo={5}
                            ayuda="De fábrica: 1."
                            error={errores.visitasPorDia?.message}
                            {...form.register("visitasPorDia")}
                        />
                        <CampoNumero
                            etiqueta="Visitas que agenda al día en toda la empresa"
                            minimo={1}
                            maximo={500}
                            ayuda="Un freno por si algo sale mal: al llegar, las demás las agenda una persona. De fábrica: 20."
                            error={errores.visitasPorDiaEmpresa?.message}
                            {...form.register("visitasPorDiaEmpresa")}
                        />
                    </div>
                ) : null}
            </section>
        </FormularioPagina>
    );
}

function Regla({ titulo, detalle }: { titulo: string; detalle: string }) {
    return (
        <span className="grid min-w-0">
            <span className="[overflow-wrap:anywhere]">{titulo}</span>
            <span className="text-xs font-normal text-muted-foreground [overflow-wrap:anywhere]">{detalle}</span>
        </span>
    );
}
