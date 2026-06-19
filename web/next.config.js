/** @type {import('next').NextConfig} */
const backendURL = process.env.DSO_API_URL || 'http://localhost:8471'

const nextConfig = {
  eslint: {
    ignoreDuringBuilds: true,
  },
  images: {
    unoptimized: true,
  },
  basePath: '',
  assetPrefix: '/',

  async rewrites() {
    return {
      beforeFiles: [
        {
          source: '/api/:path*',
          destination: `${backendURL}/api/:path*`,
        },
        {
          source: '/health',
          destination: `${backendURL}/health`,
        },
      ],
    }
  },
}

module.exports = nextConfig
