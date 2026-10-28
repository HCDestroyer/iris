// ESLint de @iris/sync: TypeScript estricto (sin `any`) y núcleo sin DOM.
import js from "@eslint/js";
import globals from "globals";
import tseslint from "typescript-eslint";

export default tseslint.config(
    { ignores: ["node_modules/**", "dist/**"] },
    js.configs.recommended,
    ...tseslint.configs.strict,
    {
        files: ["**/*.{ts,mjs}"],
        languageOptions: { globals: { ...globals.es2022 } },
        rules: {
            "@typescript-eslint/no-explicit-any": "error",
            "@typescript-eslint/consistent-type-imports": "error",
            // El núcleo se reutiliza en Expo: nada del DOM fuera del adaptador de Dexie.
            "no-restricted-globals": [
                "error",
                { name: "window", message: "El núcleo de @iris/sync no depende del DOM." },
                { name: "document", message: "El núcleo de @iris/sync no depende del DOM." },
                { name: "navigator", message: "El núcleo de @iris/sync no depende del DOM." },
                { name: "localStorage", message: "El núcleo de @iris/sync no depende del DOM." },
                { name: "indexedDB", message: "Solo el adaptador de Dexie toca IndexedDB." },
            ],
        },
    },
    {
        files: ["*.config.{ts,mjs}"],
        languageOptions: { globals: { ...globals.node } },
    },
);
