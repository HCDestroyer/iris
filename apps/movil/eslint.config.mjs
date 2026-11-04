// ESLint de la app nativa: TypeScript estricto (sin `any`), reglas de hooks y nada de primitivos
// táctiles crudos ni de HeroUI Native directo en las pantallas (pasan por `src/componentes`, el
// equivalente de `@iris/ui`).
import js from "@eslint/js";
import reactHooks from "eslint-plugin-react-hooks";
import globals from "globals";
import tseslint from "typescript-eslint";

export default tseslint.config(
    { ignores: ["node_modules/**", "dist/**", ".expo/**", "android/**", "ios/**", "expo-env.d.ts"] },
    js.configs.recommended,
    ...tseslint.configs.strict,
    {
        files: ["**/*.{ts,tsx}"],
        plugins: { "react-hooks": reactHooks },
        languageOptions: { globals: { ...globals.es2022 } },
        rules: {
            ...reactHooks.configs.recommended.rules,
            "@typescript-eslint/no-explicit-any": "error",
            "@typescript-eslint/consistent-type-imports": "error",
        },
    },
    {
        files: ["src/pantallas/**/*.tsx", "src/App.tsx"],
        rules: {
            "no-restricted-imports": [
                "error",
                {
                    paths: [
                        {
                            name: "react-native",
                            importNames: ["Pressable", "TouchableOpacity", "TouchableHighlight", "TextInput", "Button", "ActivityIndicator"],
                            message: "Usa Boton, Campo, Opciones, Tocable o Cargando de src/componentes (identidad Horus).",
                        },
                    ],
                    patterns: [
                        {
                            group: ["heroui-native", "heroui-native/*"],
                            message: "Las pantallas usan src/componentes (HeroUI Native envuelto con el tema Horus), nunca heroui-native directo.",
                        },
                    ],
                },
            ],
        },
    },
    {
        files: ["*.config.{js,mjs}", "jest.config.js", "prueba/resolver.js"],
        languageOptions: { globals: { ...globals.node }, sourceType: "commonjs" },
        rules: { "@typescript-eslint/no-require-imports": "off" },
    },
);
