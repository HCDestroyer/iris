// API de Iris simulada para las pruebas: responde por método y ruta con cuerpos tipados por el
// contrato y guarda las peticiones (con sus cabeceras) para afirmar CSRF, canal y empresa.
import type { Schema } from "@iris/contratos";
import { configurarEntorno, entorno } from "../src/entorno";

export interface Peticion {
    metodo: string;
    ruta: string;
    cabeceras: Headers;
    cuerpo: unknown;
}

type Respuesta = { status: number; cuerpo?: unknown; cabeceras?: Record<string, string> };
type Manejador = (p: Peticion) => Respuesta | Promise<Respuesta>;

export const problema = (status: number, codigo: string, title = codigo): Respuesta => ({
    status,
    cuerpo: { type: `urn:iris:problema:${codigo}`, title, status } satisfies Schema<"Problema">,
    cabeceras: { "Content-Type": "application/problem+json" },
});

export class ApiFalsa {
    readonly peticiones: Peticion[] = [];
    private readonly rutas = new Map<string, Manejador>();
    sinRed = false;

    en(metodo: string, ruta: string, m: Manejador): this {
        this.rutas.set(`${metodo} ${ruta}`, m);
        return this;
    }

    instalar(): this {
        configurarEntorno({ ...entorno(), fetch: (entrada, init) => this.fetch(entrada, init) });
        return this;
    }

    async fetch(entrada: RequestInfo | URL, init?: RequestInit): Promise<Response> {
        const req = entrada instanceof Request ? entrada : new Request(String(entrada), init);
        const url = new URL(req.url);
        const texto = req.method === "GET" || req.method === "HEAD" ? "" : await req.text();
        const p: Peticion = { metodo: req.method, ruta: url.pathname, cabeceras: req.headers, cuerpo: texto ? (JSON.parse(texto) as unknown) : null };
        this.peticiones.push(p);
        if (this.sinRed) {
            throw new TypeError("Network request failed");
        }
        const m = this.rutas.get(`${p.metodo} ${p.ruta}`);
        const r = m ? await m(p) : problema(404, "no_encontrado");
        return new Response(r.cuerpo === undefined ? null : JSON.stringify(r.cuerpo), {
            status: r.status,
            headers: { "Content-Type": "application/json", ...r.cabeceras },
        });
    }
}

export const usuario = (id = 5): Schema<"Usuario"> => ({
    id,
    nombre: "Téc. Ana",
    correo: "ana@isp.gt",
    tipo: "persona",
    activo: true,
    mfaActivo: true,
    roles: [],
    empresas: [1],
    version: 1,
    creadoEn: "2026-10-01T00:00:00Z",
    actualizadoEn: "2026-10-01T00:00:00Z",
});

export const yo = (id = 5, empresas: Schema<"EmpresaResumen">[] = [{ id: 1, nombre: "ISP Norte" }]): Schema<"Yo"> => ({
    usuario: usuario(id),
    actor: "usuario",
    permisos: [],
    empresas,
    ...(empresas.length === 1 && empresas[0] ? { empresaActiva: empresas[0] } : {}),
    modoEmpresa: empresas.length === 1 ? "activa" : "por_elegir",
    todasLasEmpresas: false,
    hayDatosSinEmpresa: false,
});
