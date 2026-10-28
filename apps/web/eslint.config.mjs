// ESLint del panel (configuración plana). Reglas de CLAUDE.md §10: TypeScript estricto sin
// `any` y nada de <button>/<input> crudos fuera de packages/ui.
import js from "@eslint/js";
import nextPlugin from "@next/eslint-plugin-next";
import reactHooks from "eslint-plugin-react-hooks";
import globals from "globals";
import tseslint from "typescript-eslint";

const elementosCrudos = ["button", "input", "select", "textarea"].map((nombre) => ({
    selector: `JSXOpeningElement[name.name='${nombre}']`,
    message: `Nada de <${nombre}> crudo fuera de packages/ui: usa el componente de @iris/ui.`,
}));

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
        },
    },
);
