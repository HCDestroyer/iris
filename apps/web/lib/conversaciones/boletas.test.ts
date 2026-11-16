import { describe, expect, it } from "vitest";
import { ErrorApi } from "../api/errores";
import { abrirEnArgos, explicarErrorAbrir, explicarNoAbrio, origenDe, urlArgosSegura } from "../integraciones/abrir-argos";
import { debeReleer, esquemaAprobar, esquemaRechazar, estadoBoleta, explicarErrorBoleta, montoLegible, NOTA_RECHAZO, quienDecidio, textoConfirmacion, TEXTO_SIN_CONFIRMAR } from "./boletas";
import { textoEvento } from "./presentacion";

const error = (codigo: string, estado: number, detalle?: string) => ({ estado, codigo, titulo: "Título", detalle });

describe("boletas: aprobar y rechazar (B3)", () => {
    it("sin vínculo con Argos: ofrece vincular", () => {
        const e = explicarErrorBoleta(error("argos_sin_vinculo", 409), "aprobar");
        expect(e.tipo).toBe("vincular");
        expect(e.titulo).toBe("Vincula tu usuario de Argos para aprobar pagos");
    });

    it("permiso, empresa o usuario inactivo en Argos: se explican y no se reintenta", () => {
        for (const codigo of ["argos_sin_permiso", "argos_sin_empresa", "argos_usuario_inactivo", "solo_personas"]) {
            expect(explicarErrorBoleta(error(codigo, 403), "rechazar").tipo).toBe("explicar");
        }
        expect(explicarErrorBoleta(error("argos_sin_permiso", 403), "rechazar").texto).toContain("rechazar comprobantes");
        expect(explicarErrorBoleta(error("argos_sin_permiso", 403), "aprobar").texto).toContain("aprobar comprobantes");
    });

    it("ya la decidió otra persona o caja: se vuelve a leer", () => {
        for (const codigo of ["otra_persona_decidio", "comprobante_revisado", "boleta_ya_decidida"]) {
            expect(explicarErrorBoleta(error(codigo, 409), "aprobar").tipo).toBe("releer");
        }
    });

    it("Argos no respondió o no hay red: no se afirma que no quedó nada; «Reintentar» para confirmarlo", () => {
        expect(TEXTO_SIN_CONFIRMAR).toBe("Puede haberse aplicado en Argos: vuelve a intentar para confirmarlo.");
        expect(explicarErrorBoleta(error("argos_no_disponible", 502), "aprobar")).toEqual({ tipo: "reintentar", titulo: "Argos no respondió", texto: TEXTO_SIN_CONFIRMAR });
        expect(explicarErrorBoleta(error("idempotencia_en_curso", 409), "aprobar")).toMatchObject({ tipo: "reintentar", texto: TEXTO_SIN_CONFIRMAR });
        expect(explicarErrorBoleta(error("sin_conexion", 0), "aprobar").texto).toContain(TEXTO_SIN_CONFIRMAR);
        expect(explicarErrorBoleta(error("sin_conexion", 0), "rechazar")).toMatchObject({ tipo: "reintentar", titulo: "No se pudo rechazar la boleta" });
        expect(explicarErrorBoleta(error("algo_nuevo", 500), "aprobar").tipo).toBe("reintentar");
    });

    it("todo 409 relee la boleta, sea cual sea su código; lo demás no", () => {
        for (const estado of [409]) expect(debeReleer({ estado })).toBe(true);
        for (const estado of [0, 403, 404, 422, 500, 502]) expect(debeReleer({ estado })).toBe(false);
    });

    it("el monto cambió: se pide confirmar de nuevo, sin perder el diálogo", () => {
        expect(explicarErrorBoleta(error("monto_cambio", 409), "aprobar")).toMatchObject({ tipo: "confirmar", titulo: "El monto de la boleta cambió" });
    });

    it("Argos sin el contrato 0.7.0: pide actualizar, sin reintentar", () => {
        expect(explicarErrorBoleta(error("argos_sin_aprobacion", 422), "aprobar")).toMatchObject({ tipo: "explicar", titulo: "Actualiza Argos a 1.15.0 o posterior" });
    });

    it("un código que el panel no conoce muestra el detalle del servidor y no ofrece reintentar un 4xx", () => {
        expect(explicarErrorBoleta(error("regla_nueva", 422, "No se puede por la regla nueva"), "aprobar")).toEqual({ tipo: "explicar", titulo: "No se pudo aprobar la boleta", texto: "No se puede por la regla nueva" });
    });

    it("esquemas espejo: nota hasta 400; motivo de 3 a 500 y causa del catálogo", () => {
        expect(esquemaAprobar.safeParse({ nota: "" }).success).toBe(true);
        expect(esquemaAprobar.safeParse({ nota: "x".repeat(401) }).success).toBe(false);
        expect(esquemaRechazar.safeParse({ motivo: "no", causa: "otro" }).success).toBe(false);
        expect(esquemaRechazar.safeParse({ motivo: "  ab  ", causa: "otro" }).success).toBe(false);
        expect(esquemaRechazar.safeParse({ motivo: "No aparece el depósito", causa: "no_aparece" }).success).toBe(true);
        expect(esquemaRechazar.safeParse({ motivo: "x".repeat(501), causa: "otro" }).success).toBe(false);
        expect(esquemaRechazar.safeParse({ motivo: "No aparece", causa: "porque_si" }).success).toBe(false);
    });

    it("confirmación con Argos: pendiente es discreto; agotada es un aviso", () => {
        expect(textoConfirmacion({ confirmacionArgos: null })).toBeNull();
        expect(textoConfirmacion({ confirmacionArgos: "pendiente" })).toEqual({ texto: "Confirmando con Argos…", aviso: false });
        expect(textoConfirmacion({ confirmacionArgos: "agotada" })).toEqual({ texto: "No se pudo confirmar con Argos", aviso: true });
    });

    it("rechazar: la nota dice qué le llega al cliente", () => {
        expect(NOTA_RECHAZO).toBe("Al cliente le llega un texto fijo según la causa. Tu motivo se agrega solo si no lleva números, puntos ni enlaces.");
    });

    it("mensajes de sistema de la boleta en el hilo", () => {
        expect(textoEvento({ tipo: "boleta_aprobada", actorNombre: "Ana", referencia: "BI-778812" })).toBe("Ana aprobó la boleta BI-778812");
        expect(textoEvento({ tipo: "boleta_aprobada" })).toBe("Caja aprobó la boleta");
        expect(textoEvento({ tipo: "boleta_rechazada", actorNombre: "Ana", detalle: "No aparece el depósito" })).toBe("Ana rechazó la boleta: No aparece el depósito");
        expect(textoEvento({ tipo: "boleta_corregida", referencia: "BI-778812", detalle: "aprobada → rechazada" })).toBe("Argos corrigió la boleta BI-778812: aprobada → rechazada");
        expect(textoEvento({ tipo: "boleta_sin_confirmar", referencia: "BI-778812" })).toBe("No se pudo confirmar con Argos la boleta BI-778812: revísala en Argos");
    });

    it("monto, estado y quién decidió", () => {
        expect(montoLegible("250.00")).toBe("Q 250.00");
        expect(montoLegible("1250.5")).toBe("Q 1,250.50");
        expect(montoLegible("no-es-numero")).toBe("Q no-es-numero");
        expect(estadoBoleta({ estado: "registrada" })).toEqual({ texto: "Pendiente de revisar", tono: "warning" });
        expect(estadoBoleta({ estado: "aprobada" }).tono).toBe("success");
        expect(estadoBoleta({ estado: "fallida" }).texto).toBe("Argos no la aceptó");
        expect(quienDecidio({ estado: "registrada", verificadoPorNombre: null })).toBeNull();
        expect(quienDecidio({ estado: "aprobada", verificadoPorNombre: "Marta Caal" })).toBe("La aprobó Marta Caal");
        expect(quienDecidio({ estado: "rechazada", verificadoPorNombre: null, decididaPor: { id: 1, nombre: "Ana López" } })).toBe("La rechazó Ana López");
        expect(quienDecidio({ estado: "aprobada", verificadoPorNombre: null })).toBe("La aprobó caja en Argos");
    });
});

describe("abrir Argos con autologin", () => {
    const ARGOS = "https://argos.isp.gt";

    it("el enlace del ticket solo se abre por https y en el origen del panel de Argos del conector", () => {
        expect(urlArgosSegura("https://argos.isp.gt/sso/entrar#abc", ARGOS)).toBe("https://argos.isp.gt/sso/entrar#abc");
        // Otro sitio, aunque se parezca; otro puerto; otro subdominio.
        expect(urlArgosSegura("https://argos.isp.gt.otro.sitio/sso/entrar#abc", ARGOS)).toBeNull();
        expect(urlArgosSegura("https://argos.isp.gt:8443/sso/entrar#abc", ARGOS)).toBeNull();
        expect(urlArgosSegura("https://otro.isp.gt/sso/entrar#abc", ARGOS)).toBeNull();
        // http por la red nunca, ni con el origen esperado en http.
        expect(urlArgosSegura("http://argos.isp.gt/sso/entrar#abc", ARGOS)).toBeNull();
        expect(urlArgosSegura("http://argos.isp.gt/sso/entrar#abc", "http://argos.isp.gt")).toBeNull();
        expect(urlArgosSegura("http://10.0.0.5:8080/sso/entrar#abc", "http://10.0.0.5:8080")).toBeNull();
        // http solo en este equipo (desarrollo).
        expect(urlArgosSegura("http://localhost:3000/sso/entrar#abc", "http://localhost:3000")).toBe("http://localhost:3000/sso/entrar#abc");
        expect(urlArgosSegura("http://127.0.0.1:3000/sso/entrar#abc", "http://127.0.0.1:3000")).toBe("http://127.0.0.1:3000/sso/entrar#abc");
        expect(urlArgosSegura("http://localhost.otro.sitio/sso/entrar", "http://localhost.otro.sitio")).toBeNull();
        // Sin origen conocido no se abre nada.
        expect(urlArgosSegura("https://argos.isp.gt/sso/entrar#abc", null)).toBeNull();
        expect(urlArgosSegura("javascript:alert(1)", ARGOS)).toBeNull();
        expect(urlArgosSegura("data:text/html,hola", ARGOS)).toBeNull();
        expect(urlArgosSegura("https://ana:clave@argos.isp.gt/", ARGOS)).toBeNull();
        expect(urlArgosSegura("/sso/entrar", ARGOS)).toBeNull();
        expect(urlArgosSegura("", ARGOS)).toBeNull();
    });

    it("el origen esperado sale de la dirección del conector", () => {
        expect(origenDe("https://argos.isp.gt/")).toBe(ARGOS);
        expect(origenDe("https://argos.isp.gt/panel")).toBe(ARGOS);
        expect(origenDe("javascript:alert(1)")).toBeNull();
        expect(origenDe("https://ana:clave@argos.isp.gt/")).toBeNull();
        expect(origenDe(undefined)).toBeNull();
        expect(origenDe("")).toBeNull();
    });

    function ventana() {
        const v = { opener: {} as unknown, destino: null as string | null, cerrada: false, location: { replace: (u: string) => void (v.destino = u) }, close: () => void (v.cerrada = true) };
        return v;
    }

    it("abre la pestaña en el clic, sin opener, y la manda al enlace", async () => {
        const v = ventana();
        const r = await abrirEnArgos(undefined, ARGOS, { abrirVentana: () => v, pedir: async () => ({ url: `${ARGOS}/sso/entrar#t1` }) });
        expect(r).toEqual({ abierto: true });
        expect(v.opener).toBeNull();
        expect(v.destino).toBe(`${ARGOS}/sso/entrar#t1`);
        expect(v.cerrada).toBe(false);
    });

    it("con las ventanas emergentes bloqueadas no pide el ticket", async () => {
        let pedidos = 0;
        const r = await abrirEnArgos(undefined, ARGOS, { abrirVentana: () => null, pedir: async () => ((pedidos += 1), { url: `${ARGOS}/sso/entrar#t1` }) });
        expect(r).toEqual({ abierto: false, motivo: "emergentes_bloqueadas" });
        expect(pedidos).toBe(0);
        expect(explicarNoAbrio({ abierto: false, motivo: "emergentes_bloqueadas" })).toEqual({ texto: "Permite las ventanas emergentes para abrir Argos.", reintentar: true });
    });

    it("sin saber cuál es el panel de Argos no abre ni pide nada", async () => {
        let abiertas = 0;
        let pedidos = 0;
        const r = await abrirEnArgos(undefined, null, { abrirVentana: () => ((abiertas += 1), ventana()), pedir: async () => ((pedidos += 1), { url: `${ARGOS}/x` }) });
        expect(r).toEqual({ abierto: false, motivo: "origen_desconocido" });
        expect([abiertas, pedidos]).toEqual([0, 0]);
    });

    it("un enlace de otro sitio no se abre: la pestaña se cierra y lo dice", async () => {
        const v = ventana();
        const r = await abrirEnArgos(undefined, ARGOS, { abrirVentana: () => v, pedir: async () => ({ url: "https://argos.isp.gt.otro.sitio/sso/entrar#t1" }) });
        expect(r).toEqual({ abierto: false, motivo: "enlace_no_confiable" });
        expect(v.destino).toBeNull();
        expect(v.cerrada).toBe(true);
        expect(explicarNoAbrio({ abierto: false, motivo: "enlace_no_confiable" }).reintentar).toBe(false);
    });

    const pideContrasena = new ErrorApi({ estado: 403, codigo: "reautenticacion_requerida", titulo: "Re-autenticación requerida" });

    it("sesión abierta desde Argos: cierra la pestaña, pide la contraseña y reintenta una vez", async () => {
        const ventanas: ReturnType<typeof ventana>[] = [];
        let pedidos = 0;
        let contrasenas = 0;
        const r = await abrirEnArgos(undefined, ARGOS, {
            abrirVentana: () => (ventanas.push(ventana()), ventanas.at(-1) ?? null),
            pedir: () => ((pedidos += 1), pedidos === 1 ? Promise.reject(pideContrasena) : Promise.resolve({ url: `${ARGOS}/sso/entrar#t2` })),
            reautenticar: async () => ((contrasenas += 1), true),
        });
        expect(r).toEqual({ abierto: true });
        expect([pedidos, contrasenas, ventanas.length]).toEqual([2, 1, 2]);
        expect(ventanas[0]?.cerrada).toBe(true);
        expect(ventanas[1]?.destino).toBe(`${ARGOS}/sso/entrar#t2`);
    });

    it("si no confirma la contraseña, no abre; y si el servidor la vuelve a pedir, no entra en un ciclo", async () => {
        const v = ventana();
        const cancelada = await abrirEnArgos(undefined, ARGOS, { abrirVentana: () => v, pedir: () => Promise.reject(pideContrasena), reautenticar: async () => false });
        expect(cancelada).toEqual({ abierto: false, motivo: "sin_reautenticar" });
        expect(v.cerrada).toBe(true);
        expect(explicarNoAbrio({ abierto: false, motivo: "sin_reautenticar" }).reintentar).toBe(true);
        let pedidos = 0;
        const otraVez = await abrirEnArgos(undefined, ARGOS, { abrirVentana: ventana, pedir: () => ((pedidos += 1), Promise.reject(pideContrasena)), reautenticar: async () => true });
        expect(otraVez).toEqual({ abierto: false, error: pideContrasena });
        expect(pedidos).toBe(2);
    });

    it("si pedir el enlace falla, la pestaña se cierra y queda el error del servidor", async () => {
        const v = ventana();
        const fallo = new ErrorApi({ estado: 502, codigo: "argos_no_disponible", titulo: "Argos no respondió" });
        const r = await abrirEnArgos({ tipo: "cliente", idExterno: "4101" }, ARGOS, { abrirVentana: () => v, pedir: () => Promise.reject(fallo) });
        expect(r).toEqual({ abierto: false, error: fallo });
        expect(v.cerrada).toBe(true);
        expect(explicarNoAbrio({ abierto: false, error: fallo })).toEqual({ texto: "Argos no respondió. Vuelve a intentarlo.", reintentar: true });
    });

    it("explica por qué no abrió y si vale la pena reintentar", () => {
        const api = (codigo: string, estado: number, reintentarEn?: number) => new ErrorApi({ estado, codigo, titulo: "Título", reintentarEn });
        expect(explicarErrorAbrir(api("conector_apagado", 409))).toMatchObject({ reintentar: false });
        expect(explicarErrorAbrir(api("argos_sin_aprobacion", 422)).texto).toContain("Actualiza Argos a 1.15.0");
        expect(explicarErrorAbrir(api("demasiados_intentos", 429, 40))).toEqual({ texto: "Demasiados intentos. Vuelve a probar en 40 s.", reintentar: true });
        expect(explicarErrorAbrir(api("argos_no_disponible", 502))).toMatchObject({ reintentar: true });
        expect(explicarErrorAbrir(new Error("x"))).toEqual({ texto: "No se pudo abrir Argos.", reintentar: true });
    });
});
