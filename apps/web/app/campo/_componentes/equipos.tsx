"use client";

import { zodResolver } from "@hookform/resolvers/zod";
import type { OrdenVista } from "@iris/sync";
import { textoElegido, textosEscaneo, type ObjetivoEscaneo, type UsoCodigo } from "@iris/sync/campo";
import { Boton, Campo, CampoSelector, GrupoSegmentado, InsigniaEstado, Tarjeta, TarjetaContenido, TarjetaEncabezado, TarjetaTitulo } from "@iris/ui";
import { ArrowDownToLine, ArrowUpFromLine, ScanLine, ShieldCheck, ShieldQuestion } from "lucide-react";
import { useState, type ReactNode } from "react";
import { useForm, useWatch } from "react-hook-form";
import { normalizarMac } from "@/lib/campo/codigos";
import { esquemaEquipo, type DatosEquipo } from "@/lib/campo/esquemas";
import { CONDICIONES } from "@/lib/campo/textos";
import { useCampo } from "./contexto";
import { EscanerEquipo } from "./escaner-equipo";

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
    // Campo que se está escaneando (el escáner sabe su objetivo) y lo último que se llenó escaneando.
    const [escaneando, setEscaneando] = useState<ObjetivoEscaneo | null>(null);
    const [escaneado, setEscaneado] = useState<UsoCodigo>({});
    const form = useForm<DatosEquipo>({
        resolver: zodResolver(esquemaEquipo),
        defaultValues: { accion, serie: "", mac: "", modelo: "", unidadId: "", condicion: "" },
    });
    const unidadId = useWatch({ control: form.control, name: "unidadId" });
    const serie = useWatch({ control: form.control, name: "serie" });
    const mac = useWatch({ control: form.control, name: "mac" });

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
            setEscaneado({});
        }
    });

    const alElegir = (uso: UsoCodigo) => {
        const objetivo = escaneando;
        setEscaneando(null);
        if (uso.serie) {
            form.setValue("serie", uso.serie, { shouldValidate: true, shouldDirty: true });
        }
        if (uso.mac) {
            form.setValue("mac", uso.mac, { shouldValidate: true, shouldDirty: true });
        }
        setEscaneado((e) => ({ ...e, ...uso }));
        notificar({ tono: "success", texto: textoElegido(uso) });
        if (objetivo) {
            form.setFocus(objetivo);
        }
    };

    const aMano = () => {
        const objetivo = escaneando;
        setEscaneando(null);
        if (objetivo) {
            form.setFocus(objetivo);
        }
    };

    /** Botón de escanear de un campo (icono con nombre accesible, 44 px). */
    const botonEscanear = (objetivo: ObjetivoEscaneo) => (
        <Boton variante="secundario" tamano="lg" onClick={() => setEscaneando(objetivo)} title={textosEscaneo(objetivo).botonCampo} className="size-11 shrink-0 p-0">
            <ScanLine aria-hidden />
            <span className="sr-only">{textosEscaneo(objetivo).botonCampo}</span>
        </Boton>
    );
    /** Insignia discreta «Escaneado» mientras el valor es el que se leyó (al editarlo, se quita). */
    const insigniaEscaneado = (valor: string, leido: string | undefined): ReactNode =>
        leido && valor === leido ? <InsigniaEstado tono="info" icono={ScanLine} texto="Escaneado" /> : undefined;

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
                <EscanerEquipo objetivo={escaneando} existentes={orden.equipos ?? []} alElegir={alElegir} alEscribirAMano={aMano} alCerrar={() => setEscaneando(null)} />
            ) : null}
            <div className="flex items-start gap-2">
                <Campo
                    etiqueta="Número de serie"
                    className="min-w-0 flex-1"
                    autoComplete="off"
                    autoCapitalize="characters"
                    ayuda={insigniaEscaneado(serie, escaneado.serie)}
                    error={form.formState.errors.serie?.message}
                    {...form.register("serie")}
                />
                <div className="pt-6">{botonEscanear("serie")}</div>
            </div>
            <div className="flex items-start gap-2">
                <Campo
                    etiqueta="MAC"
                    className="min-w-0 flex-1"
                    autoComplete="off"
                    autoCapitalize="characters"
                    placeholder="AA:BB:CC:DD:EE:FF"
                    ayuda={insigniaEscaneado(mac, escaneado.mac)}
                    error={form.formState.errors.mac?.message}
                    {...form.register("mac")}
                />
                <div className="pt-6">{botonEscanear("mac")}</div>
            </div>
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
