import { z } from "zod";
import type { Umbrales } from "./tipos";

/* Umbrales de los avisos del Servidor de IA (contratos/voz.md §4.12), espejo del servidor. */

/** Los de fábrica. «Caído» e «incompatible» avisan siempre: no tienen umbral ni se apagan. */
export const DE_FABRICA: Omit<Umbrales, "version"> = {
    vramPct: 95,
    vramMinutos: 5,
    discoPct: 90,
    temperaturaC: 85,
    temperaturaMinutos: 5,
    saturacionRechazos: 10,
    saturacionMinutos: 5,
};

const entero = (min: number, max: number, mensaje: string) =>
    z
        .string()
        .trim()
        .refine((v) => /^\d+$/.test(v) && Number(v) >= min && Number(v) <= max, mensaje);

export const esquemaUmbrales = z.object({
    vramPct: entero(50, 100, "Entre 50 y 100 %"),
    vramMinutos: entero(1, 60, "Entre 1 y 60 minutos"),
    discoPct: entero(50, 99, "Entre 50 y 99 %"),
    temperaturaC: entero(60, 100, "Entre 60 y 100 °C"),
    temperaturaMinutos: entero(1, 60, "Entre 1 y 60 minutos"),
    saturacionRechazos: entero(1, 1000, "Entre 1 y 1000"),
    saturacionMinutos: entero(1, 60, "Entre 1 y 60 minutos"),
});
export type ValoresUmbrales = z.infer<typeof esquemaUmbrales>;
export const CAMPOS_UMBRALES = Object.keys(DE_FABRICA) as (keyof ValoresUmbrales)[];

export const valoresDeUmbrales = (u: Omit<Umbrales, "version">): ValoresUmbrales => ({
    vramPct: String(u.vramPct),
    vramMinutos: String(u.vramMinutos),
    discoPct: String(u.discoPct),
    temperaturaC: String(u.temperaturaC),
    temperaturaMinutos: String(u.temperaturaMinutos),
    saturacionRechazos: String(u.saturacionRechazos),
    saturacionMinutos: String(u.saturacionMinutos),
});

export const entradaDeUmbrales = (v: ValoresUmbrales, version: number): Umbrales => ({
    version,
    vramPct: Number(v.vramPct),
    vramMinutos: Number(v.vramMinutos),
    discoPct: Number(v.discoPct),
    temperaturaC: Number(v.temperaturaC),
    temperaturaMinutos: Number(v.temperaturaMinutos),
    saturacionRechazos: Number(v.saturacionRechazos),
    saturacionMinutos: Number(v.saturacionMinutos),
});

/** ¿Son los valores de fábrica? */
export const esDeFabrica = (v: ValoresUmbrales): boolean => CAMPOS_UMBRALES.every((c) => v[c] === String(DE_FABRICA[c]));

/** Empresas permitidas y simultáneas por empresa (`POST /voz/motores/{id}/empresas/actualizar`). */
export const esquemaEmpresas = z
    .object({
        quienes: z.enum(["todas", "algunas"]),
        empresas: z.array(z.number()),
        simultaneasPorEmpresa: entero(1, 16, "Entre 1 y 16"),
    })
    .superRefine((d, ctx) => {
        if (d.quienes === "algunas" && d.empresas.length === 0) ctx.addIssue({ code: "custom", path: ["empresas"], message: "Elige al menos una empresa, o deja «Todas las empresas»" });
    });
export type ValoresEmpresas = z.infer<typeof esquemaEmpresas>;

/** La dirección del servidor al enlazar: https, con host y sin usuario, ruta ni parámetros. */
export const esquemaEnlace = z.object({
    nombre: z.string().trim().min(1, "Ponle un nombre para reconocerlo").max(80, "Hasta 80 caracteres"),
    url: z
        .string()
        .trim()
        .min(1, "Escribe la dirección que mostró el instalador")
        .refine((v) => {
            try {
                const u = new URL(v);
                return u.protocol === "https:" && u.hostname !== "" && u.username === "" && u.password === "" && (u.pathname === "/" || u.pathname === "") && u.search === "" && u.hash === "";
            } catch {
                return false;
            }
        }, "Tiene que ser como https://10.0.0.5:8443 (https, la dirección del servidor y su puerto)"),
});
export type ValoresEnlace = z.infer<typeof esquemaEnlace>;
