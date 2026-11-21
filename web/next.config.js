// CommonJS config: Next.js loads next.config.js with require(), so require() is the supported form here.
/* eslint-disable @typescript-eslint/no-require-imports */
const crypto = require('crypto')
const path = require('path')

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
  webpack: (config, { dev }) => {
    config.optimization.chunkIds = 'deterministic'
    if (dev) {
      config.optimization.moduleIds = 'deterministic'
    } else {
      // Production (the only build that is embedded): module ids are
      // assigned by CheckoutPathNeutralModuleIds below, replacing webpack's
      // stock 'deterministic' ids, which depend on the checkout directory.
      config.optimization.moduleIds = false
      config.plugins.push(new CheckoutPathNeutralModuleIds())
    }
    return config
  },
}

// Checkout-location independence (docs/audit/35).
//
// webpack's stock 'deterministic' module ids hash
// makePathsRelative(context, module.identifier()). That only relativises the
// resource path; Next.js's own loaders carry ABSOLUTE paths inside their
// request *query strings*:
//   next-app-loader?page=..&pagePath=<abs>&appDir=<abs>&appPaths=<abs>
//   next-flight-client-entry-loader?modules={"request":"<abs path>",..}
//   next-metadata-route-loader?..   next-client-pages-loader?..
// so the id of every page / layout / main-app entry module (and thereby the
// chunk bodies, content-hashed filenames, HTML and RSC flight payloads that
// mention them) changed with the directory the repository was checked out
// in. That made the committed internal/webui/assets/ unreproducible on CI,
// which checks out somewhere else.
//
// This plugin assigns the same kind of ids (small numbers derived from a hash
// of the module identifier) but first replaces the checkout directory, in
// raw / percent-encoded / JSON-escaped form, by a constant token, so ids are
// a function of the project-relative layout only. Modules are processed in a
// path-independent order (sorted by neutralised identifier). It affects ids
// and the hashes derived from them, not generated code semantics.
class CheckoutPathNeutralModuleIds {
  apply(compiler) {
    const root = path.resolve(__dirname)
    const posixRoot = root.split(path.sep).join('/')
    const forms = [
      root,
      posixRoot,
      JSON.stringify(root).slice(1, -1),
      encodeURIComponent(root),
      encodeURIComponent(posixRoot),
    ]
      .filter((v, i, all) => v && all.indexOf(v) === i)
      .sort((x, y) => y.length - x.length) // longest first
    const neutral = (text) => forms.reduce((t, f) => t.split(f).join('<web-root>'), text)

    compiler.hooks.compilation.tap('CheckoutPathNeutralModuleIds', (compilation) => {
      compilation.hooks.moduleIds.tap('CheckoutPathNeutralModuleIds', () => {
        const { chunkGraph } = compilation
        const used = new Set()
        const todo = []
        for (const mod of compilation.modules) {
          if (!mod.needId) continue
          const id = chunkGraph.getModuleId(mod)
          if (id !== null) used.add(String(id))
          else if (chunkGraph.getNumberOfModuleChunks(mod) > 0) {
            todo.push({ mod, name: neutral(mod.identifier()) })
          }
        }
        todo.sort((x, y) => (x.name < y.name ? -1 : x.name > y.name ? 1 : 0))
        // Id space: smallest power of ten >= 4x the module count, so probing
        // collisions stays rare and independent of any path.
        let space = 1000
        while (space < (todo.length + used.size) * 4) space *= 10
        for (const { mod, name } of todo) {
          const digest = crypto.createHash('sha256').update(name).digest()
          let id = Number(digest.readBigUInt64BE(0) % BigInt(space))
          while (used.has(String(id))) id = (id + 1) % space
          used.add(String(id))
          chunkGraph.setModuleId(mod, id)
        }
      })
    })
  }
}

module.exports = nextConfig
