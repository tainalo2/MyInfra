import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";
import tailwindcss from "@tailwindcss/vite";

// MYINFRA_API points the dev server at another MyInfra, such as a test one.
const target = process.env.MYINFRA_API || "http://127.0.0.1:8080";

export default defineConfig({
  plugins: [react(), tailwindcss()],
  server: {
    port: Number(process.env.VITE_PORT) || 5173,
    proxy: {
      "/api": { target, ws: true },
      "/join": target,
    },
  },
});
