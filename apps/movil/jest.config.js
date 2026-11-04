// Jest con jest-expo (módulos nativos simulados) y React Native Testing Library. Los paquetes del
// workspace (@iris/*) se publican como TypeScript fuente y Babel los transforma como el resto.
/** @type {import('jest').Config} */
module.exports = {
    preset: "jest-expo",
    roots: ["<rootDir>/prueba"],
    setupFiles: ["<rootDir>/prueba/preparar.ts"],
    setupFilesAfterEnv: ["<rootDir>/prueba/despues.ts"],
    // La primera prueba de cada archivo carga React Native y el motor (en frío, sin caché, pasa de 5 s).
    testTimeout: 30_000,
    // lucide-react-native publica ESM en .mjs (que Jest no transforma): en pruebas, su build CommonJS.
    moduleNameMapper: {
        "^lucide-react-native$": "<rootDir>/node_modules/lucide-react-native/dist/cjs/lucide-react-native.js",
    },
    // Los de jest-expo más HeroUI Native y Uniwind, que se publican como ESM (con pnpm, la ruta
    // real pasa por `node_modules/.pnpm/<paquete>/node_modules/<paquete>`).
    // Reanimated y Worklets en su implementación de JS (la que traen para Jest), sin módulo nativo.
    resolver: "<rootDir>/prueba/resolver.js",
    transformIgnorePatterns: [
        "/node_modules/(?!(\\.pnpm|react-native|@react-native|@react-native-community|expo|@expo|heroui-native|uniwind|tailwind-variants|tailwind-merge)(/|@|\\+|-))",
        "/node_modules/react-native-reanimated/plugin/",
        "/node_modules/@react-native/babel-preset/",
    ],
};
