"use client";

import { zodResolver } from "@hookform/resolvers/zod";
import { avisar, Aviso, Campo, CampoNumero, CampoSelector, FormularioPagina, liberarProteccion, SeccionFormulario } from "@iris/ui";
import { useRouter } from "next/navigation";
import { useState } from "react";
import { useForm, useWatch } from "react-hook-form";
import { AccionesAsistente } from "@/componentes/asistente/acciones";
import { ErrorApi, mensajeDeError } from "@/lib/api/errores";
import { guardarAjustesAsistente } from "@/lib/asistente/api";
import { NOMBRE_PRIORIDAD, PRIORIDADES, type PerfilAsistente } from "@/lib/asistente/fase3";
import { codigoDePerfil, entradaPerfiles, esDefecto, herenciaDelGeneral, esquemaPerfil, perfilDeValores, perfilesCon, perfilesDe, valoresPerfil, type ValoresPerfil } from "@/lib/asistente/fase3b";
import type { AjustesAsistente } from "@/lib/asistente/tipos";
import { useClaveIdempotencia } from "@/lib/idempotencia";

const LISTA = "/administracion/asistente/perfiles";
const CAMPOS = ["nombre", "prioridadMaxima", "diasAdelanteMax"] as const;

/**
 * `/administracion/asistente/perfiles/nuevo` y `/[codigo]/editar`: un perfil del asistente
 * (contratos/conversaciones.md §17.1): qué puede hacer solo y hasta dónde llega. Se elige en cada
 * franja del horario; el general («defecto») vale en las demás. El código lo arma el panel con el
 * nombre: la persona no lo escribe y no cambia al editar (las franjas lo nombran).
 */
export function FormularioPerfil({ ajustes, perfil }: { ajustes: AjustesAsistente; perfil?: PerfilAsistente }) {
    const router = useRouter();
    const clave = useClaveIdempotencia();
    const [error, setError] = useState<unknown>(null);
    const form = useForm<ValoresPerfil>({ resolver: zodResolver(esquemaPerfil), defaultValues: valoresPerfil(perfil) });
    const [saldoConNumero, daSaldo] = useWatch({ control: form.control, name: ["saldoConNumero", "acciones.dar_saldo"] });
    const errores = form.formState.errors;
    const conflicto = error instanceof ErrorApi && error.estado === 409;
    const general = perfil ? esDefecto(perfil) : false;

    async function guardar(d: ValoresPerfil) {
        setError(null);
        const otros = perfilesDe(ajustes).filter((p) => p.codigo !== perfil?.codigo);
        if (otros.some((p) => p.nombre.trim().toLowerCase() === d.nombre.trim().toLowerCase())) {
            form.setError("nombre", { message: "Ya hay un perfil con ese nombre" }, { shouldFocus: true });
            return;
        }
        const nuevo = perfilDeValores(
            d,
            perfil?.codigo ??
                codigoDePerfil(
                    d.nombre,
                    otros.map((p) => p.codigo),
                ),
            // Editar conserva lo del perfil; uno nuevo hereda del general la regla de las visitas.
            perfil ?? herenciaDelGeneral(ajustes),
        );
        try {
            await guardarAjustesAsistente(entradaPerfiles(ajustes, perfilesCon(ajustes, nuevo)), clave.actual());
            clave.renovar();
            avisar({ titulo: perfil ? `Perfil «${nuevo.nombre}» guardado` : `Perfil «${nuevo.nombre}» creado`, ...(perfil ? {} : { descripcion: "Elígelo en las franjas de un horario para que se use." }) });
            await liberarProteccion();
            router.replace(LISTA);
        } catch (e) {
            clave.trasError(e);
            // El servidor nombra los campos dentro del perfil (`body.perfiles[2].nombre`).
            const campo = e instanceof ErrorApi && e.estado === 422 ? CAMPOS.find((c) => e.errores.some((x) => x.field.endsWith(c))) : undefined;
            if (campo && e instanceof ErrorApi) form.setError(campo, { message: e.errores.find((x) => x.field.endsWith(campo))?.message ?? e.titulo }, { shouldFocus: true });
            else setError(e);
        }
    }

    return (
        <FormularioPagina
            titulo={perfil ? `Editar el perfil «${perfil.nombre}»` : "Nuevo perfil"}
            descripcion={general ? "El de siempre: vale en todas las franjas donde no elijas otro." : "Lo que el asistente puede hacer solo en las franjas de horario donde elijas este perfil."}
            alEnviar={form.handleSubmit(guardar)}
            navegar={router.push}
            volverA={LISTA}
            sucio={form.formState.isDirty}
            barra={{
                textoGuardar: perfil ? "Guardar cambios" : "Crear perfil",
                enviando: form.formState.isSubmitting,
                ...(conflicto ? { faltan: ["la versión nueva"], motivo: "Alguien más cambió los ajustes: vuelve a abrirlos." } : {}),
            }}
            error={
                error ? (
                    <Aviso tono="error" titulo={conflicto ? "Alguien más cambió los ajustes" : "No se pudo guardar"}>
                        {conflicto ? "Vuelve a abrir el perfil desde la lista para ver el cambio." : `${mensajeDeError(error).titulo}${error instanceof ErrorApi && error.detalle ? `: ${error.detalle}` : ""}`}
                    </Aviso>
                ) : null
            }
        >
            <SeccionFormulario titulo="Perfil">
                <Campo etiqueta="Nombre" maxLength={40} placeholder="Noche y fin de semana" autoComplete="off" error={errores.nombre?.message} {...form.register("nombre")} />
            </SeccionFormulario>
            <SeccionFormulario titulo="¿Qué puede hacer solo?">
                <AccionesAsistente registrar={(c) => form.register(`acciones.${c}`)} registrarSaldoConNumero={() => form.register("saldoConNumero")} alApagarSaldo={() => form.setValue("saldoConNumero", false, { shouldDirty: true })} saldoConNumero={saldoConNumero} daSaldo={daSaldo} />
            </SeccionFormulario>
            <SeccionFormulario titulo="Hasta dónde llega">
                <div className="grid gap-4 sm:grid-cols-2">
                    <CampoSelector etiqueta="Prioridad máxima de sus tickets" ayuda="Lo que pida más prioridad lo pasa a una persona." error={errores.prioridadMaxima?.message} {...form.register("prioridadMaxima")}>
                        {PRIORIDADES.map((p) => (
                            <option key={p} value={p}>
                                {NOMBRE_PRIORIDAD[p]}
                            </option>
                        ))}
                    </CampoSelector>
                    <CampoNumero etiqueta="Días adelante para agendar, como mucho" minimo={1} maximo={14} error={errores.diasAdelanteMax?.message} {...form.register("diasAdelanteMax")} />
                </div>
            </SeccionFormulario>
        </FormularioPagina>
    );
}
