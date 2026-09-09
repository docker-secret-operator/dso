/** @type {import('next').NextConfig} */
const nextConfig = {
  // Static export: the Go backend (internal/webui) embeds and serves this
  // output directly from its own HTTP mux, alongside /api/* served by the
  // same process (internal/server). There is no Node runtime in production,
  // so no rewrites()/middleware/server routes are usable here -- the Go
  // server already serves /api/* at the same origin, so client code just
  // calls same-origin paths directly (see lib/api-client.ts, lib/api-fetch.ts).
  output: 'export',

  // Static export requires either a static `next/image` loader disabled or
  // a custom loader; DSO doesn't use next/image yet, but disable built-in
  // optimization defensively since it requires a Node server.
  images: {
    unoptimized: true,
  },

  // Next.js embeds a random buildId into every page's HTML and into every
  // chunk filename by default, so two builds of *identical* source produce
  // completely different output. That breaks internal/webui/assets/'s
  // whole purpose: it's committed and diffed by CI (scripts/build-webui.sh
  // + the "webui" CI job) specifically so a fresh rebuild of unchanged
  // source is a no-op. A fixed buildId here doesn't need to change across
  // deploys -- the embedded output is fully replaced by every
  // scripts/build-webui.sh run, and DSO ships as one binary with no
  // multiple-version cache-coexistence concern that random buildIds
  // normally protect against.
  generateBuildId: async () => 'dso-webui',

  // A fixed buildId alone isn't enough: webpack's default chunk/module id
  // derivation can still vary between separate build invocations of
  // identical source (observed directly -- two consecutive
  // scripts/build-webui.sh runs with a fixed buildId still produced
  // different chunk hashes). Forcing 'deterministic' ids ties them to
  // stable content/dependency-graph hashes instead of build-specific
  // internal state, which is what actually makes internal/webui/assets/
  // reproducible across builds.
  webpack: (config) => {
    config.optimization.moduleIds = 'deterministic'
    config.optimization.chunkIds = 'deterministic'
    return config
  },
}

module.exports = nextConfig
