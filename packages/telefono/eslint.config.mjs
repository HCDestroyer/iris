// ESLint de @iris/telefono: TypeScript estricto (sin `any`). Usa WebRTC y WebSocket del navegador
// (el panel y /campo).
import js from "@eslint/js";
import globals from "globals";
import tseslint from "typescript-eslint";

export default tseslint.config(
    { ignores: ["node_modules/**", "dist/**"] },
    js.configs.recommended,
    ...tseslint.configs.strict,
    {
        files: ["**/*.{ts,mjs}"],
        languageOptions: { globals: { ...globals.es2022, ...globals.browser } },
        rules: {
            "@typescript-eslint/no-explicit-any": "error",
            "@typescript-eslint/consistent-type-imports": "error",
        },
    },
    {
        files: ["*.config.{ts,mjs}"],
        languageOptions: { globals: { ...globals.node } },
    },
);
