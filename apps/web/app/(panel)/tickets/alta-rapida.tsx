"use client";

import { zodResolver } from "@hookform/resolvers/zod";
import {
    BotonEnviar,
    Boton,
    Campo,
    CampoArea,
    CampoSelector,
    DialogoCerrar,
    DialogoContenido,
    DialogoDisparador,
    DialogoPie,
    DialogoProtegido,
} from "@iris/ui";
import { Plus } from "lucide-react";
import { useRouter } from "next/navigation";
import { useRef, useState } from "react";
import { useForm } from "react-hook-form";
import { ErrorAlta, errorEnCliente } from "@/componentes/clientes/error-alta";
import { SelectorCliente, type SeleccionCliente } from "@/componentes/clientes/selector-cliente";
import type { Categoria } from "@/componentes/tickets/catalogos";
import { api, claveTrasError, nuevaClaveIdempotencia, resolver } from "@/lib/api/cliente";
import { erroresACampos } from "@/lib/formularios";
import { useSesion } from "@/lib/sesion";
import { entradaCrearTicket, esquemaCrearTicket, type DatosCrearTicket, type EntradaFormCrear } from "@/lib/tickets/esquemas";
import { ORDEN_PRIORIDADES, PRIORIDADES } from "@/lib/tickets/presentacion";

const VACIO: EntradaFormCrear = {
    asunto: "",
    categoriaId: "",
    prioridad: "media",
    descripcion: "",
    contacto: { nombre: "", telefono: "", correo: "" },
};

/** Alta rápida de ticket en un diálogo; al crear, abre el detalle. */
export function AltaRapida({ categorias }: { categorias: readonly Categoria[] }) {
    const { puede } = useSesion();
    const router = useRouter();
    const [abierto, setAbierto] = useState(false);
    const [seleccion, setSeleccion] = useState<SeleccionCliente | null>(null);
    const cliente = seleccion?.cliente ?? null;
    const [apagado, setApagado] = useState(false);
    const [errorGeneral, setErrorGeneral] = useState<unknown>(null);
    const clave = useRef(nuevaClaveIdempotencia());
    const form = useForm<EntradaFormCrear, unknown, DatosCrearTicket>({ resolver: zodResolver(esquemaCrearTicket), defaultValues: VACIO });
    const errores = form.formState.errors;
    const buscarCliente = puede(["clientes.ver"]);

    function reiniciar() {
        form.reset(VACIO);
        setSeleccion(null);
        setApagado(false);
        setErrorGeneral(null);
        clave.current = nuevaClaveIdempotencia();
    }

    async function crear(datos: DatosCrearTicket) {
        setErrorGeneral(null);
        try {
            const cuerpo = entradaCrearTicket({
                ...datos,
                ...(cliente ? { clienteId: cliente.id } : {}),
                ...(seleccion?.servicio ? { servicioId: seleccion.servicio.id } : {}),
            });
            if (cliente) {
                delete cuerpo.contacto;
            }
            const ticket = await resolver(api().POST("/api/v1/tickets", { body: cuerpo, params: { header: { "Idempotency-Key": clave.current } } }));
            reiniciar();
            setAbierto(false);
            router.push(`/tickets/${ticket.id}`);
        } catch (error) {
            clave.current = claveTrasError(error, clave.current);
            const campos = ["asunto", "categoriaId", "prioridad", "descripcion", "contacto.nombre", "contacto.telefono", "contacto.correo"] as const;
            if (!erroresACampos(error, campos, form.setError)) {
                setErrorGeneral(error);
            }
        }
    }

    return (
        <DialogoProtegido
            open={abierto}
            sucio={form.formState.isDirty || seleccion !== null}
            ocupado={form.formState.isSubmitting}
            onOpenChange={(a) => {
                setAbierto(a);
                if (!a) {
                    reiniciar();
                }
            }}
        >
            <DialogoDisparador asChild>
                <Boton>
                    <Plus />
                    Nuevo ticket
                </Boton>
            </DialogoDisparador>
            <DialogoContenido titulo="Nuevo ticket" descripcion="Alta rápida: lo demás se completa en el detalle.">
                <form noValidate className="grid gap-4" onSubmit={(e) => void form.handleSubmit(crear)(e)}>
                    <Campo etiqueta="Asunto" autoFocus maxLength={200} error={errores.asunto?.message} {...form.register("asunto")} />
                    <div className="grid gap-4 sm:grid-cols-2">
                        <CampoSelector etiqueta="Categoría" error={errores.categoriaId?.message} {...form.register("categoriaId")}>
                            <option value="">Sin categoría</option>
                            {categorias.map((c) => (
                                <option key={c.id} value={c.id}>
                                    {c.nombre}
                                </option>
                            ))}
                        </CampoSelector>
                        <CampoSelector etiqueta="Prioridad" error={errores.prioridad?.message} {...form.register("prioridad")}>
                            {[...ORDEN_PRIORIDADES].reverse().map((p) => (
                                <option key={p} value={p}>
                                    {PRIORIDADES[p].texto}
                                </option>
                            ))}
                        </CampoSelector>
                    </div>
                    <CampoArea etiqueta="Descripción" rows={3} maxLength={20000} ayuda="Primer mensaje público del ticket." error={errores.descripcion?.message} {...form.register("descripcion")} />
                    {buscarCliente ? (
                        <SelectorCliente
                            etiqueta="Cliente en Argos"
                            valor={seleccion}
                            alCambiar={(v) => {
                                setSeleccion(v);
                                setErrorGeneral(null);
                            }}
                            alConectorApagado={setApagado}
                            error={errorEnCliente(errorGeneral)}
                            ayuda={cliente ? undefined : "Opcional: si no es cliente, captura el contacto abajo."}
                        />
                    ) : null}
                    {cliente ? null : (
                        <fieldset className="grid gap-3 rounded-md border p-3">
                            <legend className="px-1 text-sm font-medium">
                                {apagado || !buscarCliente ? "Datos de contacto" : "Contacto (si no es cliente de Argos)"}
                            </legend>
                            <Campo etiqueta="Nombre" maxLength={120} autoComplete="off" error={errores.contacto?.nombre?.message} {...form.register("contacto.nombre")} />
                            <div className="grid gap-3 sm:grid-cols-2">
                                <Campo etiqueta="Teléfono" type="tel" maxLength={40} autoComplete="off" error={errores.contacto?.telefono?.message} {...form.register("contacto.telefono")} />
                                <Campo etiqueta="Correo" type="email" maxLength={254} autoComplete="off" error={errores.contacto?.correo?.message} {...form.register("contacto.correo")} />
                            </div>
                        </fieldset>
                    )}
                    {errorGeneral ? <ErrorAlta error={errorGeneral} que="el ticket" /> : null}
                    <DialogoPie>
                        <DialogoCerrar asChild>
                            <Boton variante="contorno">Cancelar</Boton>
                        </DialogoCerrar>
                        <BotonEnviar enviando={form.formState.isSubmitting} textoEnviando="Creando…">
                            Crear ticket
                        </BotonEnviar>
                    </DialogoPie>
                </form>
            </DialogoContenido>
        </DialogoProtegido>
    );
}
