"use client";

import { zodResolver } from "@hookform/resolvers/zod";
import { Aviso, Boton, Campo, CampoArea, CampoSelector, Dialogo, DialogoCerrar, DialogoContenido, DialogoDisparador, DialogoPie } from "@iris/ui";
import { ClipboardPlus, Plus } from "lucide-react";
import { useRouter } from "next/navigation";
import { useRef, useState } from "react";
import { useForm, useWatch } from "react-hook-form";
import { BuscadorCliente, type ClienteResumen } from "@/componentes/tickets/buscador-cliente";
import { api, claveTrasError, nuevaClaveIdempotencia, resolver } from "@/lib/api/cliente";
import { mensajeDeError } from "@/lib/api/errores";
import { erroresACampos } from "@/lib/formularios";
import { entradaCrearOrden, esquemaCrearOrden, type DatosCrearOrden, type EntradaFormCrearOrden } from "@/lib/ordenes/esquemas";
import { ORDEN_TIPOS, TIPOS_ORDEN } from "@/lib/ordenes/presentacion";
import { useSesion } from "@/lib/sesion";
import { useTiposOrden } from "./catalogos";

type TicketOrigen = { id: number; numero: string; asunto: string };

const CAMPOS = ["tipo", "plantillaId", "descripcion", "direccion", "zonaId", "lat", "lon", "contacto.nombre", "contacto.telefono"] as const;

function vacio(ticket: TicketOrigen | undefined): EntradaFormCrearOrden {
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
 * Alta de orden en un diálogo: desde cero o desde un ticket (hereda empresa, cliente, servicio,
 * zona, cuadrilla y contacto, y el ticket recibe una nota interna). Al crear, abre el detalle.
 */
export function DialogoCrearOrden({ ticket }: { ticket?: TicketOrigen }) {
    const { puede } = useSesion();
    const router = useRouter();
    const tipos = useTiposOrden();
    const [abierto, setAbierto] = useState(false);
    const [cliente, setCliente] = useState<ClienteResumen | null>(null);
    const [, setApagado] = useState(false);
    const [errorGeneral, setErrorGeneral] = useState<unknown>(null);
    const clave = useRef(nuevaClaveIdempotencia());
    const form = useForm<EntradaFormCrearOrden, unknown, DatosCrearOrden>({ resolver: zodResolver(esquemaCrearOrden), defaultValues: vacio(ticket) });
    const errores = form.formState.errors;
    const tipoElegido = useWatch({ control: form.control, name: "tipo" });
    const plantillas = tipos.estado === "listo" ? (tipos.datos.find((t) => t.codigo === tipoElegido)?.plantillas ?? []) : [];
    const buscarCliente = !ticket && puede(["clientes.ver"]);

    function reiniciar() {
        form.reset(vacio(ticket));
        setCliente(null);
        setErrorGeneral(null);
        clave.current = nuevaClaveIdempotencia();
    }

    async function crear(datos: DatosCrearOrden) {
        setErrorGeneral(null);
        try {
            const cuerpo = entradaCrearOrden({ ...datos, ...(cliente ? { clienteId: cliente.id } : {}) });
            const orden = await resolver(api().POST("/api/v1/ordenes", { body: cuerpo, params: { header: { "Idempotency-Key": clave.current } } }));
            reiniciar();
            setAbierto(false);
            router.push(`/ordenes/${orden.id}`);
        } catch (error) {
            clave.current = claveTrasError(error, clave.current);
            if (!erroresACampos(error, CAMPOS, form.setError)) {
                setErrorGeneral(error);
            }
        }
    }

    return (
        <Dialogo
            open={abierto}
            onOpenChange={(a) => {
                setAbierto(a);
                if (!a) reiniciar();
            }}
        >
            <DialogoDisparador asChild>
                {ticket ? (
                    <Boton variante="secundario" tamano="sm">
                        <ClipboardPlus />
                        Crear orden
                    </Boton>
                ) : (
                    <Boton>
                        <Plus />
                        Nueva orden
                    </Boton>
                )}
            </DialogoDisparador>
            <DialogoContenido
                titulo={ticket ? "Crear orden desde el ticket" : "Nueva orden"}
                descripcion={ticket ? `${ticket.numero} · ${ticket.asunto}` : "Se crea pendiente; después se asigna y programa."}
            >
                <form
                    noValidate
                    className="grid gap-4"
                    onSubmit={(e) => {
                        // Con cliente elegido la dirección sale de la instantánea de Argos.
                        form.setValue("clienteId", cliente?.id);
                        void form.handleSubmit(crear)(e);
                    }}
                >
                    {ticket ? (
                        <Aviso tono="info" titulo="Hereda los datos del ticket">
                            La orden toma el cliente, el servicio, la zona y el contacto del ticket, y el ticket recibe una nota interna.
                        </Aviso>
                    ) : null}
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
                    <CampoArea etiqueta="Trabajo a realizar" rows={3} maxLength={4000} error={errores.descripcion?.message} {...form.register("descripcion")} />
                    {buscarCliente ? <BuscadorCliente elegido={cliente} alElegir={setCliente} alConectorApagado={setApagado} /> : null}
                    <Campo
                        etiqueta={ticket || cliente ? "Dirección (opcional)" : "Dirección"}
                        ayuda={ticket || cliente ? "Vacía: la del servicio o el cliente en Argos." : undefined}
                        maxLength={300}
                        autoComplete="off"
                        error={errores.direccion?.message}
                        {...form.register("direccion")}
                    />
                    {ticket ? null : (
                        <>
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
                            <fieldset className="grid gap-3 rounded-md border p-3">
                                <legend className="px-1 text-sm font-medium">Contacto en sitio</legend>
                                <div className="grid gap-3 sm:grid-cols-2">
                                    <Campo etiqueta="Nombre" maxLength={120} autoComplete="off" error={errores.contacto?.nombre?.message} {...form.register("contacto.nombre")} />
                                    <Campo etiqueta="Teléfono" type="tel" maxLength={40} autoComplete="off" error={errores.contacto?.telefono?.message} {...form.register("contacto.telefono")} />
                                </div>
                            </fieldset>
                        </>
                    )}
                    {errorGeneral ? (
                        <Aviso tono="error" titulo="No se pudo crear la orden">
                            {[mensajeDeError(errorGeneral).titulo, mensajeDeError(errorGeneral).detalle].filter(Boolean).join(". ")}
                        </Aviso>
                    ) : null}
                    <DialogoPie>
                        <DialogoCerrar asChild>
                            <Boton variante="contorno">Cancelar</Boton>
                        </DialogoCerrar>
                        <Boton type="submit" disabled={form.formState.isSubmitting}>
                            {form.formState.isSubmitting ? "Creando…" : "Crear orden"}
                        </Boton>
                    </DialogoPie>
                </form>
            </DialogoContenido>
        </Dialogo>
    );
}
