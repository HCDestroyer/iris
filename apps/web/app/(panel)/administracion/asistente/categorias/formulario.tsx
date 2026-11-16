"use client";

import { zodResolver } from "@hookform/resolvers/zod";
import { avisar, Aviso, Campo, CampoArea, CampoNumero, CampoSelector, Casilla, FormularioPagina, Interruptor, liberarProteccion, ListaEleccion, SeccionFormulario } from "@iris/ui";
import { useRouter } from "next/navigation";
import { useState } from "react";
import { useForm, useWatch } from "react-hook-form";
import { ChipCategoria } from "@/componentes/asistente/chip-categoria";
import { ErrorApi, mensajeDeError } from "@/lib/api/errores";
import { activarCategoria, actualizarCategoria, crearCategoria } from "@/lib/asistente/api-fase3";
import { entradaCategoria, esquemaCategoria, NOMBRE_CONDICION, NOMBRE_TONO, TONOS_CATEGORIA, valoresCategoria, type Categoria, type ModoCategoria, type ValoresCategoria } from "@/lib/asistente/fase3";
import { erroresACampos } from "@/lib/formularios";
import { useClaveIdempotencia } from "@/lib/idempotencia";
import type { Apoyo } from "./datos";

const LISTA = "/administracion/asistente/categorias";
const CAMPOS = ["nombre", "descripcion", "tono", "colaId", "slaPrimeraRespuestaMin", "modo", "condicion", "manualIds"] as const;

/**
 * `/administracion/asistente/categorias/nueva` y `/[id]/editar`: un tipo de mensaje con su chip, la
 * cola a la que va, el tiempo de primera respuesta, lo que el asistente hace solo y los manuales en
 * los que se apoya (contratos/conversaciones.md §17).
 */
export function FormularioCategoria({ categoria, apoyo }: { categoria?: Categoria; apoyo: Apoyo }) {
    const router = useRouter();
    const clave = useClaveIdempotencia();
    const claveActiva = useClaveIdempotencia();
    const [error, setError] = useState<unknown>(null);
    // Lo ya guardado si el segundo paso (activar o desactivar) falla.
    const [guardada, setGuardada] = useState<Categoria | null>(null);
    const form = useForm<ValoresCategoria>({ resolver: zodResolver(esquemaCategoria), defaultValues: valoresCategoria(categoria) });
    const [nombre, tono, modo, manualIds] = useWatch({ control: form.control, name: ["nombre", "tono", "modo", "manualIds"] });
    const errores = form.formState.errors;
    const conflicto = error instanceof ErrorApi && error.estado === 409;
    // Los manuales activos y, además, los que la categoría ya tenía (aunque estén inactivos).
    const manuales = apoyo.manuales.filter((m) => m.activo || (categoria?.manualIds ?? []).includes(m.id));

    async function guardar(d: ValoresCategoria) {
        setError(null);
        let paso: "datos" | "activa" = "datos";
        try {
            const base = guardada ?? categoria;
            let actual = guardada;
            if (!actual) {
                actual = base ? await actualizarCategoria(base.id, { ...entradaCategoria(d), version: base.version }, clave.actual()) : await crearCategoria(entradaCategoria(d), clave.actual());
                clave.renovar();
                setGuardada(actual);
            }
            paso = "activa";
            if (actual.activa !== d.activa) {
                actual = await activarCategoria(actual.id, d.activa, actual.version, claveActiva.actual());
                claveActiva.renovar();
            }
            avisar({ titulo: categoria ? `Categoría «${actual.nombre}» guardada` : `Categoría «${actual.nombre}» creada` });
            await liberarProteccion();
            router.replace(LISTA);
        } catch (e) {
            (paso === "datos" ? clave : claveActiva).trasError(e);
            if (paso === "activa" || !erroresACampos(e, CAMPOS, form.setError)) setError(e);
        }
    }

    return (
        <FormularioPagina
            titulo={categoria ? `Editar categoría «${categoria.nombre}»` : "Nueva categoría"}
            descripcion="El asistente clasifica cada mensaje nuevo en una categoría."
            alEnviar={form.handleSubmit(guardar)}
            navegar={router.push}
            volverA={LISTA}
            sucio={form.formState.isDirty}
            barra={{
                textoGuardar: categoria ? "Guardar cambios" : "Crear categoría",
                enviando: form.formState.isSubmitting,
                ...(conflicto ? { faltan: ["la versión nueva"], motivo: "Alguien más cambió esta categoría: vuelve a abrirla." } : {}),
            }}
            error={
                error ? (
                    <Aviso tono="error" titulo={conflicto ? "Alguien más cambió esta categoría" : guardada ? "Se guardó la categoría, pero no su estado" : "No se pudo guardar"}>
                        {conflicto ? "Vuelve a abrirla desde la lista para ver el cambio." : guardada ? `${mensajeDeError(error).titulo}. Vuelve a tocar «Guardar»: sigue desde ahí.` : mensajeDeError(error).titulo}
                    </Aviso>
                ) : null
            }
        >
            <SeccionFormulario titulo="Categoría">
                <Campo etiqueta="Nombre" maxLength={60} placeholder="Sin internet" error={errores.nombre?.message} {...form.register("nombre")} />
                <CampoArea
                    etiqueta="Descripción"
                    rows={3}
                    maxLength={500}
                    placeholder="El cliente dice que no tiene servicio, que está lento o que se le cae."
                    ayuda="Con tus palabras: el asistente la usa para reconocer estos mensajes."
                    error={errores.descripcion?.message}
                    {...form.register("descripcion")}
                />
                <div className="grid gap-3 sm:grid-cols-[minmax(0,12rem)_minmax(0,1fr)] sm:items-end">
                    <CampoSelector etiqueta="Color del chip" error={errores.tono?.message} {...form.register("tono")}>
                        {TONOS_CATEGORIA.map((t) => (
                            <option key={t} value={t}>
                                {NOMBRE_TONO[t]}
                            </option>
                        ))}
                    </CampoSelector>
                    <p className="flex min-w-0 items-center gap-2 pb-2 text-sm text-muted-foreground" data-slot="vista-chip">
                        Así se ve:
                        <ChipCategoria categoria={{ nombre: nombre.trim() || "Nombre", tono }} />
                    </p>
                </div>
                {categoria ? <Interruptor etiqueta="Categoría activa" ayuda="Inactiva: el asistente deja de usarla para clasificar." {...form.register("activa")} /> : null}
            </SeccionFormulario>

            <SeccionFormulario titulo="Cola y tiempo de respuesta">
                <CampoSelector etiqueta="Cola" ayuda="A dónde va la conversación cuando hace falta una persona." error={errores.colaId?.message} {...form.register("colaId")}>
                    <option value="">La cola del número</option>
                    {apoyo.colas
                        .filter((c) => c.activa || c.id === categoria?.colaId)
                        .map((c) => (
                            <option key={c.id} value={String(c.id)}>
                                {c.nombre}
                            </option>
                        ))}
                </CampoSelector>
                <CampoNumero
                    etiqueta="Primera respuesta en (min)"
                    minimo={1}
                    maximo={10080}
                    placeholder="El de la cola"
                    ayuda="Vacío: vale el tiempo de respuesta de la cola."
                    error={errores.slaPrimeraRespuestaMin?.message}
                    {...form.register("slaPrimeraRespuestaMin")}
                />
            </SeccionFormulario>

            <SeccionFormulario titulo="Qué hace el asistente">
                <ListaEleccion
                    leyenda="Qué hace el asistente con estos mensajes"
                    leyendaOculta
                    nombre="modo"
                    valor={modo}
                    alCambiar={(v) => form.setValue("modo", v as ModoCategoria, { shouldDirty: true })}
                    error={errores.modo?.message}
                    opciones={[
                        { valor: "responde_solo", texto: "Responde solo", contenido: <Opcion titulo="Responde solo" detalle="Contesta él cuando atiende solo; si duda, deja la conversación en la cola." /> },
                        { valor: "sugiere", texto: "Sugiere al agente", contenido: <Opcion titulo="Sugiere al agente" detalle="Propone la respuesta y una persona la revisa y la envía." /> },
                    ]}
                />
                {modo === "responde_solo" ? (
                    <CampoSelector etiqueta="Cuándo responde solo" error={errores.condicion?.message} {...form.register("condicion")}>
                        <option value="siempre">{NOMBRE_CONDICION.siempre}</option>
                        <option value="si_hay_falla">{NOMBRE_CONDICION.si_hay_falla}</option>
                        <option value="solo_verificados">{NOMBRE_CONDICION.solo_verificados}</option>
                    </CampoSelector>
                ) : null}
            </SeccionFormulario>

            <SeccionFormulario titulo="Manuales base" descripcion="Los que el asistente consulta primero para estos mensajes.">
                {manuales.length === 0 ? <p className="text-sm text-muted-foreground">Todavía no hay manuales en la biblioteca.</p> : null}
                <div className="grid gap-2">
                    {manuales.map((m) => (
                        <Casilla
                            key={m.id}
                            etiqueta={m.activo ? m.titulo : `${m.titulo} (inactivo)`}
                            checked={manualIds.includes(m.id)}
                            onChange={(e) =>
                                form.setValue("manualIds", e.target.checked ? [...manualIds, m.id] : manualIds.filter((x) => x !== m.id), { shouldDirty: true, shouldValidate: form.formState.isSubmitted })
                            }
                        />
                    ))}
                </div>
                {errores.manualIds?.message ? (
                    <p role="alert" className="text-sm text-error">
                        {errores.manualIds.message}
                    </p>
                ) : null}
            </SeccionFormulario>
        </FormularioPagina>
    );
}

function Opcion({ titulo, detalle }: { titulo: string; detalle: string }) {
    return (
        <span className="grid min-w-0 gap-0.5">
            <span className="text-sm font-semibold">{titulo}</span>
            <span className="text-sm text-muted-foreground">{detalle}</span>
        </span>
    );
}
