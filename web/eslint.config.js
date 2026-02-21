import js from "@eslint/js";

export default [
  js.configs.recommended,
  {
    languageOptions: {
      ecmaVersion: 2022,
      sourceType: "module",
      globals: {
        window: "readonly",
        document: "readonly",
        console: "readonly",
        fetch: "readonly",
        URL: "readonly",
        URLSearchParams: "readonly",
        AbortController: "readonly",
        setTimeout: "readonly",
        clearTimeout: "readonly",
        setInterval: "readonly",
        clearInterval: "readonly",
        Go: "readonly",
        WebAssembly: "readonly",
        HTMLElement: "readonly",
        MutationObserver: "readonly",
        RequestInit: "readonly",
        Response: "readonly",
        Event: "readonly",
        CustomEvent: "readonly",
        localStorage: "readonly",
        history: "readonly",
        location: "readonly",
        navigator: "readonly",
        alert: "readonly",
        confirm: "readonly",
        btoa: "readonly",
        atob: "readonly",
        AbortSignal: "readonly",
      },
    },
    rules: {
      "no-unused-vars": ["warn", { argsIgnorePattern: "^_" }],
    },
  },
];
