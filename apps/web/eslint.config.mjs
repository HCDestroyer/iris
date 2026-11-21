// ESLint del panel (configuración plana). Reglas de CLAUDE.md §10: TypeScript estricto sin
// `any`, nada de controles crudos (<button>, <input>, <select>, <textarea>, <table>, <dialog>)
// fuera de packages/ui y nada de importar HeroUI directo: todo pasa por los envoltorios de
// @iris/ui (docs/ui/guia.md, «HeroUI primero»).
import js from "@eslint/js";
import nextPlugin from "@next/eslint-plugin-next";
import reactHooks from "eslint-plugin-react-hooks";
import globals from "globals";
import tseslint from "typescript-eslint";

const EQUIVALENTE = { button: "Boton", input: "Campo o Entrada", select: "Selector", textarea: "AreaTexto", table: "Tabla", dialog: "Dialogo, DialogoConfirmar u Hoja" };

const elementosCrudos = Object.entries(EQUIVALENTE).map(([nombre, envoltorio]) => ({
    selector: `JSXOpeningElement[name.name='${nombre}']`,
    message: `Nada de <${nombre}> crudo fuera de packages/ui: usa ${envoltorio} de @iris/ui (HeroUI por dentro).`,
}));

// Las pantallas nunca importan HeroUI (ni React Aria, que es lo que HeroUI envuelve): si falta una
// pieza, se agrega a packages/ui con nombre en español.
const importsProhibidos = {
    patterns: [
        { group: ["@heroui/*"], message: "Importa de @iris/ui, nunca de @heroui/*: si falta una pieza, agrégala a packages/ui (docs/ui/guia.md)." },
        { group: ["react-aria-components", "react-aria-components/*"], message: "Importa de @iris/ui: React Aria llega por los envoltorios de HeroUI en packages/ui." },
    ],
};

export default tseslint.config(
    {
        ignores: [".next/**", "node_modules/**", "playwright-report/**", "test-results/**", "next-env.d.ts", "public/sw.js*", "public/swe-worker-*.js"],
    },
    js.configs.recommended,
    ...tseslint.configs.strict,
    {
        files: ["**/*.{ts,tsx,mjs}"],
        languageOptions: { globals: { ...globals.browser, ...globals.node } },
        plugins: { "@next/next": nextPlugin, "react-hooks": reactHooks },
        rules: {
            ...nextPlugin.configs.recommended.rules,
            ...nextPlugin.configs["core-web-vitals"].rules,
            ...reactHooks.configs.recommended.rules,
            "@typescript-eslint/no-explicit-any": "error",
            "@typescript-eslint/consistent-type-imports": "error",
            "no-restricted-syntax": ["error", ...elementosCrudos],
            "no-restricted-imports": ["error", importsProhibidos],
        },
    },
);
