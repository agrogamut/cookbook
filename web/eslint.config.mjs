import { defineConfig, globalIgnores } from "eslint/config";
import nextVitals from "eslint-config-next/core-web-vitals";
import nextTs from "eslint-config-next/typescript";

const eslintConfig = defineConfig([
  ...nextVitals,
  ...nextTs,
  // Override default ignores of eslint-config-next.
  globalIgnores([
    // Default ignores of eslint-config-next:
    ".next/**",
    "out/**",
    "build/**",
    "next-env.d.ts",
    // shadcn-generated code: owned by the CLI, re-added verbatim on every `shadcn add`,
    // never hand-edited -- lint findings here are the upstream template's, not this repo's.
    "src/components/ui/**",
    "src/hooks/use-mobile.ts",
  ]),
]);

export default eslintConfig;
