import { dirname } from "node:path";
import { fileURLToPath } from "node:url";
import { FlatCompat } from "@eslint/eslintrc";
import i18next from "eslint-plugin-i18next";

const __filename = fileURLToPath(import.meta.url);
const __dirname = dirname(__filename);

const compat = new FlatCompat({
  baseDirectory: __dirname,
});

const eslintConfig = [
  {
    ignores: [".next/**", "node_modules/**", "next-env.d.ts", "messages/**"],
  },
  ...compat.extends("next/core-web-vitals", "next/typescript"),
  i18next.configs["flat/recommended"],
];

export default eslintConfig;
