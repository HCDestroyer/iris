// Genera lib/manual/contenido.generado.json desde docs/manual/**/*.md (el manual de Iris).
//
//   pnpm --filter @iris/web manual              regenera el JSON
//   pnpm --filter @iris/web manual --comprobar  falla si el JSON no está al día
//
// El panel no lee docs/ en tiempo de ejecución: la imagen de Docker solo copia apps/web y
// packages/. Por eso el contenido viaja como JSON dentro de apps/web, y una prueba
// (lib/manual/contenido.test.ts) verifica que coincide con los .md.
import { readdirSync, readFileSync, writeFileSync } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

const web = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
const raiz = path.resolve(web, "../../docs/manual");
const destino = path.join(web, "lib/manual/contenido.generado.json");

/** Archivos que no son páginas: el índice va aparte y claves.md es el contrato con el botón «?». */
const FUERA = new Set(["indice.md", "claves.md"]);

function leerPaginas() {
    const paginas = [];
    for (const seccion of readdirSync(raiz, { withFileTypes: true })) {
        if (!seccion.isDirectory()) continue;
        for (const archivo of readdirSync(path.join(raiz, seccion.name))) {
            if (!archivo.endsWith(".md")) continue;
            const fuente = readFileSync(path.join(raiz, seccion.name, archivo), "utf8").replace(/\r\n?/g, "\n");
            const titulo = /^# (.+)$/m.exec(fuente)?.[1]?.trim();
            if (!titulo) throw new Error(`${seccion.name}/${archivo}: falta el título (# …) en la primera línea`);
            paginas.push({ clave: `${seccion.name}/${archivo.slice(0, -3)}`, titulo, fuente });
        }
    }
    return paginas.sort((a, b) => a.clave.localeCompare(b.clave, "es"));
}

export function generar() {
    const indice = readFileSync(path.join(raiz, "indice.md"), "utf8").replace(/\r\n?/g, "\n");
    for (const archivo of readdirSync(raiz)) {
        if (archivo.endsWith(".md") && !FUERA.has(archivo)) throw new Error(`docs/manual/${archivo}: las páginas van en una carpeta por módulo`);
    }
    return `${JSON.stringify({ indice, paginas: leerPaginas() }, null, 4)}\n`;
}

if (process.argv[1] && path.resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
    const contenido = generar();
    if (process.argv.includes("--comprobar")) {
        let actual = "";
        try {
            actual = readFileSync(destino, "utf8");
        } catch {
            // Sin archivo: no está al día.
        }
        if (actual !== contenido) {
            console.error("El manual no está al día: corre `pnpm --filter @iris/web manual`.");
            process.exit(1);
        }
        console.log("El manual está al día.");
    } else {
        writeFileSync(destino, contenido);
        console.log(`Manual generado: ${path.relative(web, destino)}`);
    }
}
