"use client";

import { zodResolver } from "@hookform/resolvers/zod";
import { BotonEnviar, Boton, CampoSelector, DialogoConfirmar, ErrorFormulario, Tarjeta, TarjetaContenido, TarjetaEncabezado, TarjetaTitulo } from "@iris/ui";
import { Building2, X } from "lucide-react";
import { useState } from "react";
import { useForm, useWatch } from "react-hook-form";
import { api, resolver } from "@/lib/api/cliente";
import { ErrorApi, mensajeDeError } from "@/lib/api/errores";
import { useClaveIdempotencia } from "@/lib/idempotencia";
import { useSesion } from "@/lib/sesion";
import { cuerpoEmpresaUsuario, esquemaAsignarEmpresa, nombreEmpresa, type DatosAsignarEmpresa, type Usuario } from "@/lib/usuarios/esquemas";

/**
 * Empresas del usuario (`usuario.empresas`) con «Asignar» (`POST /usuarios/{id}/asignar-empresa`,
 * `usuarios.asignar_empresa`) y «Quitar» (`/quitar-empresa`, `usuarios.quitar_empresa`). Solo se
 * ofrecen las empresas en que opera quien administra (las de `GET /yo`).
 */
export function EmpresasUsuario({ usuario, alCambiar }: { usuario: Usuario; alCambiar: (u: Usuario) => void }) {
    const { yo, puede, empresas: propias } = useSesion();
    const puedeAsignar = puede(["usuarios.asignar_empresa"]);
    const puedeQuitar = puede(["usuarios.quitar_empresa"]);
    const asignadas = usuario.empresas ?? [];
    const [quitar, setQuitar] = useState<number | null>(null);
    const [error, setError] = useState<unknown>(null);
    const claveQuitar = useClaveIdempotencia();
    // Sin conector (sin empresas registradas) no hay nada que mostrar.
    if (yo.modoEmpresa === "sin_conector" && asignadas.length === 0) return null;
    const libres = propias.filter((e) => !asignadas.includes(e.id));

    async function confirmarQuitar(id: number) {
        setError(null);
        try {
            const u = await resolver(
                api().POST("/api/v1/usuarios/{id}/quitar-empresa", {
                    params: { path: { id: usuario.id }, header: { "Idempotency-Key": claveQuitar.actual() } },
                    body: cuerpoEmpresaUsuario(id),
                }),
            );
            claveQuitar.renovar();
            setQuitar(null);
            alCambiar(u);
        } catch (e) {
            claveQuitar.trasError(e);
            setError(e);
        }
    }

    return (
        <Tarjeta data-seccion="empresas">
            <TarjetaEncabezado>
                <TarjetaTitulo>Empresas</TarjetaTitulo>
            </TarjetaEncabezado>
            <TarjetaContenido className="grid gap-4">
                {asignadas.length === 0 ? (
                    <p className="text-sm text-muted-foreground">Sin empresas: solo un administrador global puede administrarlo, y no opera en ninguna empresa salvo que su rol tenga todas.</p>
                ) : (
                    <ul className="grid gap-2" aria-label="Empresas asignadas">
                        {asignadas.map((id) => (
                            <li key={id} className="flex flex-wrap items-center justify-between gap-2 rounded-md border border-border p-2" data-empresa={id}>
                                <span className="inline-flex min-w-0 items-center gap-2 text-sm">
                                    <Building2 aria-hidden className="size-4 shrink-0 text-muted-foreground" />
                                    <span className="truncate font-medium">{nombreEmpresa(id, propias)}</span>
                                </span>
                                {puedeQuitar && propias.some((e) => e.id === id) ? (
                                    <Boton variante="fantasma" tamano="sm" onClick={() => setQuitar(id)} aria-label={`Quitar ${nombreEmpresa(id, propias)}`}>
                                        <X />
                                        Quitar
                                    </Boton>
                                ) : null}
                            </li>
                        ))}
                    </ul>
                )}
                {puedeAsignar && usuario.activo ? <FormularioAsignar usuarioId={usuario.id} libres={libres} alAsignar={alCambiar} /> : null}
            </TarjetaContenido>
            <DialogoConfirmar
                open={quitar !== null}
                onOpenChange={(a) => {
                    if (!a) {
                        setQuitar(null);
                        setError(null);
                    }
                }}
                peligro
                titulo={quitar !== null ? `¿Quitar ${nombreEmpresa(quitar, propias)} a ${usuario.nombre}?` : ""}
                descripcion="Deja de ver y operar los datos de esa empresa (sus tokens también). Puedes volver a asignarla después."
                textoConfirmar="Quitar"
                textoOcupado="Quitando…"
                alConfirmar={() => (quitar !== null ? confirmarQuitar(quitar) : undefined)}
            >
                {error ? <AvisoEmpresa error={error} que="quitar la empresa" /> : null}
            </DialogoConfirmar>
        </Tarjeta>
    );
}

function FormularioAsignar({ usuarioId, libres, alAsignar }: { usuarioId: number; libres: readonly { id: number; nombre: string }[]; alAsignar: (u: Usuario) => void }) {
    const form = useForm<DatosAsignarEmpresa>({ resolver: zodResolver(esquemaAsignarEmpresa), defaultValues: { empresaId: "" } });
    const empresaId = useWatch({ control: form.control, name: "empresaId" });
    const [error, setError] = useState<unknown>(null);
    const clave = useClaveIdempotencia();

    async function enviar(d: DatosAsignarEmpresa) {
        setError(null);
        try {
            const u = await resolver(
                api().POST("/api/v1/usuarios/{id}/asignar-empresa", {
                    params: { path: { id: usuarioId }, header: { "Idempotency-Key": clave.actual() } },
                    body: cuerpoEmpresaUsuario(d.empresaId),
                }),
            );
            clave.renovar();
            form.reset({ empresaId: "" });
            alAsignar(u);
        } catch (e) {
            clave.trasError(e);
            setError(e);
        }
    }

    return (
        <form className="grid gap-3 border-t border-border pt-4" noValidate aria-label="Asignar empresa" onSubmit={(e) => void form.handleSubmit(enviar)(e)}>
            <CampoSelector
                etiqueta="Empresa"
                disabled={libres.length === 0}
                ayuda={libres.length === 0 ? "Ya tiene todas las empresas en que operas." : "Solo las empresas en que operas tú."}
                error={form.formState.errors.empresaId?.message}
                {...form.register("empresaId")}
            >
                <option value="">Elige la empresa…</option>
                {libres.map((e) => (
                    <option key={e.id} value={String(e.id)}>
                        {e.nombre}
                    </option>
                ))}
            </CampoSelector>
            {error ? <AvisoEmpresa error={error} que="asignar la empresa" /> : null}
            <div className="flex flex-wrap items-center gap-3">
                <BotonEnviar faltan={empresaId ? [] : ["la empresa"]} enviando={form.formState.isSubmitting} textoEnviando="Asignando…" disabled={libres.length === 0}>
                    <Building2 />
                    Asignar empresa
                </BotonEnviar>
            </div>
        </form>
    );
}

function AvisoEmpresa({ error, que }: { error: unknown; que: string }) {
    const codigo = error instanceof ErrorApi ? error.codigo : undefined;
    const estado = error instanceof ErrorApi ? error.estado : undefined;
    if (codigo === "empresa_ya_asignada") return <ErrorFormulario titulo="Ya tiene esa empresa" detalle="Recarga la página para ver sus empresas actuales." />;
    if (codigo === "empresa_no_asignada") return <ErrorFormulario titulo="Ya no tiene esa empresa" detalle="Recarga la página para ver sus empresas actuales." />;
    if (estado === 403) {
        return <ErrorFormulario titulo={`No puedes ${que}`} detalle={mensajeDeError(error).detalle ?? "Tiene permisos o empresas que tú no administras. Pídeselo a un administrador global."} />;
    }
    if (estado === 404) return <ErrorFormulario titulo="Este usuario ya no está en tu alcance" detalle="No se cambió nada." />;
    const { titulo, detalle } = mensajeDeError(error);
    return <ErrorFormulario titulo={`No se pudo ${que}: ${titulo}`} detalle={detalle} />;
}
