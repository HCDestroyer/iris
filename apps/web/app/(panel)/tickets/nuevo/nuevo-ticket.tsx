"use client";

import { zodResolver } from "@hookform/resolvers/zod";
import { avisar, Aviso, Campo, CampoArea, CampoSelector, FormularioPagina, liberarProteccion, SeccionFormulario } from "@iris/ui";
import { useRouter } from "next/navigation";
import { useEffect, useRef, useState, useSyncExternalStore } from "react";
import { useForm } from "react-hook-form";
import { ErrorAlta, errorEnCliente } from "@/componentes/clientes/error-alta";
import { SelectorCliente, type SeleccionCliente } from "@/componentes/clientes/selector-cliente";
import { useCategorias } from "@/componentes/tickets/catalogos";
import { api, claveTrasError, nuevaClaveIdempotencia, resolver } from "@/lib/api/cliente";
import { vincularTicketCentralita } from "@/lib/centralita/api";
import { erroresACampos } from "@/lib/formularios";
import { useSesion } from "@/lib/sesion";
import { entradaCrearTicket, esquemaCrearTicket, type DatosCrearTicket, type EntradaFormCrear } from "@/lib/tickets/esquemas";
import { ORDEN_PRIORIDADES, PRIORIDADES } from "@/lib/tickets/presentacion";

const sinSuscripcion = () => () => undefined;
/** `?llamada=<uuid>`: la llamada de la centralita desde la que se crea el ticket. */
function llamadaDeLaUrl(): string | null {
    const id = new URLSearchParams(window.location.search).get("llamada");
    return id && /^[0-9a-f-]{36}$/i.test(id) ? id : null;
}

const VACIO: EntradaFormCrear = {
    asunto: "",
    categoriaId: "",
    prioridad: "media",
    descripcion: "",
    contacto: { nombre: "", telefono: "", correo: "" },
};

/**
 * «Nuevo ticket» en pantalla propia (`/tickets/nuevo`, permiso `tickets.crear`): asunto,
 * categoría, prioridad, descripción y cliente de Argos o contacto. Al crear, aviso y ficha.
 */
export function NuevoTicket() {
    const { puede } = useSesion();
    const router = useRouter();
    const categorias = useCategorias();
    const listaCategorias = categorias.estado === "listo" ? categorias.datos.filter((c) => c.activa) : [];
    const [seleccion, setSeleccion] = useState<SeleccionCliente | null>(null);
    const cliente = seleccion?.cliente ?? null;
    const [apagado, setApagado] = useState(false);
    // Búsqueda sin elegir o «Cambiar cliente» abierto: no se manda ni el anterior ni ninguno.
    const [clienteAMedias, setClienteAMedias] = useState<string | null>(null);
    const [errorGeneral, setErrorGeneral] = useState<unknown>(null);
    const clave = useRef(nuevaClaveIdempotencia());
    const form = useForm<EntradaFormCrear, unknown, DatosCrearTicket>({ resolver: zodResolver(esquemaCrearTicket), defaultValues: VACIO });
    const errores = form.formState.errors;
    const buscarCliente = puede(["clientes.ver"]);
    // Desde el aviso de una llamada de la centralita (E4a): `?llamada=<id>&telefono=`. Al crear, la
    // llamada se agrega al ticket (a elección de la persona: nunca automático).
    const llamada = useSyncExternalStore(sinSuscripcion, llamadaDeLaUrl, () => null);
    useEffect(() => {
        const q = new URLSearchParams(window.location.search);
        const tel = q.get("telefono");
        if (tel && /^\+[0-9]{8,15}$/.test(tel) && !form.getValues("contacto.telefono")) form.setValue("contacto.telefono", tel);
    }, [form]);

    async function crear(datos: DatosCrearTicket) {
        if (clienteAMedias) return;
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
            avisar({ titulo: `Ticket ${ticket.numero} creado` });
            if (llamada) {
                try {
                    await vincularTicketCentralita(llamada, ticket.id);
                } catch {
                    avisar({ tono: "warning", titulo: "El ticket se creó, pero la llamada no se pudo agregar", descripcion: "Agrégala desde el aviso o la ficha del cliente." });
                }
            }
            await liberarProteccion();
            router.replace(`/tickets/${ticket.id}`);
        } catch (error) {
            clave.current = claveTrasError(error, clave.current);
            const campos = ["asunto", "categoriaId", "prioridad", "descripcion", "contacto.nombre", "contacto.telefono", "contacto.correo"] as const;
            if (!erroresACampos(error, campos, form.setError)) {
                setErrorGeneral(error);
            }
        }
    }

    return (
        <FormularioPagina
            titulo="Nuevo ticket"
            descripcion="Alta rápida: lo demás se completa en el detalle."
            alEnviar={(e) => void form.handleSubmit(crear)(e)}
            navegar={router.push}
            volverA="/tickets"
            sucio={form.formState.isDirty || seleccion !== null}
            barra={{
                textoGuardar: "Crear ticket",
                textoEnviando: "Creando…",
                enviando: form.formState.isSubmitting,
                faltan: clienteAMedias ? ["el cliente"] : [],
                ...(clienteAMedias ? { motivo: clienteAMedias } : {}),
            }}
            error={errorGeneral ? <ErrorAlta error={errorGeneral} que="el ticket" /> : null}
        >
            {llamada ? (
                <Aviso tono="info" titulo="Desde una llamada de la centralita" data-slot="ticket-desde-llamada">
                    Al crear el ticket, la llamada queda agregada a él.
                </Aviso>
            ) : null}
            <SeccionFormulario titulo="1. El problema">
                <Campo etiqueta="Asunto" autoFocus maxLength={200} error={errores.asunto?.message} {...form.register("asunto")} />
                <div className="grid gap-4 sm:grid-cols-2">
                    <CampoSelector etiqueta="Categoría" error={errores.categoriaId?.message} {...form.register("categoriaId")}>
                        <option value="">Sin categoría</option>
                        {listaCategorias.map((c) => (
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
                <CampoArea etiqueta="Descripción" rows={4} maxLength={20000} ayuda="Primer mensaje público del ticket." error={errores.descripcion?.message} {...form.register("descripcion")} />
            </SeccionFormulario>
            <SeccionFormulario titulo="2. Cliente">
                {buscarCliente ? (
                    <SelectorCliente
                        etiqueta="Cliente en Argos"
                        valor={seleccion}
                        alCambiar={(v) => {
                            setSeleccion(v);
                            setErrorGeneral(null);
                        }}
                        alConectorApagado={setApagado}
                        alBuscarSinElegir={setClienteAMedias}
                        error={errorEnCliente(errorGeneral)}
                        ayuda={cliente ? undefined : "Opcional: si no es cliente, captura el contacto abajo."}
                    />
                ) : null}
                {cliente ? null : (
                    <fieldset className="grid gap-3 rounded-md border p-3">
                        <legend className="px-1 text-sm font-medium">{apagado || !buscarCliente ? "Datos de contacto" : "Contacto (si no es cliente de Argos)"}</legend>
                        <Campo etiqueta="Nombre" maxLength={120} autoComplete="off" error={errores.contacto?.nombre?.message} {...form.register("contacto.nombre")} />
                        <div className="grid gap-3 sm:grid-cols-2">
                            <Campo etiqueta="Teléfono" type="tel" maxLength={40} autoComplete="off" error={errores.contacto?.telefono?.message} {...form.register("contacto.telefono")} />
                            <Campo etiqueta="Correo" type="email" maxLength={254} autoComplete="off" error={errores.contacto?.correo?.message} {...form.register("contacto.correo")} />
                        </div>
                    </fieldset>
                )}
            </SeccionFormulario>
        </FormularioPagina>
    );
}
