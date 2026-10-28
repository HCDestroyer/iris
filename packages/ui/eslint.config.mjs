// ESLint de @iris/ui: TypeScript estricto (sin `any`) y reglas de hooks de React.
import js from "@eslint/js";
import reactHooks from "eslint-plugin-react-hooks";
import globals from "globals";
import tseslint from "typescript-eslint";

export default tseslint.config(
    { ignores: ["node_modules/**", "dist/**"] },
    js.configs.recommended,
    ...tseslint.configs.strict,
    {
        files: ["**/*.{ts,tsx}"],
        languageOptions: { globals: { ...globals.browser, ...globals.node } },
        plugins: { "react-hooks": reactHooks },
        rules: {
            ...reactHooks.configs.recommended.rules,
            "@typescript-eslint/no-explicit-any": "error",
            "@typescript-eslint/consistent-type-imports": "error",
        },
    },
);
