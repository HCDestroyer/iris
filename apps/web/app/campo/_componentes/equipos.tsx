"use client";

import { zodResolver } from "@hookform/resolvers/zod";
import type { OrdenVista } from "@iris/sync";
import { Boton, Campo, CampoSelector, Escaner, GrupoSegmentado, InsigniaEstado, Tarjeta, TarjetaContenido, TarjetaEncabezado, TarjetaTitulo } from "@iris/ui";
import { ArrowDownToLine, ArrowUpFromLine, ScanLine, ShieldCheck, ShieldQuestion } from "lucide-react";
import { useState } from "react";
import { useForm, useWatch } from "react-hook-form";
import { interpretarCodigo, normalizarMac } from "@/lib/campo/codigos";
import { esquemaEquipo, type DatosEquipo } from "@/lib/campo/esquemas";
import { CONDICIONES } from "@/lib/campo/textos";
import { useCampo } from "./contexto";

/** Equipos instalados y retirados según el tipo de orden; serie y MAC escaneadas o a mano. */
export function Equipos({ orden, instalar, retirar }: { orden: OrdenVista; instalar: boolean; retirar: boolean }) {
    const registrados = orden.equipos ?? [];
    const [elegida, setAccion] = useState<"instalar" | "retirar">("instalar");
    // Solo se elige con los dos movimientos (cambio de equipo); si no, el único que admite el tipo.
    const accion = instalar && retirar ? elegida : instalar ? "instalar" : "retirar";
    const comodato = orden.comodato.disponible ? (orden.comodato.equipos ?? []) : [];
    if (!instalar && !retirar && registrados.length === 0) {
        return null;
    }
    return (
        <Tarjeta>
            <TarjetaEncabezado>
                <TarjetaTitulo>Equipos</TarjetaTitulo>
            </TarjetaEncabezado>
            <TarjetaContenido className="grid gap-3">
                {registrados.length > 0 ? (
                    <ul className="grid gap-2 text-sm">
                        {registrados.map((e) => (
                            <li key={e.id} className="flex flex-wrap items-center gap-2">
                                <InsigniaEstado
                                    tono="info"
                                    icono={e.accion === "instalar" ? ArrowDownToLine : ArrowUpFromLine}
                                    texto={e.accion === "instalar" ? "Instalado" : "Retirado"}
                                />
                                <span className="font-mono">{e.serie ?? `Unidad ${e.unidadId ?? "—"}`}</span>
                                {e.mac ? <span className="font-mono text-muted-foreground">{e.mac}</span> : null}
                                {e.condicion ? <span className="text-muted-foreground">· {CONDICIONES[e.condicion]}</span> : null}
                            </li>
                        ))}
                    </ul>
                ) : null}
                {instalar && retirar ? (
                    <GrupoSegmentado
                        etiqueta="Movimiento"
                        className="w-full"
                        opciones={[
                            { valor: "instalar", etiqueta: "Instalar", icono: ArrowDownToLine },
                            { valor: "retirar", etiqueta: "Retirar", icono: ArrowUpFromLine },
                        ]}
                        valor={accion}
                        alCambiar={setAccion}
                    />
                ) : null}
                {instalar || retirar ? <FormularioEquipo key={accion} orden={orden} accion={accion} comodato={comodato} /> : null}
            </TarjetaContenido>
        </Tarjeta>
    );
}

function FormularioEquipo({
    orden,
    accion,
    comodato,
}: {
    orden: OrdenVista;
    accion: "instalar" | "retirar";
    comodato: NonNullable<OrdenVista["comodato"]["equipos"]>;
}) {
    const { motor, ejecutar, notificar } = useCampo();
    const [escaneando, setEscaneando] = useState(false);
    const form = useForm<DatosEquipo>({
        resolver: zodResolver(esquemaEquipo),
        defaultValues: { accion, serie: "", mac: "", modelo: "", unidadId: "", condicion: "" },
    });
    const unidadId = useWatch({ control: form.control, name: "unidadId" });

    const enviar = form.handleSubmit(async (d) => {
        const datos = {
            ...(d.unidadId ? { unidadId: Number(d.unidadId) } : {}),
            ...(d.serie ? { serie: d.serie } : {}),
            ...(d.mac ? { mac: normalizarMac(d.mac) ?? d.mac } : {}),
            ...(d.modelo ? { modelo: d.modelo } : {}),
            ...(d.accion === "retirar" && d.condicion ? { condicion: d.condicion } : {}),
        };
        const tipo = d.accion === "instalar" ? "equipo.instalar" : "equipo.retirar";
        if (await ejecutar(() => motor.registrar(tipo, orden.id, datos), d.accion === "instalar" ? "Equipo instalado registrado" : "Equipo retirado registrado")) {
            form.reset({ accion, serie: "", mac: "", modelo: "", unidadId: "", condicion: "" });
        }
    });

    const alLeer = (texto: string) => {
        setEscaneando(false);
        const leido = interpretarCodigo(texto);
        if (leido.serie) {
            form.setValue("serie", leido.serie, { shouldValidate: true });
        }
        if (leido.mac) {
            form.setValue("mac", leido.mac, { shouldValidate: true });
        }
        notificar({ tono: "success", texto: leido.mac && !leido.serie ? `MAC leída: ${leido.mac}` : `Leído: ${leido.serie ?? ""}${leido.mac ? ` · ${leido.mac}` : ""}` });
    };

    return (
        <form onSubmit={enviar} className="grid gap-3" noValidate aria-label={accion === "instalar" ? "Instalar equipo" : "Retirar equipo"}>
            {accion === "retirar" && comodato.length > 0 ? (
                <CampoSelector
                    etiqueta="Equipo en comodato"
                    value={unidadId}
                    onChange={(e) => {
                        const eq = comodato.find((c) => String(c.unidadId) === e.target.value);
                        form.setValue("unidadId", e.target.value);
                        form.setValue("serie", eq?.serie ?? "", { shouldValidate: true });
                        form.setValue("mac", eq?.mac ?? "");
                        form.setValue("modelo", eq?.modelo ?? "");
                    }}
                >
                    <option value="">Otro (escribir o escanear)</option>
                    {comodato.map((c) => (
                        <option key={c.unidadId} value={c.unidadId}>
                            {c.modelo} · {c.serie}
                        </option>
                    ))}
                </CampoSelector>
            ) : null}
            {escaneando ? (
                <Escaner alLeer={alLeer} alCerrar={() => setEscaneando(false)} />
            ) : (
                <Boton variante="secundario" tamano="lg" onClick={() => setEscaneando(true)}>
                    <ScanLine aria-hidden />
                    Escanear serie o MAC
                </Boton>
            )}
            <Campo etiqueta="Número de serie" autoComplete="off" autoCapitalize="characters" error={form.formState.errors.serie?.message} {...form.register("serie")} />
            <Campo etiqueta="MAC" autoComplete="off" autoCapitalize="characters" placeholder="AA:BB:CC:DD:EE:FF" error={form.formState.errors.mac?.message} {...form.register("mac")} />
            {accion === "instalar" ? <Campo etiqueta="Modelo" autoComplete="off" error={form.formState.errors.modelo?.message} {...form.register("modelo")} /> : null}
            {accion === "retirar" ? (
                <CampoSelector etiqueta="Condición" error={form.formState.errors.condicion?.message} {...form.register("condicion")}>
                    <option value="">Elige la condición</option>
                    {Object.entries(CONDICIONES).map(([v, t]) => (
                        <option key={v} value={v}>
                            {t}
                        </option>
                    ))}
                </CampoSelector>
            ) : null}
            {accion === "retirar" && !comodato.length ? (
                <p className="flex items-center gap-1.5 text-xs text-muted-foreground">
                    <ShieldQuestion aria-hidden className="size-3.5" />
                    Sin comodato de Argos en el teléfono: se valida al enviar.
                </p>
            ) : accion === "retirar" ? (
                <p className="flex items-center gap-1.5 text-xs text-muted-foreground">
                    <ShieldCheck aria-hidden className="size-3.5" />
                    Elige el equipo del comodato para que coincida con Argos.
                </p>
            ) : null}
            <Boton tamano="lg" type="submit" variante="secundario" disabled={form.formState.isSubmitting}>
                {accion === "instalar" ? "Registrar equipo instalado" : "Registrar equipo retirado"}
            </Boton>
        </form>
    );
}
