/** @type {import('next').NextConfig} */
const nextConfig = {
  output: 'export',
  eslint: {
    ignoreDuringBuilds: true,
  },
  images: {
    unoptimized: true,
  },
  basePath: '',
  assetPrefix: '/',

  // Development proxy for API calls
  // Uncomment the rewrites section below to enable API proxying during development
  // You need to have the DSO backend running on localhost:8471
}

// Rewrites for development (commented out for static export)
// Uncomment this for local development with running backend
/*
const withRewrites = async () => {
  return {
    ...nextConfig,
    async rewrites() {
      return {
        beforeFiles: [
          {
            source: '/api/:path*',
            destination: 'http://localhost:8471/api/:path*',
          },
          {
            source: '/health',
            destination: 'http://localhost:8471/health',
          },
        ],
      }
    },
  }
}

module.exports = withRewrites()
*/

module.exports = nextConfig
