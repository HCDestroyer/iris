// ESLint del panel (configuración plana). Reglas de CLAUDE.md §10: TypeScript estricto sin
// `any`, nada de controles crudos (<button>, <input>, <select>, <textarea>, <table>, <dialog>,
// <details>, <progress>, <meter>, ni armados con createElement o con roles a mano) fuera de packages/ui y nada de importar HeroUI directo: todo pasa por los envoltorios de
// @iris/ui (docs/ui/guia.md, «HeroUI primero»).
import js from "@eslint/js";
import nextPlugin from "@next/eslint-plugin-next";
import reactHooks from "eslint-plugin-react-hooks";
import globals from "globals";
import tseslint from "typescript-eslint";

// Elemento crudo → envoltorio de @iris/ui que lo reemplaza (HeroUI por dentro).
const EQUIVALENTE = {
    button: "Boton",
    input: "Campo o Entrada",
    select: "Selector",
    textarea: "AreaTexto",
    table: "Tabla",
    dialog: "Dialogo, DialogoConfirmar u Hoja",
    details: "Desplegable",
    summary: "Desplegable",
    progress: "BarraProgreso",
    meter: "Medidor",
};
// Los que también se pueden armar sin JSX (`createElement("table")`, `const Etiqueta = "table"`).
const ETIQUETAS_DINAMICAS = ["button", "input", "select", "textarea", "table", "dialog", "details"];

// Rol hecho a mano → envoltorio que ya lo trae.
const ROLES = {
    progressbar: "BarraProgreso",
    meter: "Medidor",
    radiogroup: "GrupoSegmentado, GrupoOpciones o ListaEleccion",
    radio: "GrupoSegmentado, GrupoOpciones o ListaEleccion",
    dialog: "Dialogo, Hoja o PanelEmergente",
};

const elementosCrudos = [
    ...Object.entries(EQUIVALENTE).map(([nombre, envoltorio]) => ({
        selector: `JSXOpeningElement[name.name='${nombre}']`,
        message: `Nada de <${nombre}> crudo fuera de packages/ui: usa ${envoltorio} de @iris/ui (HeroUI por dentro).`,
    })),
    ...ETIQUETAS_DINAMICAS.flatMap((nombre) => [
        {
            selector: `CallExpression:matches([callee.name='createElement'], [callee.property.name='createElement'])[arguments.0.value='${nombre}']`,
            message: `Nada de createElement("${nombre}") fuera de packages/ui: usa ${EQUIVALENTE[nombre]} de @iris/ui.`,
        },
        {
            selector: `VariableDeclarator[init.value='${nombre}']`,
            message: `Nada de guardar la etiqueta "${nombre}" para dibujarla después: usa ${EQUIVALENTE[nombre]} de @iris/ui.`,
        },
    ]),
    ...Object.entries(ROLES).map(([rol, envoltorio]) => ({
        selector: `JSXAttribute[name.name='role']:matches([value.value='${rol}'], [value.expression.value='${rol}'])`,
        message: `Nada de role="${rol}" hecho a mano fuera de packages/ui: usa ${envoltorio} de @iris/ui.`,
    })),
    {
        selector: "ImportExpression[source.value=/^(@heroui\\/|react-aria|@react-aria\\/|@react-stately\\/)/]",
        message: "Nada de import() de HeroUI o React Aria fuera de packages/ui: usa el envoltorio de @iris/ui (docs/ui/guia.md).",
    },
];

// Las pantallas nunca importan HeroUI (ni React Aria, que es lo que HeroUI envuelve): si falta una
// pieza, se agrega a packages/ui con nombre en español.
const importsProhibidos = {
    patterns: [
        { group: ["@heroui/*"], message: "Importa de @iris/ui, nunca de @heroui/*: si falta una pieza, agrégala a packages/ui (docs/ui/guia.md)." },
        {
            group: ["react-aria", "react-aria/*", "react-aria-components", "react-aria-components/*", "@react-aria/*", "@react-stately/*"],
            message: "Importa de @iris/ui: React Aria llega por los envoltorios de HeroUI en packages/ui.",
        },
    ],
};

export default tseslint.config(
    {
        ignores: [".next/**", "node_modules/**", "playwright-report/**", "test-results/**", "next-env.d.ts", "public/sw.js*", "public/swe-worker-*.js"],
    },
    // Un `eslint-disable` que ya no tapa nada es un error: no quedan llaves abiertas para saltar las reglas.
    { linterOptions: { reportUnusedDisableDirectives: "error" } },
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
