import type { Schema } from "@iris/contratos";
import { describe, expect, it } from "vitest";
import {
    cuerpoAsignarRol,
    cuerpoCrearRol,
    cuerpoEditarRol,
    esquemaAsignarRol,
    esquemaFiltrosAccesos,
    esquemaRol,
    faltaEnAsignacion,
    FILTROS_VACIOS,
    inicioDiaGuatemala,
    queryAccesos,
} from "./esquemas";
import { agruparPermisos, CANALES, ESTADOS_ACTIVIDAD, nombrePlataforma, RESULTADOS, sensiblesAgregados, textoActividad, textoAlcance } from "./presentacion";

const catalogo: Schema<"PermisoInfo">[] = [
    { codigo: "tickets.ver", descripcion: "Ver tickets", modulo: "soporte", moduloNombre: "Soporte (tickets)", sensible: false },
    { codigo: "buzones.crear", descripcion: "Crear buzones", modulo: "soporte", moduloNombre: "Soporte (tickets)", sensible: true },
    { codigo: "roles.crear", descripcion: "Crear roles", modulo: "roles", moduloNombre: "Roles y permisos", sensible: true },
    { codigo: "agenda.ver", descripcion: "Ver agenda", modulo: "agenda", moduloNombre: "Agenda y rutas", sensible: false },
];

describe("presentación de accesos", () => {
    it("cada canal, resultado y estado tiene icono y texto (nunca solo color)", () => {
        for (const p of [...Object.values(CANALES), ...Object.values(RESULTADOS), ...Object.values(ESTADOS_ACTIVIDAD)]) {
            expect(p.etiqueta.length).toBeGreaterThan(0);
            expect(p.icono).toBeTruthy();
        }
    });

    it("texto del indicador de la app de campo", () => {
        const ahora = Date.parse("2026-10-30T18:00:00Z");
        expect(textoActividad({ estado: "al_dia", ultimaConexion: "2026-10-30T17:59:00Z", pendientes: 0 }, ahora)).toBe("Al día");
        expect(textoActividad({ estado: "atrasado", ultimaConexion: "2026-10-30T17:59:00Z", pendientes: 3 }, ahora)).toBe("Atrasado · 3 sin enviar");
        expect(textoActividad({ estado: "sin_conexion", ultimaConexion: "2026-10-30T16:00:00Z", pendientes: null }, ahora)).toBe("Sin conexión hace 2 h");
        expect(textoActividad({ estado: "sin_conexion", ultimaConexion: null, pendientes: null }, ahora)).toBe("Sin conexión registrada");
        expect(textoActividad({ estado: "sin_conexion", ultimaConexion: "2026-10-27T16:00:00Z", pendientes: null }, ahora)).toBe("Sin conexión desde 27/10/2026 10:00");
        expect(nombrePlataforma("android")).toBe("Android");
        expect(nombrePlataforma("tizen")).toBe("tizen");
        expect(nombrePlataforma(null)).toBe("—");
    });

    it("agrupa los permisos por módulo y detecta los sensibles que se agregan", () => {
        const grupos = agruparPermisos(catalogo);
        expect(grupos.map((g) => g.nombre)).toEqual(["Agenda y rutas", "Roles y permisos", "Soporte (tickets)"]);
        expect(grupos[2]?.permisos.map((p) => p.codigo)).toEqual(["buzones.crear", "tickets.ver"]);
        expect(sensiblesAgregados(catalogo, ["buzones.crear"], ["buzones.crear", "roles.crear", "tickets.ver"]).map((p) => p.codigo)).toEqual(["roles.crear"]);
        expect(sensiblesAgregados(catalogo, [], ["tickets.ver"])).toEqual([]);
    });

    it("alcance legible", () => {
        expect(textoAlcance({})).toBe("Todas las zonas");
        expect(textoAlcance({ zonaId: 4 }, { zonas: new Map([[4, "Norte"]]) })).toBe("Zona Norte");
        expect(textoAlcance({ cuadrillaId: 9 })).toBe("Cuadrilla 9");
    });
});

describe("esquemas de roles (espejo de roles.go)", () => {
    it("exige código válido, nombre y al menos un permiso", () => {
        const r = esquemaRol.safeParse({ codigo: "Con Espacio", nombre: " ", descripcion: "", permisos: [] });
        expect(r.success).toBe(false);
        const mensajes = r.success ? [] : r.error.issues.map((i) => i.message);
        expect(mensajes).toContain("Elige al menos un permiso");
        expect(mensajes).toContain("Escribe un nombre");
        expect(mensajes.some((m) => m.startsWith("Solo minúsculas"))).toBe(true);
        expect(esquemaRol.safeParse({ codigo: "mesa_ayuda", nombre: "Mesa", descripcion: "", permisos: ["tickets.ver"] }).success).toBe(true);
    });

    it("arma los cuerpos sin repetir permisos y sin descripción vacía al crear", () => {
        const d = { codigo: " mesa ", nombre: " Mesa ", descripcion: " ", permisos: ["tickets.ver", "agenda.ver", "tickets.ver"] };
        expect(cuerpoCrearRol(d)).toEqual({ codigo: "mesa", nombre: "Mesa", permisos: ["agenda.ver", "tickets.ver"] });
        expect(cuerpoEditarRol(d, 3)).toEqual({ version: 3, nombre: "Mesa", descripcion: "", permisos: ["agenda.ver", "tickets.ver"] });
    });

    it("asignar: rol obligatorio y zona o cuadrilla según el alcance (nunca ambas)", () => {
        expect(faltaEnAsignacion({ rol: "", alcance: "global" })).toBe("Elige un rol para poder asignarlo.");
        expect(faltaEnAsignacion({ rol: "soporte", alcance: "zona", zonaId: "" })).toBe("Elige la zona del alcance.");
        expect(faltaEnAsignacion({ rol: "soporte", alcance: "cuadrilla", cuadrillaId: "" })).toBe("Elige la cuadrilla del alcance.");
        expect(faltaEnAsignacion({ rol: "soporte", alcance: "global" })).toBeNull();
        expect(esquemaAsignarRol.safeParse({ rol: "soporte", alcance: "zona", zonaId: "", cuadrillaId: "" }).success).toBe(false);
        expect(cuerpoAsignarRol({ rol: "soporte", alcance: "zona", zonaId: "4", cuadrillaId: "9" })).toEqual({ rol: "soporte", zonaId: 4 });
        expect(cuerpoAsignarRol({ rol: "soporte", alcance: "cuadrilla", zonaId: "4", cuadrillaId: "9" })).toEqual({ rol: "soporte", cuadrillaId: 9 });
        expect(cuerpoAsignarRol({ rol: "soporte", alcance: "global", zonaId: "4", cuadrillaId: "" })).toEqual({ rol: "soporte" });
    });
});

describe("filtros del historial", () => {
    it("valida el rango y arma la query con el día de Guatemala", () => {
        expect(esquemaFiltrosAccesos.safeParse({ ...FILTROS_VACIOS, desde: "2026-10-30", hasta: "2026-10-29" }).success).toBe(false);
        expect(inicioDiaGuatemala("2026-10-30")).toBe("2026-10-30T06:00:00.000Z");
        expect(queryAccesos({ usuarioId: "7", canal: "campo_pwa", resultado: "exito", desde: "2026-10-30", hasta: "2026-10-30" })).toEqual({
            usuarioId: 7,
            canal: "campo_pwa",
            resultado: "exito",
            desde: "2026-10-30T06:00:00.000Z",
            hasta: "2026-10-31T06:00:00.000Z",
        });
        expect(queryAccesos(FILTROS_VACIOS)).toEqual({});
    });
});
