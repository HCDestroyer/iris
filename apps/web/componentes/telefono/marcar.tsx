"use client";

import { Boton, Dialogo, DialogoContenido, Entrada, MenuDesplegable, MenuDesplegableContenido, MenuDesplegableDisparador, MenuDesplegableElemento, MenuDesplegableEtiqueta, TecladoTelefono } from "@iris/ui";
import { ChevronDown, Delete, Phone, Smartphone, UserRound } from "lucide-react";
import { useState } from "react";
import { clasificarMarcado, nacionalLegible } from "@/lib/telefono/presentacion";
import type { TipoTelefonoFicha } from "@/lib/telefono/tipos";
import { puedeLlamar, useTelefono, type ContextoTelefono, type OpcionLlamar } from "./proveedor";

/*
 * Llamar con tu extensión (maqueta E4c, pantallas 12–14): desde una ficha (el número lo pone el
 * servidor: el de Argos) o con el teclado (solo Guatemala y extensiones del equipo).
 */

/** El teclado libre. Un solo campo: va en un diálogo. */
export function DialogoMarcar({ t }: { t: ContextoTelefono }) {
    const [marcado, setMarcado] = useState("");
    const [llamando, setLlamando] = useState(false);
    const c = clasificarMarcado(marcado);
    const cerrar = () => t.abrir(null);
    const ext = t.estado.extension;
    const legible = c.tipo === "externo" ? nacionalLegible(c.nacional) : marcado;
    async function llamar() {
        if (c.tipo !== "externo" && c.tipo !== "interno") return;
        setLlamando(true);
        const numero = c.tipo === "externo" ? c.nacional : c.numero;
        await t.llamar({ clave: "teclado", etiqueta: numero, entrada: { numero }, nombre: c.tipo === "externo" ? `+502 ${nacionalLegible(c.nacional)}` : `Extensión ${c.numero}`, numero });
        setLlamando(false);
        cerrar();
    }
    return (
        <Dialogo open onOpenChange={(a) => !a && cerrar()}>
            <DialogoContenido titulo="Marcar" descripcion={`Desde tu extensión${ext ? ` ${ext}` : ""} · números de Guatemala y extensiones del equipo`} centrado>
                <form
                    className="grid gap-3"
                    onSubmit={(e) => {
                        e.preventDefault();
                        void llamar();
                    }}
                >
                    <label className="sr-only" htmlFor="numero-marcar">
                        Número
                    </label>
                    <Entrada
                        id="numero-marcar"
                        inputMode="tel"
                        autoComplete="off"
                        value={legible}
                        maxLength={20}
                        onChange={(e) => setMarcado(e.target.value.replace(/[^0-9+\s-]/g, ""))}
                        className="h-12 border-0 bg-transparent text-center font-mono text-2xl font-semibold tracking-[0.06em] tabular-nums shadow-none"
                        aria-describedby="ayuda-marcar"
                    />
                    <p id="ayuda-marcar" className="min-h-5 text-center text-xs text-muted-foreground" aria-live="polite">
                        {c.tipo === "invalido" ? <span className="text-error">{c.motivo}</span> : c.tipo === "interno" ? "Extensión o grupo del equipo" : c.tipo === "externo" ? "Guatemala" : ""}
                    </p>
                    <TecladoTelefono etiqueta="Teclado para marcar" alTocar={(d) => setMarcado((m) => (m.replace(/\s/g, "") + d).slice(0, 15))} />
                    <div className="flex items-center justify-center gap-6">
                        <span className="w-10" />
                        <Boton type="submit" aria-label="Llamar" disabled={llamando || (c.tipo !== "externo" && c.tipo !== "interno")} className="h-14 w-[4.5rem] rounded-full">
                            <Phone />
                        </Boton>
                        <Boton type="button" variante="fantasma" tamano="icono" aria-label="Borrar" disabled={marcado === ""} onClick={() => setMarcado((m) => m.replace(/\s/g, "").slice(0, -1))}>
                            <Delete />
                        </Boton>
                    </div>
                </form>
            </DialogoContenido>
        </Dialogo>
    );
}

export type TelefonoDeFicha = { tipo: TipoTelefonoFicha; etiqueta: string; mostrar: string };

/**
 * «Llamar» en una ficha (cliente, ticket, orden, conversación): un menú con sus teléfonos. El número
 * lo pone el servidor (el de Argos): aquí solo se elige cuál. Sin teléfono de Iris no se muestra.
 */
export function BotonLlamarTelefono({ opciones, titulo = "Llamar desde tu extensión" }: { opciones: readonly OpcionLlamar[]; titulo?: string }) {
    const t = useTelefono();
    const [ocupado, setOcupado] = useState(false);
    if (!puedeLlamar(t) || opciones.length === 0) return null;
    const ext = t.estado.extension;
    const hacer = async (o: OpcionLlamar) => {
        setOcupado(true);
        await t.llamar(o);
        setOcupado(false);
    };
    const [unica] = opciones;
    if (opciones.length === 1 && unica) {
        return (
            <Boton tamano="sm" disabled={ocupado} data-slot="llamar-telefono" onClick={() => void hacer(unica)} title={`${titulo}${ext ? ` ${ext}` : ""}`}>
                <Phone />
                Llamar
            </Boton>
        );
    }
    return (
        <MenuDesplegable>
            <MenuDesplegableDisparador>
                <Boton tamano="sm" disabled={ocupado} data-slot="llamar-telefono">
                    <Phone />
                    Llamar
                    <ChevronDown />
                </Boton>
            </MenuDesplegableDisparador>
            <MenuDesplegableContenido align="end" className="min-w-64">
                <MenuDesplegableEtiqueta>{`${titulo}${ext ? ` ${ext}` : ""}`}</MenuDesplegableEtiqueta>
                {opciones.map((o) => (
                    <MenuDesplegableElemento key={o.clave} icono={o.entrada.telefono === "movil" ? Smartphone : o.entrada.telefono === "contacto" ? UserRound : Phone} textValue={o.etiqueta} onSelect={() => void hacer(o)}>
                        <span className="grid">
                            <span className="font-medium">{o.etiqueta}</span>
                            {o.detalle ? <span className="text-xs text-muted-foreground tabular-nums">{o.detalle}</span> : null}
                        </span>
                    </MenuDesplegableElemento>
                ))}
            </MenuDesplegableContenido>
        </MenuDesplegable>
    );
}

/** Las opciones de un cliente: un elemento por teléfono que tiene (principal y móvil). */
export function opcionesCliente(cliente: { id: number; nombre: string }, telefonos: readonly { tipo: "telefono" | "movil"; mostrar: string }[], contexto: { ticketId?: number } = {}): OpcionLlamar[] {
    const vistos = new Set<TipoTelefonoFicha>();
    const out: OpcionLlamar[] = [];
    for (const tel of telefonos) {
        const tipo: TipoTelefonoFicha = tel.tipo === "movil" ? "movil" : "principal";
        if (vistos.has(tipo)) continue;
        vistos.add(tipo);
        const entrada = contexto.ticketId ? { ticketId: contexto.ticketId, telefono: tipo } : { clienteId: cliente.id, telefono: tipo };
        out.push({ clave: tipo, etiqueta: tipo === "movil" ? "Móvil" : "Principal", detalle: tel.mostrar, entrada, nombre: cliente.nombre, numero: tel.mostrar });
    }
    return out;
}
