import type { Config } from "tailwindcss"

const config = {
  darkMode: ["class"],
  content: [
    "./app/**/*.{js,ts,jsx,tsx,mdx}",
    "./components/**/*.{js,ts,jsx,tsx,mdx}",
    "./pages/**/*.{js,ts,jsx,tsx,mdx}",
  ],
  theme: {
    extend: {
      colors: {
        // shadcn/ui CSS variable tokens (keep for backward compat)
        border: "hsl(var(--border))",
        input: "hsl(var(--input))",
        ring: "hsl(var(--ring))",
        background: "hsl(var(--background))",
        foreground: "hsl(var(--foreground))",
        primary: {
          DEFAULT: "hsl(var(--primary))",
          foreground: "hsl(var(--primary-foreground))",
        },
        secondary: {
          DEFAULT: "hsl(var(--secondary))",
          foreground: "hsl(var(--secondary-foreground))",
        },
        destructive: {
          DEFAULT: "hsl(var(--destructive))",
          foreground: "hsl(var(--destructive-foreground))",
        },
        muted: {
          DEFAULT: "hsl(var(--muted))",
          foreground: "hsl(var(--muted-foreground))",
        },
        accent: {
          DEFAULT: "hsl(var(--accent))",
          foreground: "hsl(var(--accent-foreground))",
        },
        popover: {
          DEFAULT: "hsl(var(--popover))",
          foreground: "hsl(var(--popover-foreground))",
        },
        card: {
          DEFAULT: "hsl(var(--card))",
          foreground: "hsl(var(--card-foreground))",
        },
        // DSO design-system surface tokens
        surface: {
          0: "#0a0b0f",
          1: "#111318",
          2: "#1a1d24",
          3: "#21262e",
          4: "#2a2f3a",
        },
        // DSO border tokens
        "dso-border": {
          subtle: "rgba(255,255,255,0.05)",
          DEFAULT: "rgba(255,255,255,0.09)",
          strong: "rgba(255,255,255,0.15)",
        },
        // Status colors
        status: {
          healthy: "#10b981",
          warning: "#f59e0b",
          critical: "#ef4444",
          info: "#3b82f6",
          offline: "#64748b",
        },
      },
      borderRadius: {
        lg: "var(--radius)",
        md: "calc(var(--radius) - 2px)",
        sm: "calc(var(--radius) - 4px)",
      },
      fontFamily: {
        sans: ["Inter", "system-ui", "-apple-system", "sans-serif"],
        mono: ["JetBrains Mono", "Fira Code", "Consolas", "monospace"],
      },
      keyframes: {
        "accordion-down": {
          from: { height: "0" },
          to: { height: "var(--radix-accordion-content-height)" },
        },
        "accordion-up": {
          from: { height: "var(--radix-accordion-content-height)" },
          to: { height: "0" },
        },
        "slide-in-from-right": {
          from: { transform: "translateX(100%)", opacity: "0" },
          to: { transform: "translateX(0)", opacity: "1" },
        },
        "slide-out-to-right": {
          from: { transform: "translateX(0)", opacity: "1" },
          to: { transform: "translateX(100%)", opacity: "0" },
        },
        "fade-in": {
          from: { opacity: "0", transform: "translateY(4px)" },
          to: { opacity: "1", transform: "translateY(0)" },
        },
        "pulse-glow": {
          "0%, 100%": { opacity: "1" },
          "50%": { opacity: "0.4" },
        },
        "mesh-gradient": {
          "0%": { backgroundPosition: "0% 50%" },
          "50%": { backgroundPosition: "100% 50%" },
          "100%": { backgroundPosition: "0% 50%" },
        },
      },
      animation: {
        "accordion-down": "accordion-down 0.2s ease-out",
        "accordion-up": "accordion-up 0.2s ease-out",
        "slide-in": "slide-in-from-right 0.25s cubic-bezier(0.16,1,0.3,1)",
        "slide-out": "slide-out-to-right 0.2s ease-in",
        "fade-in": "fade-in 0.15s ease-out",
        "pulse-glow": "pulse-glow 2s ease-in-out infinite",
        "mesh-gradient": "mesh-gradient 15s ease infinite",
      },
      transitionTimingFunction: {
        spring: "cubic-bezier(0.16,1,0.3,1)",
      },
      boxShadow: {
        "card": "0 1px 3px rgba(0,0,0,0.4), 0 0 0 1px rgba(255,255,255,0.06)",
        "card-hover": "0 4px 12px rgba(0,0,0,0.5), 0 0 0 1px rgba(255,255,255,0.10)",
        "glow-indigo": "0 0 20px rgba(99,102,241,0.25)",
        "glow-red": "0 0 20px rgba(239,68,68,0.20)",
      },
    },
  },
  plugins: [require("tailwindcss-animate")],
} satisfies Config

export default config
