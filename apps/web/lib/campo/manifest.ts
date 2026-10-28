// Manifest de la PWA «Iris Campo» (lo sirve app/campo/manifest.webmanifest/route.ts).
export const MANIFEST_CAMPO = {
    id: "/campo",
    name: "Iris Campo",
    short_name: "Iris Campo",
    description: "Órdenes de trabajo del técnico, con y sin señal",
    lang: "es",
    dir: "ltr",
    start_url: "/campo",
    scope: "/campo",
    display: "standalone",
    orientation: "portrait",
    theme_color: "#0f766e",
    background_color: "#ffffff",
    categories: ["business", "productivity"],
    icons: [
        { src: "/icon.svg", sizes: "any", type: "image/svg+xml", purpose: "any" },
        { src: "/icon.svg", sizes: "any", type: "image/svg+xml", purpose: "maskable" },
    ],
} as const;
