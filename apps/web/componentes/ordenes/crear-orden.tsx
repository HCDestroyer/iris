"use client";

import { zodResolver } from "@hookform/resolvers/zod";
import type { Schema } from "@iris/contratos";
import { avisar, Aviso, Campo, CampoArea, CampoSelector, FormularioPagina, liberarProteccion, SeccionFormulario } from "@iris/ui";
import { useRouter } from "next/navigation";
import { useRef, useState } from "react";
import { useForm, useWatch } from "react-hook-form";
import { ErrorAlta, errorEnCliente } from "@/componentes/clientes/error-alta";
import { SelectorCliente, type SeleccionCliente } from "@/componentes/clientes/selector-cliente";
import { api, claveTrasError, nuevaClaveIdempotencia, resolver } from "@/lib/api/cliente";
import { erroresACampos } from "@/lib/formularios";
import { entradaCrearOrden, esquemaCrearOrden, type DatosCrearOrden, type EntradaFormCrearOrden } from "@/lib/ordenes/esquemas";
import { ORDEN_TIPOS, TIPOS_ORDEN } from "@/lib/ordenes/presentacion";
import { useSesion } from "@/lib/sesion";
import { useTiposOrden } from "./catalogos";
import { TicketOrigen } from "./ticket-origen";

type Ticket = Schema<"Ticket">;

const CAMPOS = ["tipo", "plantillaId", "descripcion", "direccion", "zonaId", "lat", "lon", "contacto.nombre", "contacto.telefono"] as const;

function vacio(ticket: Ticket | undefined): EntradaFormCrearOrden {
    return {
        tipo: ticket ? "visita" : "instalacion",
        plantillaId: "",
        descripcion: ticket ? ticket.asunto : "",
        direccion: "",
        zonaId: "",
        lat: "",
        lon: "",
        contacto: { nombre: "", telefono: "" },
        ...(ticket ? { ticketId: ticket.id } : {}),
    };
}

/**
 * «Nueva orden» en pantalla propia (`/ordenes/nueva`): desde cero o desde un ticket (hereda
 * empresa, cliente, servicio, zona, cuadrilla y contacto, y el ticket recibe una nota interna).
 * Al crear, aviso y ficha de la orden; «Cancelar» vuelve a la lista o al ticket.
 */
export function FormularioCrearOrden({ ticket }: { ticket?: Ticket }) {
    const { puede } = useSesion();
    const router = useRouter();
    const tipos = useTiposOrden();
    const [seleccion, setSeleccion] = useState<SeleccionCliente | null>(null);
    const cliente = seleccion?.cliente ?? null;
    const [, setApagado] = useState(false);
    const [errorGeneral, setErrorGeneral] = useState<unknown>(null);
    const clave = useRef(nuevaClaveIdempotencia());
    const form = useForm<EntradaFormCrearOrden, unknown, DatosCrearOrden>({ resolver: zodResolver(esquemaCrearOrden), defaultValues: vacio(ticket) });
    const errores = form.formState.errors;
    const tipoElegido = useWatch({ control: form.control, name: "tipo" });
    const direccion = useWatch({ control: form.control, name: "direccion" });
    // Sin ticket ni cliente hace falta la dirección: se avisa junto al botón y al campo.
    const falta = !ticket && !cliente && !direccion.trim();
    const plantillas = tipos.estado === "listo" ? (tipos.datos.find((t) => t.codigo === tipoElegido)?.plantillas ?? []) : [];
    const buscarCliente = !ticket && puede(["clientes.ver"]);

    async function crear(datos: DatosCrearOrden) {
        setErrorGeneral(null);
        try {
            const cuerpo = entradaCrearOrden({
                ...datos,
                ...(cliente ? { clienteId: cliente.id } : {}),
                ...(seleccion?.servicio ? { servicioId: seleccion.servicio.id } : {}),
            });
            const orden = await resolver(api().POST("/api/v1/ordenes", { body: cuerpo, params: { header: { "Idempotency-Key": clave.current } } }));
            avisar({ titulo: `Orden ${orden.numero} creada` });
            await liberarProteccion();
            router.replace(`/ordenes/${orden.id}`);
        } catch (error) {
            clave.current = claveTrasError(error, clave.current);
            if (!erroresACampos(error, CAMPOS, form.setError)) {
                setErrorGeneral(error);
            }
        }
    }

    return (
        <FormularioPagina
            titulo={ticket ? "Crear orden desde el ticket" : "Nueva orden"}
            descripcion={ticket ? `${ticket.numero} · ${ticket.asunto}` : "Se crea pendiente; después se asigna y programa."}
            alEnviar={(e) => {
                // Con cliente elegido la dirección sale de la instantánea de Argos.
                form.setValue("clienteId", cliente?.id);
                void form.handleSubmit(crear)(e);
            }}
            navegar={router.push}
            volverA={ticket ? `/tickets/${ticket.id}` : "/ordenes"}
            sucio={form.formState.isDirty || seleccion !== null}
            barra={{
                textoGuardar: "Crear orden",
                textoEnviando: "Creando…",
                enviando: form.formState.isSubmitting,
                faltan: falta ? ["el cliente"] : [],
                motivo: "Elige el cliente o escribe la dirección para crear la orden.",
            }}
            error={errorGeneral ? <ErrorAlta error={errorGeneral} que="la orden" /> : null}
        >
            {ticket ? (
                <Aviso tono="info" titulo="Hereda los datos del ticket">
                    La orden toma el cliente, el servicio, la zona y el contacto del ticket, y el ticket recibe una nota interna.
                </Aviso>
            ) : null}
            <SeccionFormulario titulo={ticket ? "1. Ticket y trabajo" : "1. Trabajo"}>
                {ticket ? <TicketOrigen ticket={ticket} /> : null}
                <div className="grid gap-4 sm:grid-cols-2">
                    <CampoSelector etiqueta="Tipo" autoFocus error={errores.tipo?.message} {...form.register("tipo", { onChange: () => form.setValue("plantillaId", "") })}>
                        {ORDEN_TIPOS.map((t) => (
                            <option key={t} value={t}>
                                {TIPOS_ORDEN[t].texto}
                            </option>
                        ))}
                    </CampoSelector>
                    <CampoSelector
                        etiqueta="Checklist"
                        disabled={plantillas.length < 2}
                        ayuda={tipos.estado === "error" ? "No se pudieron cargar las plantillas: se usará la del tipo." : undefined}
                        error={errores.plantillaId?.message}
                        {...form.register("plantillaId")}
                    >
                        <option value="">La del tipo</option>
                        {plantillas.map((p) => (
                            <option key={p.id} value={p.id}>
                                {p.nombre}
                            </option>
                        ))}
                    </CampoSelector>
                </div>
                <CampoArea etiqueta="Trabajo a realizar" rows={4} maxLength={4000} error={errores.descripcion?.message} {...form.register("descripcion")} />
            </SeccionFormulario>
            <SeccionFormulario titulo={ticket ? "2. Lugar" : "2. Cliente y lugar"}>
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
                        ayuda={cliente ? "La dirección y la zona salen del cliente si las dejas vacías." : "Elige el cliente o escribe la dirección de la visita."}
                    />
                ) : null}
                <Campo
                    etiqueta={ticket || cliente ? "Dirección (opcional)" : "Dirección"}
                    ayuda={ticket || cliente ? "Vacía: la del servicio o el cliente en Argos." : "Obligatoria si no eliges un cliente."}
                    maxLength={300}
                    autoComplete="off"
                    error={errores.direccion?.message}
                    {...form.register("direccion")}
                />
                {ticket ? null : (
                    <div className="grid grid-cols-2 gap-3 sm:grid-cols-3">
                        <Campo etiqueta="Latitud" inputMode="decimal" placeholder="14.634915" maxLength={20} error={errores.lat?.message} {...form.register("lat")} />
                        <Campo etiqueta="Longitud" inputMode="decimal" placeholder="-90.506882" maxLength={20} error={errores.lon?.message} {...form.register("lon")} />
                        <Campo
                            etiqueta="Zona"
                            className="col-span-2 sm:col-span-1"
                            inputMode="numeric"
                            placeholder="Id de zona"
                            ayuda={cliente ? "Vacía: la del cliente." : undefined}
                            maxLength={12}
                            error={errores.zonaId?.message}
                            {...form.register("zonaId")}
                        />
                    </div>
                )}
            </SeccionFormulario>
            {ticket ? null : (
                <SeccionFormulario titulo="3. Contacto en sitio">
                    <fieldset className="grid gap-3">
                        <legend className="sr-only">Contacto en sitio</legend>
                        <div className="grid gap-3 sm:grid-cols-2">
                            <Campo etiqueta="Nombre" maxLength={120} autoComplete="off" error={errores.contacto?.nombre?.message} {...form.register("contacto.nombre")} />
                            <Campo etiqueta="Teléfono" type="tel" maxLength={40} autoComplete="off" error={errores.contacto?.telefono?.message} {...form.register("contacto.telefono")} />
                        </div>
                    </fieldset>
                </SeccionFormulario>
            )}
        </FormularioPagina>
    );
}
