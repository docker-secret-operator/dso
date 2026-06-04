# DSO Web Dashboard

Production-ready web UI for Docker Secret Operator.

## Development Setup

```bash
# Install dependencies
npm install

# Start development server (http://localhost:3000)
npm run dev

# Build for production
npm run build

# Export static assets (creates ../ui-dist/)
npm run export
```

## Project Structure

```
web/
├── app/                  # Next.js app directory (routes)
│   ├── dashboard/        # Dashboard page
│   ├── secrets/          # Secrets management page
│   ├── events/           # Real-time events page
│   ├── audit/            # Audit log page
│   ├── settings/         # Settings page
│   └── layout.tsx        # Root layout
├── components/
│   ├── ui/               # shadcn/ui components (Button, Card, Badge, etc.)
│   ├── sidebar.tsx       # Navigation sidebar
│   ├── header.tsx        # Top header with status
│   └── providers.tsx     # React Query provider wrapper
├── hooks/
│   └── useWebSocket.ts   # WebSocket connection hook
├── lib/
│   ├── api-client.ts     # Typed API client with axios
│   ├── query-client.ts   # React Query configuration
│   ├── constants.ts      # Application constants
│   └── utils.ts          # Formatting and utility functions
├── styles/
│   └── globals.css       # Global styles + theme variables
├── public/               # Static assets
├── package.json          # Dependencies
├── tsconfig.json         # TypeScript config
├── next.config.js        # Next.js config (output: export for static)
├── tailwind.config.ts    # TailwindCSS config
└── postcss.config.js     # PostCSS config

```

## Key Technologies

- **Next.js 14**: React framework with app router
- **TypeScript**: Type-safe code
- **TailwindCSS**: Utility-first styling
- **shadcn/ui**: Pre-built accessible components
- **TanStack Query (React Query)**: Data fetching and caching
- **Axios**: HTTP client
- **Lucide Icons**: Icon library

## API Integration

The dashboard connects to the existing DSO REST API on `:8471`:

- `GET /health` - Agent health status
- `GET /api/secrets` - List secrets
- `GET /api/events` - Event history
- `GET /api/logs` - Audit logs
- `WS /api/events/ws` - Real-time event stream

## Development Workflow

1. **Run dev server**: `npm run dev`
2. **Open browser**: http://localhost:3000
3. **Make changes**: Files auto-reload via hot module replacement (HMR)
4. **Check types**: `tsc --noEmit` (run TypeScript compiler)

## Building for Production

```bash
# Export as static HTML/CSS/JS
npm run export

# Output goes to ../ui-dist/
# These static files embed in the Go binary
```

## Environment Variables

- `NEXT_PUBLIC_API_URL`: API base URL (default: http://127.0.0.1:8471)
- `NEXT_PUBLIC_WS_URL`: WebSocket URL (default: ws://127.0.0.1:8471)

## Notes

- The `next.config.js` has `output: 'export'` for static HTML export
- No Node.js runtime needed in production (embeds in Go binary)
- Images are disabled (`unoptimized: true`) for static export
- All components are type-safe with full TypeScript support
