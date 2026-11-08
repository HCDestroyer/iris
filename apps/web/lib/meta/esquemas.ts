import { z } from "zod";

/*
 * Espejo de `POST /conexion-meta/actualizar` (contratos/conversaciones.md §15.1): app ID y config
 * ID de 1 a 30 dígitos, Graph API `vNN.N` o vacío (la de Iris), servicio central en https (hasta
 * 300), app secret hasta 256 y clave de enlace hasta 512. Los
 * secretos son de solo escritura: vacío = no cambia, salvo la primera vez (lo exige el servidor y
 * aquí `requiereSecreto` / `requiereClave`).
 */

const MSJ_SECRETO = "Hasta 256 caracteres";
const MSJ_CLAVE = "Hasta 512 caracteres";

export const esquemaConexionMeta = z
    .object({
        modo: z.enum(["propia", "compartida"]),
        appId: z.string().trim(),
        configId: z.string().trim(),
        graphVersion: z.string().trim(),
        appSecret: z.string().trim(),
        servicioUrl: z.string().trim(),
        claveEnlace: z.string().trim(),
        requiereSecreto: z.boolean(),
        requiereClave: z.boolean(),
    })
    .superRefine((v, ctx) => {
        if (v.modo === "propia") {
            if (!v.appId) ctx.addIssue({ code: "custom", path: ["appId"], message: "Pega el App ID" });
            else if (!/^[0-9]{1,30}$/.test(v.appId)) ctx.addIssue({ code: "custom", path: ["appId"], message: "Solo números (hasta 30 dígitos)" });
            if (!v.configId) ctx.addIssue({ code: "custom", path: ["configId"], message: "Pega el ID de configuración" });
            else if (!/^[0-9]{1,30}$/.test(v.configId)) ctx.addIssue({ code: "custom", path: ["configId"], message: "Solo números (hasta 30 dígitos)" });
            if (v.graphVersion && !/^v[0-9]{1,2}\.[0-9]$/.test(v.graphVersion)) ctx.addIssue({ code: "custom", path: ["graphVersion"], message: "Como v23.0, o vacío para usar la de Iris" });
            if (!v.appSecret && v.requiereSecreto) ctx.addIssue({ code: "custom", path: ["appSecret"], message: "Pega el app secret" });
            else if (v.appSecret.length > 256) ctx.addIssue({ code: "custom", path: ["appSecret"], message: MSJ_SECRETO });
        } else {
            if (v.servicioUrl) {
                let valida = false;
                try {
                    valida = new URL(v.servicioUrl).protocol === "https:";
                } catch {
                    valida = false;
                }
                if (!valida) ctx.addIssue({ code: "custom", path: ["servicioUrl"], message: "Una dirección https://" });
                else if (v.servicioUrl.length > 300) ctx.addIssue({ code: "custom", path: ["servicioUrl"], message: "Hasta 300 caracteres" });
            }
            if (!v.claveEnlace && v.requiereClave) ctx.addIssue({ code: "custom", path: ["claveEnlace"], message: "Pega la clave de enlace" });
            else if (v.claveEnlace.length > 512) ctx.addIssue({ code: "custom", path: ["claveEnlace"], message: MSJ_CLAVE });
        }
    });
export type EntradaFormConexion = z.input<typeof esquemaConexionMeta>;
export type ValoresFormConexion = z.output<typeof esquemaConexionMeta>;

/** Motivo para dejar de compartir un número (5–300, §15.3). */
export const esquemaRevocar = z.object({ motivo: z.string().trim().min(5, "Escribe el motivo (al menos 5 letras)").max(300, "Hasta 300 caracteres") });

/** «Pasar a otra empresa» (§15.3): la empresa y una nota opcional (hasta 1000). */
export const esquemaPasarEmpresa = z.object({
    empresaId: z.string().min(1, "Elige la empresa"),
    nota: z.string().trim().max(1000, "Hasta 1000 caracteres"),
});
