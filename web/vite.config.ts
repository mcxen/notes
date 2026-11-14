import react from "@vitejs/plugin-react";
import { defineConfig, type Plugin } from "vite";

/** KaTeX's stylesheet points at woff2, woff and ttf copies of the same faces.
 *  Browsers here take woff2, so the other files are dropped before Vite
 *  collects assets — otherwise the formula chunk would ship three of each. */
function katexWoff2Only(): Plugin {
  return {
    name: "katex-woff2-only",
    enforce: "pre",
    transform(code, id) {
      if (!id.includes("katex") || !id.includes(".css")) return null;
      const next = code.replace(/,url\([^)]+\.woff\) format\("woff"\),url\([^)]+\.ttf\) format\("truetype"\)/g, "");
      return next === code ? null : { code: next, map: null };
    },
  };
}

export default defineConfig({
  base: "./",
  plugins: [react(), katexWoff2Only()],
});
