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
}

module.exports = nextConfig
