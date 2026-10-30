"use client";

import type { Schema } from "@iris/contratos";
import { Boton, Campo, CampoSelector, Tarjeta, TarjetaContenido } from "@iris/ui";
import { zodResolver } from "@hookform/resolvers/zod";
import { Eraser, Filter } from "lucide-react";
import { useSearchParams } from "next/navigation";
import { useState } from "react";
import { useForm } from "react-hook-form";
import { ListaAccesos } from "@/componentes/accesos/lista-accesos";
import { EncabezadoPagina } from "@/componentes/encabezado-pagina";
import { esquemaFiltrosAccesos, FILTROS_VACIOS, queryAccesos, type FiltrosAccesos } from "@/lib/accesos/esquemas";
import { CANALES, ORDEN_CANALES, ORDEN_RESULTADOS, RESULTADOS } from "@/lib/accesos/presentacion";
import { api, resolver } from "@/lib/api/cliente";
import { useLectura } from "@/lib/lectura";

/** Administración → Accesos: quién entró, cuándo, por qué canal y con qué resultado. */
export function HistorialAccesos() {
    const params = useSearchParams();
    const inicial: FiltrosAccesos = { ...FILTROS_VACIOS, usuarioId: /^\d+$/.test(params.get("usuarioId") ?? "") ? (params.get("usuarioId") ?? "") : "" };
    const [aplicados, setAplicados] = useState<FiltrosAccesos>(inicial);
    // Usuarios para el filtro (los primeros 200 que el actor puede ver); si falla, el filtro queda sin opciones.
    const usuarios = useLectura<Schema<"Usuario">[]>(async (signal) => (await resolver(api().GET("/api/v1/usuarios", { params: { query: { limit: 200 } }, signal }))).data ?? []);
    const formulario = useForm<FiltrosAccesos>({ resolver: zodResolver(esquemaFiltrosAccesos), defaultValues: inicial });
    const { errors } = formulario.formState;

    return (
        <>
            <EncabezadoPagina
                titulo="Accesos"
                descripcion="Inicios de sesión en el panel, la app de campo y desde Argos, y el uso de tokens. Los intentos con un correo que no existe aparecen sin usuario."
            />
            <Tarjeta className="mb-4">
                <TarjetaContenido className="pt-5">
                    <form
                        className="grid gap-3 sm:grid-cols-2 lg:grid-cols-5"
                        noValidate
                        aria-label="Filtros del historial"
                        onSubmit={(e) => void formulario.handleSubmit((d) => setAplicados(d))(e)}
                    >
                        <CampoSelector
                            etiqueta="Usuario"
                            {...formulario.register("usuarioId")}
                            disabled={usuarios.estado === "cargando"}
                            ayuda={usuarios.estado === "error" ? "No se pudo leer la lista de usuarios." : undefined}
                        >
                            <option value="">Todos</option>
                            {usuarios.estado === "listo"
                                ? usuarios.datos.map((u) => (
                                      <option key={u.id} value={String(u.id)}>
                                          {u.nombre}
                                      </option>
                                  ))
                                : null}
                            {/* El usuario que vino en el enlace, aunque no esté entre los primeros 200. */}
                            {inicial.usuarioId && !(usuarios.estado === "listo" && usuarios.datos.some((u) => String(u.id) === inicial.usuarioId)) ? (
                                <option value={inicial.usuarioId}>Usuario {inicial.usuarioId}</option>
                            ) : null}
                        </CampoSelector>
                        <CampoSelector etiqueta="Canal" {...formulario.register("canal")}>
                            <option value="">Todos</option>
                            {ORDEN_CANALES.map((c) => (
                                <option key={c} value={c}>
                                    {CANALES[c].etiqueta}
                                </option>
                            ))}
                        </CampoSelector>
                        <CampoSelector etiqueta="Resultado" {...formulario.register("resultado")}>
                            <option value="">Todos</option>
                            {ORDEN_RESULTADOS.map((r) => (
                                <option key={r} value={r}>
                                    {RESULTADOS[r].etiqueta}
                                </option>
                            ))}
                        </CampoSelector>
                        <Campo etiqueta="Desde" type="date" {...formulario.register("desde")} error={errors.desde?.message} />
                        <Campo etiqueta="Hasta" type="date" {...formulario.register("hasta")} error={errors.hasta?.message} />
                        <div className="flex flex-wrap gap-2 sm:col-span-2 lg:col-span-5">
                            <Boton type="submit">
                                <Filter />
                                Aplicar filtros
                            </Boton>
                            <Boton
                                type="button"
                                variante="secundario"
                                onClick={() => {
                                    formulario.reset(FILTROS_VACIOS);
                                    setAplicados(FILTROS_VACIOS);
                                }}
                            >
                                <Eraser />
                                Limpiar
                            </Boton>
                        </div>
                    </form>
                </TarjetaContenido>
            </Tarjeta>
            <ListaAccesos key={JSON.stringify(aplicados)} query={queryAccesos(aplicados)} />
        </>
    );
}
