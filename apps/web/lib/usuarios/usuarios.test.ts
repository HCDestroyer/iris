import type { Schema } from "@iris/contratos";
import { describe, expect, it } from "vitest";
import {
    cambioCorreoCierraSSO,
    cuerpoCrearUsuario,
    cuerpoDesactivarUsuario,
    cuerpoEditarUsuario,
    cuerpoEmpresaUsuario,
    esquemaAsignarEmpresa,
    esquemaCrearUsuario,
    esquemaDesactivarUsuario,
    esquemaEditarUsuario,
    faltanEnCrear,
    nombreEmpresa,
    valoresCrearUsuario,
    type DatosCrearUsuario,
} from "./esquemas";

const usuario: Schema<"Usuario"> = {
    id: 40,
    nombre: "Marta Xol",
    correo: "marta@iris.test",
    tipo: "persona",
    activo: true,
    mfaActivo: false,
    roles: [{ rol: "tecnico" }],
    empresas: [10],
    version: 3,
    creadoEn: "2026-10-01T00:00:00Z",
    actualizadoEn: "2026-10-01T00:00:00Z",
};

const base = (extra: Partial<DatosCrearUsuario> = {}): DatosCrearUsuario => ({
    ...valoresCrearUsuario(),
    nombre: "Marta Xol",
    correo: "marta@iris.test",
    contrasena: "k7Qm-3xTz-pR8w-Hn4c",
    ...extra,
});

const mensajes = (r: { success: boolean; error?: { issues: { path: PropertyKey[]; message: string }[] } }) =>
    Object.fromEntries((r.error?.issues ?? []).map((i) => [i.path.join("."), i.message]));

describe("crear usuario (espejo de crear_usuario.go)", () => {
    const sinEmpresas = esquemaCrearUsuario({ empresasObligatorias: false });
    const conEmpresas = esquemaCrearUsuario({ empresasObligatorias: true });

    it("persona por defecto: nombre, correo y contraseña de 12 caracteres o más (en runas)", () => {
        expect(valoresCrearUsuario().tipo).toBe("persona");
        expect(sinEmpresas.safeParse(base()).success).toBe(true);
        const r = sinEmpresas.safeParse(base({ nombre: "  ", correo: "no-es-correo", contrasena: "corta" }));
        expect(mensajes(r)).toEqual({ nombre: "Escribe el nombre", correo: "Correo inválido", contrasena: "Usa al menos 12 caracteres" });
        // 12 emojis son 12 runas (Go) aunque sean 24 unidades UTF-16.
        expect(sinEmpresas.safeParse(base({ contrasena: "🔑".repeat(11) })).success).toBe(false);
        expect(sinEmpresas.safeParse(base({ contrasena: "🔑".repeat(12) })).success).toBe(true);
        expect(mensajes(sinEmpresas.safeParse(base({ contrasena: "x".repeat(201) })))).toEqual({ contrasena: "Admite hasta 200 caracteres" });
        expect(mensajes(sinEmpresas.safeParse(base({ nombre: "n".repeat(121) })))).toEqual({ nombre: "Admite hasta 120 caracteres" });
    });

    it("integración: sin contraseña (la API responde 422 si viene)", () => {
        expect(sinEmpresas.safeParse(base({ tipo: "integracion", contrasena: "" })).success).toBe(true);
        expect(mensajes(sinEmpresas.safeParse(base({ tipo: "integracion" })))).toEqual({ contrasena: "Las integraciones entran solo con token" });
    });

    it("roles: con alcance completo y sin asignaciones repetidas", () => {
        const fila = { rol: "tecnico", alcance: "zona" as const, zonaId: "", cuadrillaId: "" };
        expect(mensajes(sinEmpresas.safeParse(base({ roles: [fila] })))).toEqual({ "roles.0.zonaId": "Elige la zona" });
        const dos = [
            { ...fila, zonaId: "4" },
            { ...fila, zonaId: "4" },
        ];
        expect(mensajes(sinEmpresas.safeParse(base({ roles: dos })))).toEqual({ "roles.1.rol": "Asignación repetida" });
        expect(sinEmpresas.safeParse(base({ roles: [dos[0] ?? fila, { ...fila, zonaId: "5" }] })).success).toBe(true);
    });

    it("empresas obligatorias sin empresas.todas (y sin repetir)", () => {
        expect(mensajes(conEmpresas.safeParse(base()))).toEqual({ empresas: "Indica al menos una empresa en que operará" });
        expect(conEmpresas.safeParse(base({ empresas: ["10"] })).success).toBe(true);
        expect(mensajes(sinEmpresas.safeParse(base({ empresas: ["10", "10"] })))).toEqual({ empresas: "Empresa repetida" });
    });

    it("«Falta…»: rol, alcance y empresa", () => {
        expect(faltanEnCrear(base(), false)).toEqual([]);
        expect(faltanEnCrear(base({ roles: [{ rol: "", alcance: "global", zonaId: "", cuadrillaId: "" }] }), true)).toEqual(["el rol de cada fila", "al menos una empresa"]);
        expect(faltanEnCrear(base({ roles: [{ rol: "soporte", alcance: "cuadrilla", zonaId: "", cuadrillaId: "" }] }), false)).toEqual(["la zona o cuadrilla del alcance"]);
    });

    it("cuerpo de POST /usuarios: correo normalizado, roles con alcance y empresas en número", () => {
        const d = base({
            nombre: "  Marta Xol ",
            correo: " Marta@IRIS.test ",
            roles: [
                { rol: "tecnico", alcance: "zona", zonaId: "4", cuadrillaId: "9" },
                { rol: "lectura", alcance: "global", zonaId: "", cuadrillaId: "" },
            ],
            empresas: ["20", "10"],
        });
        expect(cuerpoCrearUsuario(d)).toEqual({
            nombre: "Marta Xol",
            correo: "marta@iris.test",
            tipo: "persona",
            contrasena: "k7Qm-3xTz-pR8w-Hn4c",
            roles: [{ rol: "tecnico", zonaId: 4 }, { rol: "lectura" }],
            empresas: [10, 20],
        });
        expect(cuerpoCrearUsuario(base({ tipo: "integracion", contrasena: "" }))).toEqual({ nombre: "Marta Xol", correo: "marta@iris.test", tipo: "integracion" });
        expect(valoresCrearUsuario("tecnico").roles).toEqual([{ rol: "tecnico", alcance: "global", zonaId: "", cuadrillaId: "" }]);
    });
});

describe("editar, desactivar y empresas", () => {
    it("editar: solo lo que cambió, con la versión leída", () => {
        expect(esquemaEditarUsuario.safeParse({ nombre: "", correo: "x" }).success).toBe(false);
        expect(cuerpoEditarUsuario({ nombre: "Marta Xol", correo: "MARTA@iris.test" }, usuario)).toEqual({ version: 3 });
        expect(cuerpoEditarUsuario({ nombre: "Marta X.", correo: "mxol@iris.test" }, usuario)).toEqual({ version: 3, nombre: "Marta X.", correo: "mxol@iris.test" });
    });

    it("cambiar el correo de otro (persona) cierra su autologin; el propio no", () => {
        expect(cambioCorreoCierraSSO({ correo: "otro@iris.test" }, usuario, false)).toBe(true);
        expect(cambioCorreoCierraSSO({ correo: "otro@iris.test" }, usuario, true)).toBe(false);
        expect(cambioCorreoCierraSSO({ correo: " Marta@iris.test" }, usuario, false)).toBe(false);
        expect(cambioCorreoCierraSSO({ correo: "otro@iris.test" }, { ...usuario, tipo: "integracion" }, false)).toBe(false);
    });

    it("desactivar: motivo obligatorio (hasta 500)", () => {
        expect(mensajes(esquemaDesactivarUsuario.safeParse({ motivo: "   " }))).toEqual({ motivo: "Escribe el motivo" });
        expect(esquemaDesactivarUsuario.safeParse({ motivo: "m".repeat(501) }).success).toBe(false);
        expect(cuerpoDesactivarUsuario({ motivo: " Ya no trabaja aquí " }, 3)).toEqual({ version: 3, motivo: "Ya no trabaja aquí" });
    });

    it("empresas: elegir una, cuerpo con el id y nombre conocido o su id", () => {
        expect(esquemaAsignarEmpresa.safeParse({ empresaId: "" }).success).toBe(false);
        expect(cuerpoEmpresaUsuario("20")).toEqual({ empresaId: 20 });
        expect(nombreEmpresa(10, [{ id: 10, nombre: "Cable Norte" }])).toBe("Cable Norte");
        expect(nombreEmpresa(30, [{ id: 10, nombre: "Cable Norte" }])).toBe("Empresa 30");
    });
});
